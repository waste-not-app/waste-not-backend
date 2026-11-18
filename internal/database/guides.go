package database

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

type Guide struct {
	ID                string   `json:"id"`
	Slug              string   `json:"slug"`
	Name              string   `json:"name"`
	Category          string   `json:"category"`
	Keywords          []string `json:"keywords"`
	RecommendedMethod string   `json:"recommended_method"`
	Summary           string   `json:"summary"`
	Steps             []string `json:"steps"`
	Cautions          []string `json:"cautions"`
	SourceURL         string   `json:"source_url"`
	ReviewedAt        string   `json:"reviewed_at"`
	Reviewer          string   `json:"reviewer"`
	PublicationStatus string   `json:"publication_status"`
}

func ImportGuides(ctx context.Context, db *pgxpool.Pool, guides []Guide) error {
	for _, g := range guides {
		u, e := url.Parse(g.SourceURL)
		_, dateErr := time.Parse(time.DateOnly, g.ReviewedAt)
		id, idErr := uuid.Parse(g.ID)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || dateErr != nil || g.ReviewedAt > time.Now().UTC().Format(time.DateOnly) || idErr != nil || id == uuid.Nil || g.Slug == "" || strings.TrimSpace(g.Name) == "" || g.Summary == "" || len(g.Steps) == 0 || g.Cautions == nil || strings.TrimSpace(g.Reviewer) == "" || !domain.Contains(domain.Categories, g.Category) || !domain.Contains(domain.Methods, g.RecommendedMethod) || !domain.Contains([]string{"PUBLISHED", "DRAFT", "ARCHIVED"}, g.PublicationStatus) {
			return errors.New("invalid guide: provide a UUID, reviewed content, real source, reviewer, date, enum fields and arrays")
		}
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for _, g := range guides {
		steps, _ := json.Marshal(g.Steps)
		cautions, _ := json.Marshal(g.Cautions)
		if g.Keywords == nil {
			g.Keywords = []string{}
		}
		_, e = tx.Exec(ctx, `INSERT INTO waste_guides(id,slug,name,category,keywords,recommended_method,summary,steps,cautions,source_url,reviewed_at,reviewer,publication_status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(id) DO UPDATE SET slug=EXCLUDED.slug,name=EXCLUDED.name,category=EXCLUDED.category,keywords=EXCLUDED.keywords,recommended_method=EXCLUDED.recommended_method,summary=EXCLUDED.summary,steps=EXCLUDED.steps,cautions=EXCLUDED.cautions,source_url=EXCLUDED.source_url,reviewed_at=EXCLUDED.reviewed_at,reviewer=EXCLUDED.reviewer,publication_status=EXCLUDED.publication_status`, g.ID, g.Slug, g.Name, g.Category, g.Keywords, g.RecommendedMethod, g.Summary, steps, cautions, g.SourceURL, g.ReviewedAt, g.Reviewer, g.PublicationStatus)
		if e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
