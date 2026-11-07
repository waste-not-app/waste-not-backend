package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	coreauth "github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
)

type Repository interface {
	CreateUserWithSession(ctx context.Context, u UserRes, passwordHash string, signer coreauth.Signer, now time.Time) (coreauth.Tokens, error)
	GetUserByEmail(ctx context.Context, email string) (UserRecord, error)
	CreateSessionForUser(ctx context.Context, u UserRes, signer coreauth.Signer, now time.Time) (coreauth.Tokens, error)
	RotateRefreshToken(ctx context.Context, refreshToken string, signer coreauth.Signer, now time.Time) (coreauth.Tokens, error)
	RevokeSession(ctx context.Context, sessionID, userID string) error
	GetUserProgress(ctx context.Context, userID, today string) (domain.Progress, error)
}

type repository struct {
	dbPool *pgxpool.Pool
	q      *db.Queries
}

func NewRepository(dbPool *pgxpool.Pool) Repository {
	return &repository{
		dbPool: dbPool,
		q:      db.New(dbPool),
	}
}

func isDuplicate(err error) bool {
	var p *pgconn.PgError
	return errors.As(err, &p) && p.Code == "23505"
}

func (r *repository) insertSession(ctx context.Context, qtx *db.Queries, u UserRes, signer coreauth.Signer, now time.Time) (coreauth.Tokens, error) {
	nowUTC := now.UTC()
	sid := uuid.NewString()
	tokens, err := signer.Issue(u.ID, sid, 1, nowUTC.Add(coreauth.RefreshTTL), nowUTC)
	if err != nil {
		return tokens, err
	}
	if err = qtx.CreateSession(ctx, db.CreateSessionParams{
		ID:        sid,
		UserID:    u.ID,
		ExpiresAt: tokens.RefreshExpiresAt,
	}); err != nil {
		return tokens, err
	}
	err = qtx.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		TokenHash: coreauth.Digest([]byte(tokens.RefreshToken)),
		SessionID: sid,
	})
	tokens.User = u
	return tokens, err
}

func (r *repository) CreateUserWithSession(ctx context.Context, u UserRes, passwordHash string, signer coreauth.Signer, now time.Time) (coreauth.Tokens, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return coreauth.Tokens{}, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	err = qtx.CreateUser(ctx, db.CreateUserParams{
		ID:              u.ID,
		Name:            u.Name,
		NormalizedEmail: u.Email,
		PasswordHash:    passwordHash,
		Timezone:        u.Timezone,
	})
	if isDuplicate(err) {
		return coreauth.Tokens{}, ErrEmailExists
	}
	if err != nil {
		return coreauth.Tokens{}, err
	}

	tokens, err := r.insertSession(ctx, qtx, u, signer, now)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return tokens, err
}

func (r *repository) GetUserByEmail(ctx context.Context, email string) (UserRecord, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserRecord{}, ErrUnauthenticated
	}
	if err != nil {
		return UserRecord{}, err
	}
	return UserRecord{
		UserRes: UserRes{
			ID:       row.ID,
			Name:     row.Name,
			Email:    row.NormalizedEmail,
			Timezone: row.Timezone,
		},
		PasswordHash: row.PasswordHash,
	}, nil
}

func (r *repository) CreateSessionForUser(ctx context.Context, u UserRes, signer coreauth.Signer, now time.Time) (coreauth.Tokens, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return coreauth.Tokens{}, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	tokens, err := r.insertSession(ctx, qtx, u, signer, now)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return tokens, err
}

func (r *repository) RotateRefreshToken(ctx context.Context, refreshToken string, signer coreauth.Signer, now time.Time) (coreauth.Tokens, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return coreauth.Tokens{}, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	hash := coreauth.Digest([]byte(refreshToken))
	row, err := qtx.LockRefreshTokenSession(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return coreauth.Tokens{}, ErrSessionExpired
	}
	if err != nil {
		return coreauth.Tokens{}, err
	}

	if row.Used {
		if err = qtx.RevokeSessionByID(ctx, row.SessionID); err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			return coreauth.Tokens{}, err
		}
		return coreauth.Tokens{}, ErrSessionExpired
	}

	if row.Revoked || !row.ExpiresAt.After(now) {
		return coreauth.Tokens{}, ErrSessionExpired
	}

	tokens, err := signer.Issue(row.UserID, row.SessionID, row.Generation+1, row.ExpiresAt, now.UTC())
	if err != nil {
		return coreauth.Tokens{}, err
	}

	if err = qtx.MarkRefreshTokenUsed(ctx, hash); err == nil {
		err = qtx.IncrementSessionGeneration(ctx, row.SessionID)
	}
	if err == nil {
		err = qtx.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
			TokenHash: coreauth.Digest([]byte(tokens.RefreshToken)),
			SessionID: row.SessionID,
		})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return tokens, err
}

func (r *repository) RevokeSession(ctx context.Context, sessionID, userID string) error {
	return r.q.RevokeUserSession(ctx, db.RevokeUserSessionParams{
		ID:     sessionID,
		UserID: userID,
	})
}

func (r *repository) GetUserProgress(ctx context.Context, userID, today string) (domain.Progress, error) {
	stats, err := r.q.GetUserProgressStats(ctx, userID)
	return domain.MakeProgress(stats.Count, stats.ActivityDates, today), err
}
