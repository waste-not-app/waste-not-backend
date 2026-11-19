package photos

import (
	"context"
	"errors"
	"path"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/database/db"
)

type Record struct {
	ID        string
	UserID    string
	ObjectKey string
	MimeType  string
	SizeBytes int
	Status    string
	CreatedAt time.Time
}

type Repository interface {
	Create(ctx context.Context, id, userID, objectKey, mimeType string, sizeBytes int, put func() error) (Record, error)
	GetActive(ctx context.Context, id, userID string) (Record, error)
	DeleteIfUnused(ctx context.Context, id, userID string) (string, error)
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

func (r *repository) Create(ctx context.Context, id, userID, objectKey, mimeType string, sizeBytes int, put func() error) (Record, error) {
	if path.Dir(objectKey) != userID || path.Clean(objectKey) != objectKey {
		return Record{}, errors.New("photo object key does not belong to owner")
	}
	intent, err := r.dbPool.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer intent.Rollback(ctx)
	intentQueries := r.q.WithTx(intent)
	if _, err := intentQueries.LockAccount(ctx, userID); err != nil {
		return Record{}, err
	}
	if err := intentQueries.QueuePhotoUploadIntent(ctx, objectKey); err != nil {
		return Record{}, err
	}
	if err := intent.Commit(ctx); err != nil {
		return Record{}, err
	}

	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	q := r.q.WithTx(tx)
	// Deletion takes this account lock; cleanup takes the intent lock. Both cover the file write.
	if _, err := q.LockAccount(ctx, userID); err != nil {
		return Record{}, err
	}
	if _, err := q.LockPhotoUploadIntent(ctx, objectKey); err != nil {
		return Record{}, err
	}
	if err := put(); err != nil {
		return Record{}, err
	}
	row, err := q.CreatePhoto(ctx, db.CreatePhotoParams{
		ID:        id,
		UserID:    userID,
		ObjectKey: objectKey,
		MimeType:  mimeType,
		SizeBytes: sizeBytes,
	})
	if err != nil {
		return Record{}, err
	}
	if err := q.DeletePhotoCleanupJob(ctx, objectKey); err != nil {
		return Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	return Record{
		ID:        row.ID,
		UserID:    row.UserID,
		ObjectKey: row.ObjectKey,
		MimeType:  row.MimeType,
		SizeBytes: row.SizeBytes,
		Status:    row.Status,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (r *repository) GetActive(ctx context.Context, id, userID string) (Record, error) {
	row, err := r.q.GetActivePhotoByID(ctx, db.GetActivePhotoByIDParams{
		ID:     id,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrPhotoNotFound
	}
	if err != nil {
		return Record{}, err
	}
	return Record{
		ID:        row.ID,
		UserID:    row.UserID,
		ObjectKey: row.ObjectKey,
		MimeType:  row.MimeType,
		SizeBytes: row.SizeBytes,
		Status:    row.Status,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (r *repository) DeleteIfUnused(ctx context.Context, id, userID string) (string, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	row, err := qtx.LockPhotoForUpdate(ctx, db.LockPhotoForUpdateParams{
		ID:     id,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPhotoNotFound
	}
	if err != nil {
		return "", err
	}
	if row.Deleted || row.Status == "DELETED" {
		if err = tx.Commit(ctx); err != nil {
			return "", err
		}
		return row.ObjectKey, nil
	}
	inUse, err := qtx.IsPhotoAttachedToOtherActiveLog(ctx, db.IsPhotoAttachedToOtherActiveLogParams{
		PhotoID: &id,
		ID:      "00000000-0000-0000-0000-000000000000",
	})
	if err != nil {
		return "", err
	}
	if inUse || row.Status == "ATTACHED" {
		return "", ErrPhotoInUse
	}
	if err = qtx.MarkPhotoDeleted(ctx, db.MarkPhotoDeletedParams{
		ID:     id,
		UserID: userID,
	}); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return row.ObjectKey, nil
}
