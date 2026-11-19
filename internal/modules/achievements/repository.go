package achievements

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/database/db"
)

type Repository interface {
	ListForUser(ctx context.Context, userID, today string) ([]AchievementRes, error)
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

func (r *repository) ListForUser(ctx context.Context, userID, today string) ([]AchievementRes, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	items, err := SyncUser(ctx, qtx, userID, today)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}
