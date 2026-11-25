package auth

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/waste-not-app/waste-not-backend/internal/database/db"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

func (r *repository) IssueGoogleChallenge(ctx context.Context, nonceHash string) error {
	return r.q.IssueGoogleChallenge(ctx, nonceHash)
}

func (r *repository) ClaimGoogleChallenge(ctx context.Context, nonceHash string) error {
	claimed, err := r.q.ClaimGoogleChallenge(ctx, nonceHash)
	if err != nil {
		return err
	}
	if claimed != 1 {
		return ErrUnauthenticated
	}
	return nil
}

func (r *repository) ExchangeGoogleIdentity(ctx context.Context, identity GoogleIdentity, timezone string, password *string, signer security.Signer, now time.Time) (security.Tokens, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return security.Tokens{}, err
	}
	defer tx.Rollback(ctx)
	q := r.q.WithTx(tx)
	user, err := resolveGoogleAccount(ctx, q, identity, timezone, password)
	if err != nil {
		return security.Tokens{}, err
	}
	tokens, err := r.insertSession(ctx, q, user, signer, now)
	if err != nil {
		return security.Tokens{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return security.Tokens{}, err
	}
	return tokens, nil
}

func resolveGoogleAccount(ctx context.Context, q *db.Queries, identity GoogleIdentity, timezone string, password *string) (UserRes, error) {
	// Serialize exchanges for the same subject across instances, even before a user exists.
	if err := q.LockGoogleSubject(ctx, identity.Subject); err != nil {
		return UserRes{}, err
	}
	lookup := db.FindGoogleAccountParams{Subject: identity.Subject, Email: identity.Email}
	id, err := q.FindGoogleAccount(ctx, lookup)
	if errors.Is(err, pgx.ErrNoRows) && password == nil {
		user := UserRes{ID: uuid.NewString(), Name: googleAccountName(identity.Name), Email: identity.Email, Timezone: timezone}
		created, createErr := q.CreateGoogleUser(ctx, db.CreateGoogleUserParams{
			ID: user.ID, Name: user.Name, NormalizedEmail: user.Email, Timezone: timezone, Subject: identity.Subject,
		})
		if createErr != nil {
			return UserRes{}, createErr
		}
		if created == 1 {
			return user, nil
		}
		// Password registration or another Google subject won the unique email race.
		id, err = q.FindGoogleAccount(ctx, lookup)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return UserRes{}, ErrUnauthenticated
	}
	if err != nil {
		return UserRes{}, err
	}
	// Do not fall back to creating a new account if this row disappears while waiting for deletion.
	account, err := q.LockAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserRes{}, ErrUnauthenticated
	}
	if err != nil {
		return UserRes{}, err
	}
	if account.GoogleSub != "" && account.GoogleSub != identity.Subject {
		return UserRes{}, ErrGoogleIdentityConflict
	}
	if account.GoogleSub == "" && password == nil {
		return UserRes{}, ErrGoogleLinkRequired
	}
	if password != nil && !security.Verify(*password, account.PasswordHash) {
		return UserRes{}, ErrUnauthenticated
	}
	if account.GoogleSub == "" {
		if err := q.LinkGoogleSubject(ctx, db.LinkGoogleSubjectParams{ID: id, Subject: identity.Subject}); err != nil {
			return UserRes{}, err
		}
	}
	return lockedUserRes(id, account), nil
}

func googleAccountName(name string) string {
	name = strings.TrimSpace(name)
	if strings.ContainsRune(name, '\x00') {
		return "Google user"
	}
	n := 0
	for i := range name {
		if n == domain.MaxNameLength {
			name = strings.TrimSpace(name[:i])
			n = utf8.RuneCountInString(name)
			break
		}
		n++
	}
	if n < domain.MinNameLength {
		return "Google user"
	}
	return name
}
