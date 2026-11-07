package guides

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/waste-not-app/waste-not-backend/internal/database"
	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
)

type Repository interface {
	GetPublishedByID(ctx context.Context, id string) (database.Guide, error)
	ListPublished(ctx context.Context, q ListGuidesQuery) ([]database.Guide, error)
}

type repository struct {
	q *db.Queries
}

func NewRepository(dbPool *pgxpool.Pool) Repository {
	return &repository{q: db.New(dbPool)}
}

func toDatabaseGuide(id, slug, name, category string, keywords []string, method, summary string, stepsRaw, cautionsRaw []byte, sourceURL, reviewedAt, reviewer, pubStatus string) database.Guide {
	steps := []string{}
	cautions := []string{}
	_ = json.Unmarshal(stepsRaw, &steps)
	_ = json.Unmarshal(cautionsRaw, &cautions)
	return database.Guide{
		ID:                id,
		Slug:              slug,
		Name:              name,
		Category:          category,
		Keywords:          keywords,
		RecommendedMethod: method,
		Summary:           summary,
		Steps:             steps,
		Cautions:          cautions,
		SourceURL:         sourceURL,
		ReviewedAt:        reviewedAt,
		Reviewer:          reviewer,
		PublicationStatus: pubStatus,
	}
}

func (r *repository) GetPublishedByID(ctx context.Context, id string) (database.Guide, error) {
	row, err := r.q.GetPublishedGuideByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return database.Guide{}, ErrGuideNotFound
	}
	return toDatabaseGuide(row.ID, row.Slug, row.Name, row.Category, row.Keywords, row.RecommendedMethod, row.Summary, row.Steps, row.Cautions, row.SourceUrl, row.ReviewedAt, row.Reviewer, row.PublicationStatus), err
}

func (r *repository) ListPublished(ctx context.Context, q ListGuidesQuery) ([]database.Guide, error) {
	rows, err := r.q.ListPublishedGuides(ctx, db.ListPublishedGuidesParams{
		Category:  q.Category,
		Query:     q.Query,
		NamePos:   q.NamePos,
		IDPos:     q.IDPos,
		PageLimit: q.Limit + 1,
	})
	if err != nil {
		return nil, err
	}
	list := make([]database.Guide, 0, len(rows))
	for _, row := range rows {
		list = append(list, toDatabaseGuide(row.ID, row.Slug, row.Name, row.Category, row.Keywords, row.RecommendedMethod, row.Summary, row.Steps, row.Cautions, row.SourceUrl, row.ReviewedAt, row.Reviewer, row.PublicationStatus))
	}
	return list, nil
}
