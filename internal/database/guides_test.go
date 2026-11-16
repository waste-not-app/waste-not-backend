package database

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

func TestCuratedCoreGuidesDataset(t *testing.T) {
	raw, err := os.ReadFile("../../guides/core-guides-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var guides []Guide
	if err := dec.Decode(&guides); err != nil {
		t.Fatalf("decode curated guides: %v", err)
	}
	if len(guides) < 12 {
		t.Fatalf("expected at least 12 curated guides, got %d", len(guides))
	}

	seenIDs := map[string]bool{}
	seenSlugs := map[string]bool{}
	seenCats := map[string]bool{}
	for _, g := range guides {
		if _, err := uuid.Parse(g.ID); err != nil {
			t.Fatalf("invalid guide id %q: %v", g.ID, err)
		}
		if seenIDs[g.ID] || seenSlugs[g.Slug] {
			t.Fatalf("duplicate guide id or slug: %s / %s", g.ID, g.Slug)
		}
		seenIDs[g.ID] = true
		seenSlugs[g.Slug] = true
		if !domain.Contains(domain.Categories, g.Category) {
			t.Fatalf("invalid category %q", g.Category)
		}
		seenCats[g.Category] = true
		if !domain.Contains(domain.Methods, g.RecommendedMethod) {
			t.Fatalf("invalid method %q", g.RecommendedMethod)
		}
		u, err := url.Parse(g.SourceURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			t.Fatalf("invalid https source_url %q", g.SourceURL)
		}
		if len(g.Steps) == 0 || len(g.Keywords) == 0 || strings.TrimSpace(g.Summary) == "" {
			t.Fatalf("incomplete guide %s", g.Slug)
		}
		switch g.PublicationStatus {
		case "DRAFT", "PUBLISHED", "ARCHIVED":
		default:
			t.Fatalf("invalid publication status %q on %s", g.PublicationStatus, g.Slug)
		}
	}
	for _, cat := range domain.Categories {
		if !seenCats[cat] {
			t.Fatalf("curated guides missing category %s", cat)
		}
	}

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		return
	}
	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	schema := "test_guides_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := db.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")

	pool, err := openSchemaPool(ctx, dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := ImportGuides(ctx, pool, guides); err != nil {
		t.Fatalf("ImportGuides failed: %v", err)
	}
}

func openSchemaPool(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return Open(ctx, u.String())
}
