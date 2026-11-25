package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/database/db"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/modules/photos"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

type Repository interface {
	CreateUserWithSession(ctx context.Context, u UserRes, passwordHash string, signer security.Signer, now time.Time) (security.Tokens, error)
	GetPasswordAccount(ctx context.Context, email string) (userID, passwordHash string, err error)
	CreateSessionForUser(ctx context.Context, userID, passwordHash string, signer security.Signer, now time.Time) (security.Tokens, error)
	IssueGoogleChallenge(ctx context.Context, nonceHash string) error
	ClaimGoogleChallenge(ctx context.Context, nonceHash string) error
	ExchangeGoogleIdentity(ctx context.Context, identity GoogleIdentity, timezone string, password *string, signer security.Signer, now time.Time) (security.Tokens, error)
	RotateRefreshToken(ctx context.Context, refreshToken string, signer security.Signer, now time.Time) (security.Tokens, error)
	RevokeSession(ctx context.Context, sessionID, userID string) error
	GetUserProgress(ctx context.Context, userID, today string) (domain.Progress, error)
	RequestPasswordReset(ctx context.Context, email, tokenHash string, encrypted []byte, now time.Time) error
	ConfirmPasswordReset(ctx context.Context, tokenHash, passwordHash string, now time.Time) error
	DeleteMe(ctx context.Context, proof deleteAccountProof) error
	DeliverResetMail(ctx context.Context, deliver func(context.Context, string, []byte) error) (bool, error)
}

type repository struct {
	dbPool  *pgxpool.Pool
	q       *db.Queries
	storage photos.Storage
}

func NewRepository(dbPool *pgxpool.Pool, storage photos.Storage) Repository {
	return &repository{
		dbPool:  dbPool,
		q:       db.New(dbPool),
		storage: storage,
	}
}

func isDuplicate(err error) bool {
	var p *pgconn.PgError
	return errors.As(err, &p) && p.Code == "23505"
}

func (r *repository) insertSession(ctx context.Context, qtx *db.Queries, u UserRes, signer security.Signer, now time.Time) (security.Tokens, error) {
	nowUTC := now.UTC()
	sid := uuid.NewString()
	tokens, err := signer.Issue(u.ID, sid, 1, nowUTC.Add(security.RefreshTTL), nowUTC)
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
		TokenHash: security.Digest([]byte(tokens.RefreshToken)),
		SessionID: sid,
	})
	tokens.User = u
	return tokens, err
}

func (r *repository) CreateUserWithSession(ctx context.Context, u UserRes, passwordHash string, signer security.Signer, now time.Time) (security.Tokens, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return security.Tokens{}, err
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
		return security.Tokens{}, ErrEmailExists
	}
	if err != nil {
		return security.Tokens{}, err
	}

	tokens, err := r.insertSession(ctx, qtx, u, signer, now)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return tokens, err
}

func (r *repository) GetPasswordAccount(ctx context.Context, email string) (userID, passwordHash string, err error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrUnauthenticated
	}
	return row.ID, row.PasswordHash, err
}

func (r *repository) CreateSessionForUser(ctx context.Context, userID, passwordHash string, signer security.Signer, now time.Time) (security.Tokens, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return security.Tokens{}, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	// Serialize with password replacement so a pre-reset password check cannot create a new session afterward.
	account, err := qtx.LockAccount(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && account.PasswordHash != passwordHash) {
		return security.Tokens{}, ErrUnauthenticated
	}
	if err != nil {
		return security.Tokens{}, err
	}
	tokens, err := r.insertSession(ctx, qtx, lockedUserRes(userID, account), signer, now)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return tokens, err
}

func lockedUserRes(id string, account db.LockAccountRow) UserRes {
	return UserRes{
		ID:          id,
		Name:        account.Name,
		Email:       account.NormalizedEmail,
		Timezone:    account.Timezone,
		HasPassword: account.HasPassword,
	}
}

func (r *repository) RotateRefreshToken(ctx context.Context, refreshToken string, signer security.Signer, now time.Time) (security.Tokens, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return security.Tokens{}, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	hash := security.Digest([]byte(refreshToken))
	row, err := qtx.LockRefreshTokenSession(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return security.Tokens{}, ErrSessionExpired
	}
	if err != nil {
		return security.Tokens{}, err
	}

	if row.Used {
		if err = qtx.RevokeSessionByID(ctx, row.SessionID); err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			return security.Tokens{}, err
		}
		return security.Tokens{}, ErrSessionExpired
	}

	if row.Revoked || !row.ExpiresAt.After(now) {
		return security.Tokens{}, ErrSessionExpired
	}

	tokens, err := signer.Issue(row.UserID, row.SessionID, row.Generation+1, row.ExpiresAt, now.UTC())
	if err != nil {
		return security.Tokens{}, err
	}

	if err = qtx.MarkRefreshTokenUsed(ctx, hash); err == nil {
		err = qtx.IncrementSessionGeneration(ctx, row.SessionID)
	}
	if err == nil {
		err = qtx.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
			TokenHash: security.Digest([]byte(tokens.RefreshToken)),
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
