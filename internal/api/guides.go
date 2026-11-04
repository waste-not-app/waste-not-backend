package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/database"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

const guideColumns = `id::text,slug,name,category,keywords,recommended_method,summary,steps,cautions,source_url,reviewed_at::text,reviewer,publication_status`

func scanGuide(row pgx.Row) (database.Guide, error) {
	var g database.Guide
	err := row.Scan(&g.ID, &g.Slug, &g.Name, &g.Category, &g.Keywords, &g.RecommendedMethod, &g.Summary, &g.Steps, &g.Cautions, &g.SourceURL, &g.ReviewedAt, &g.Reviewer, &g.PublicationStatus)
	return g, err
}
func (s *Server) getGuide(w http.ResponseWriter, r *http.Request) {
	if !auth.UUID(r.PathValue("id")) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	g, err := scanGuide(s.DB.QueryRow(r.Context(), `SELECT `+guideColumns+` FROM waste_guides WHERE id=$1 AND publication_status='PUBLISHED'`, r.PathValue("id")))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	data(w, r, 200, g)
}
func (s *Server) listGuides(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	category := q.Get("category")
	n, ok := limit(q)
	if !ok || utf8.RuneCountInString(query) > 100 || (category != "" && !domain.Contains(domain.Categories, category)) {
		fail(w, r, 422, "VALIDATION_ERROR", map[string]string{"q": "Periksa pencarian, kategori, dan batas halaman."})
		return
	}
	sc := scope(r, "guides")
	name, id := "", ""
	if raw := q.Get("cursor"); raw != "" {
		c, e := s.decodeCursor(raw, sc)
		if e != nil {
			fail(w, r, 400, "INVALID_REQUEST", nil)
			return
		}
		name, id = c.Position, c.ID
	}
	rows, err := s.DB.Query(r.Context(), `SELECT `+guideColumns+` FROM waste_guides WHERE publication_status='PUBLISHED' AND ($1='' OR category=$1) AND ($2='' OR strpos(lower(name||' '||array_to_string(keywords,' ')),lower($2))>0) AND ($3='' OR (name,id)>($3,NULLIF($4,'')::uuid)) ORDER BY name,id LIMIT $5`, category, query, name, id, n+1)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	list := []database.Guide{}
	for rows.Next() {
		g, e := scanGuide(rows)
		if e != nil {
			s.internal(w, r, e)
			return
		}
		list = append(list, g)
	}
	if err = rows.Err(); err != nil {
		s.internal(w, r, err)
		return
	}
	more := len(list) > n
	var next any
	if more {
		list = list[:n]
		last := list[len(list)-1]
		next = s.encodeCursor(cursor{sc, last.Name, last.ID})
	}
	respond(w, 200, map[string]any{"data": list, "page": map[string]any{"has_more": more, "next_cursor": next}})
}
