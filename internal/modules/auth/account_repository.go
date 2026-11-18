package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	coreauth "github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/modules/photos"
	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
)

func (r *repository) RequestPasswordReset(ctx context.Context, email, tokenHash string, encrypted []byte, now time.Time) error {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := r.q.WithTx(tx)
	id, err := q.LockAccountByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	expires := now.UTC().Add(15 * time.Minute)
	if err := q.ReplacePasswordResetToken(ctx, db.ReplacePasswordResetTokenParams{UserID: id, TokenHash: tokenHash, ExpiresAt: expires}); err != nil {
		return err
	}
	if err := q.QueuePasswordResetMail(ctx, db.QueuePasswordResetMailParams{UserID: id, EncryptedToken: encrypted, ExpiresAt: expires}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *repository) ConfirmPasswordReset(ctx context.Context, tokenHash, passwordHash string, now time.Time) error {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := r.q.WithTx(tx)
	id, err := q.GetPasswordResetOwner(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidResetToken
	}
	if err != nil {
		return err
	}
	if _, err := q.LockAccount(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidResetToken
	} else if err != nil {
		return err
	}
	expires, err := q.LockPasswordResetToken(ctx, db.LockPasswordResetTokenParams{UserID: id, TokenHash: tokenHash})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !expires.After(now)) {
		return ErrInvalidResetToken
	}
	if err != nil {
		return err
	}
	if err := q.ReplaceAccountPassword(ctx, db.ReplaceAccountPasswordParams{ID: id, PasswordHash: passwordHash}); err != nil {
		return err
	}
	if err := q.DeletePasswordResetTokens(ctx, id); err != nil {
		return err
	}
	if err := q.DeletePasswordResetMail(ctx, id); err != nil {
		return err
	}
	if err := q.DeleteAccountSessions(ctx, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *repository) DeleteMe(ctx context.Context, userID, password string) error {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := r.q.WithTx(tx)
	hash, err := q.LockAccount(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSessionExpired
	}
	if err != nil {
		return err
	}
	if !coreauth.Verify(password, hash) {
		return ErrWrongPassword
	}
	if affected, err := q.DeleteAccount(ctx, userID); err != nil {
		return err
	} else if affected != 1 {
		return ErrSessionExpired
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// The cascade queues every photo durably. A storage error must not become a confirmed purge.
	if err := photos.DrainCleanup(ctx, r.dbPool, r.storage, userID, 0); err != nil {
		return errors.Join(ErrDeletionPending, err)
	}
	return nil
}

func (r *repository) DeliverResetMail(ctx context.Context, deliver func(context.Context, string, []byte) error) (bool, error) {
	// Claim commits before SMTP. Neither account nor outbox row locks span network I/O.
	message, err := r.q.ClaimPasswordResetMail(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	deliveryError := deliver(ctx, message.NormalizedEmail, message.EncryptedToken)
	if deliveryError == nil {
		err = r.q.AcknowledgePasswordResetMail(ctx, db.AcknowledgePasswordResetMailParams{UserID: message.UserID, Version: message.Version})
	} else {
		err = r.q.DeferPasswordResetMail(ctx, db.DeferPasswordResetMailParams{UserID: message.UserID, Version: message.Version, DelaySeconds: 30 * (1 << (message.Attempts - 1))})
	}
	if err != nil {
		return false, err
	}
	return true, deliveryError
}
