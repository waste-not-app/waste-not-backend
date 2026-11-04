package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

const logColumns = `id::text,category,method,quantity_kind,weight_g,item_count,activity_date::text,activity_timezone,notes,version,created_at,updated_at`

func scanLog(row pgx.Row) (domain.Log, error) {
	var l domain.Log
	err := row.Scan(&l.ID, &l.Category, &l.Method, &l.QuantityKind, &l.WeightG, &l.ItemCount, &l.ActivityDate, &l.ActivityTimezone, &l.Notes, &l.Version, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}
func (s *Server) createLog(w http.ResponseWriter, r *http.Request) {
	var in domain.LogInput
	if !decode(w, r, &in) {
		return
	}
	if !auth.UUID(in.ClientRequestID) {
		fail(w, r, 422, "VALIDATION_ERROR", map[string]string{"client_request_id": "Gunakan UUID baru untuk draf baru."})
		return
	}
	in.Notes = strings.TrimSpace(in.Notes)
	raw, _ := json.Marshal(in)
	hash := auth.Digest(raw)
	u := who(r).User
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, u.ID+":"+in.ClientRequestID); err != nil {
		s.internal(w, r, err)
		return
	}
	var existing, oldHash string
	var deleted bool
	err = tx.QueryRow(ctx, `SELECT id::text,request_hash,deleted_at IS NOT NULL FROM waste_logs WHERE user_id=$1 AND client_request_id=$2`, u.ID, in.ClientRequestID).Scan(&existing, &oldHash, &deleted)
	if err == nil {
		if oldHash != hash {
			fail(w, r, 409, "IDEMPOTENCY_CONFLICT", nil)
			return
		}
		if deleted {
			fail(w, r, 410, "LOG_DELETED", nil)
			return
		}
		l, e := scanLog(tx.QueryRow(ctx, `SELECT `+logColumns+` FROM waste_logs WHERE id=$1 AND user_id=$2`, existing, u.ID))
		if e != nil {
			s.internal(w, r, e)
			return
		}
		s.logResponse(w, r, tx, 200, l)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		s.internal(w, r, err)
		return
	}
	if f := in.Validate(domain.Today(s.Now(), u.Timezone), ""); len(f) > 0 {
		fail(w, r, 422, "VALIDATION_ERROR", f)
		return
	}
	if in.AIProvenance != nil {
		if !s.validateProvenance(w, r, tx, in) {
			return
		}
	}
	id := uuid.NewString()
	l, err := scanLog(tx.QueryRow(ctx, `INSERT INTO waste_logs(id,user_id,category,method,quantity_kind,weight_g,item_count,activity_date,activity_timezone,notes,client_request_id,request_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+logColumns, id, u.ID, in.Category, in.Method, in.QuantityKind, in.WeightG, in.ItemCount, in.ActivityDate, u.Timezone, in.Notes, in.ClientRequestID, hash))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if in.AIProvenance != nil {
		err = s.linkProvenance(ctx, tx, u.ID, id, in)
		if duplicate(err) {
			fail(w, r, 409, "AI_ALREADY_LINKED", nil)
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
	}
	s.logResponse(w, r, tx, 201, l)
}
func (s *Server) logResponse(w http.ResponseWriter, r *http.Request, tx pgx.Tx, status int, l domain.Log) {
	u := who(r).User
	p, err := progress(r.Context(), tx, u.ID, domain.Today(s.Now(), u.Timezone))
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	data(w, r, status, struct {
		domain.Log
		Progress domain.Progress `json:"progress"`
	}{l, p})
}
func (s *Server) getLog(w http.ResponseWriter, r *http.Request) {
	if !auth.UUID(r.PathValue("id")) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	l, err := scanLog(s.DB.QueryRow(r.Context(), `SELECT `+logColumns+` FROM waste_logs WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, r.PathValue("id"), who(r).User.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	data(w, r, 200, l)
}
func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	category, method, from, to, ok := filters(w, r, s.Now())
	if !ok {
		return
	}
	n, ok := limit(r.URL.Query())
	if !ok {
		fail(w, r, 422, "VALIDATION_ERROR", map[string]string{"limit": "Gunakan 1–100."})
		return
	}
	date, id := "", ""
	sc := scope(r, "logs")
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		c, e := s.decodeCursor(raw, sc)
		if e != nil {
			fail(w, r, 400, "INVALID_REQUEST", nil)
			return
		}
		if _, e = mustParseDate(c.Position); e != nil {
			fail(w, r, 400, "INVALID_REQUEST", nil)
			return
		}
		date, id = c.Position, c.ID
	}
	rows, err := s.DB.Query(r.Context(), `SELECT `+logColumns+` FROM waste_logs WHERE user_id=$1 AND deleted_at IS NULL AND ($2='' OR category=$2) AND ($3='' OR method=$3) AND ($4='' OR activity_date>=NULLIF($4,'')::date) AND ($5='' OR activity_date<=NULLIF($5,'')::date) AND ($6='' OR (activity_date,id)<(NULLIF($6,'')::date,NULLIF($7,'')::uuid)) ORDER BY activity_date DESC,id DESC LIMIT $8`, who(r).User.ID, category, method, from, to, date, id, n+1)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	logs := []domain.Log{}
	for rows.Next() {
		l, e := scanLog(rows)
		if e != nil {
			s.internal(w, r, e)
			return
		}
		logs = append(logs, l)
	}
	if err = rows.Err(); err != nil {
		s.internal(w, r, err)
		return
	}
	more := len(logs) > n
	var next any
	if more {
		logs = logs[:n]
		last := logs[len(logs)-1]
		next = s.encodeCursor(cursor{sc, last.ActivityDate, last.ID})
	}
	respond(w, 200, map[string]any{"data": logs, "page": map[string]any{"has_more": more, "next_cursor": next}})
}
func (s *Server) updateLog(w http.ResponseWriter, r *http.Request) {
	if !auth.UUID(r.PathValue("id")) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	var patch map[string]json.RawMessage
	if !decode(w, r, &patch) {
		return
	}
	version := 0
	if e := json.Unmarshal(patch["version"], &version); e != nil || version < 1 {
		fail(w, r, 422, "VALIDATION_ERROR", map[string]string{"version": "Muat versi catatan terlebih dahulu."})
		return
	}
	for key := range patch {
		if !domain.Contains([]string{"version", "category", "method", "quantity_kind", "weight_g", "item_count", "activity_date", "notes"}, key) {
			fail(w, r, 400, "INVALID_REQUEST", nil)
			return
		}
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	u := who(r).User
	old, err := scanLog(tx.QueryRow(ctx, `SELECT `+logColumns+` FROM waste_logs WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, r.PathValue("id"), u.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if old.Version != version {
		fail(w, r, 409, "VERSION_CONFLICT", nil)
		return
	}
	in := domain.LogInput{Category: old.Category, Method: old.Method, QuantityKind: old.QuantityKind, WeightG: old.WeightG, ItemCount: old.ItemCount, ActivityDate: old.ActivityDate, Notes: old.Notes}
	targets := map[string]any{"category": &in.Category, "method": &in.Method, "quantity_kind": &in.QuantityKind, "weight_g": &in.WeightG, "item_count": &in.ItemCount, "activity_date": &in.ActivityDate, "notes": &in.Notes}
	for key, target := range targets {
		if raw, exists := patch[key]; exists {
			if string(raw) == "null" {
				switch key {
				case "notes":
					in.Notes = ""
					continue
				case "weight_g", "item_count":
				default:
					fail(w, r, 422, "VALIDATION_ERROR", map[string]string{key: "Isian wajib diisi."})
					return
				}
			}
			if err = json.Unmarshal(raw, target); err != nil {
				fail(w, r, 400, "INVALID_REQUEST", nil)
				return
			}
		}
	}
	if f := in.Validate(domain.Today(s.Now(), u.Timezone), old.ActivityDate); len(f) > 0 {
		fail(w, r, 422, "VALIDATION_ERROR", f)
		return
	}
	l, err := scanLog(tx.QueryRow(ctx, `UPDATE waste_logs SET category=$1,method=$2,quantity_kind=$3,weight_g=$4,item_count=$5,activity_date=$6,notes=$7,version=version+1,updated_at=now() WHERE id=$8 AND user_id=$9 AND version=$10 AND deleted_at IS NULL RETURNING `+logColumns, in.Category, in.Method, in.QuantityKind, in.WeightG, in.ItemCount, in.ActivityDate, in.Notes, old.ID, u.ID, version))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.logResponse(w, r, tx, 200, l)
}
func (s *Server) deleteLog(w http.ResponseWriter, r *http.Request) {
	if !auth.UUID(r.PathValue("id")) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	header := r.Header.Get("If-Match")
	if header == "" {
		fail(w, r, 428, "PRECONDITION_REQUIRED", nil)
		return
	}
	version, e := strconv.Atoi(strings.Trim(header, "\""))
	if e != nil || version < 1 || header != "\""+strconv.Itoa(version)+"\"" {
		fail(w, r, 400, "INVALID_REQUEST", nil)
		return
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	var current int
	var deleted bool
	err = tx.QueryRow(ctx, `SELECT version,deleted_at IS NOT NULL FROM waste_logs WHERE id=$1 AND user_id=$2 FOR UPDATE`, r.PathValue("id"), who(r).User.ID).Scan(&current, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !deleted && current != version {
		fail(w, r, 409, "VERSION_CONFLICT", nil)
		return
	}
	if !deleted {
		_, err = tx.Exec(ctx, `UPDATE waste_logs SET deleted_at=now(),updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$2 AND version=$3`, r.PathValue("id"), who(r).User.ID, version)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(204)
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
