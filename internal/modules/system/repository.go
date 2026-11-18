package system

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/database"
	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
)

type Repository interface {
	IsSchemaMigrated(ctx context.Context) (bool, error)
}

type repository struct {
	q *db.Queries
}

func NewRepository(dbPool *pgxpool.Pool) Repository {
	return &repository{q: db.New(dbPool)}
}

func (r *repository) IsSchemaMigrated(ctx context.Context) (bool, error) {
	return r.q.IsSchemaMigrated(ctx, database.LatestMigration())
}
