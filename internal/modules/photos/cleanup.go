package photos

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/security"
	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
)

func DrainCleanup(ctx context.Context, pool *pgxpool.Pool, storage Storage, ownerID string, limit int) error {
	prefix := ""
	if ownerID != "" {
		if !security.UUID(ownerID) {
			return errors.New("invalid photo cleanup owner")
		}
		prefix = ownerID + "/"
	}
	var failures []error
	for count := 0; limit == 0 || count < limit; count++ {
		worked, err := cleanupNext(ctx, pool, storage, prefix)
		if err != nil {
			failures = append(failures, err)
		}
		if !worked {
			break
		}
	}
	if prefix != "" {
		pending, err := db.New(pool).HasOwnerPhotoCleanup(ctx, prefix)
		if err != nil {
			failures = append(failures, err)
		} else if pending {
			failures = append(failures, ErrCleanupPending)
		}
	}
	return errors.Join(failures...)
}

func cleanupNext(ctx context.Context, pool *pgxpool.Pool, storage Storage, prefix string) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)
	job, err := q.LockPhotoCleanupJob(ctx, prefix)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var deletionError error
	if storage == nil {
		deletionError = ErrPhotosDisabled
	} else {
		deletionError = storage.Delete(job.ObjectKey)
	}
	if deletionError != nil {
		delaySeconds := 30 * (1 << min(job.Attempts, 6))
		if err := q.DeferPhotoCleanupJob(ctx, db.DeferPhotoCleanupJobParams{ObjectKey: job.ObjectKey, DelaySeconds: delaySeconds}); err != nil {
			return false, err
		}
	} else if err := q.DeletePhotoCleanupJob(ctx, job.ObjectKey); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, deletionError
}
