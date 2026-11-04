package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Timezone string `json:"timezone"`
	}
	if !decode(w, r, &in) {
		return
	}
	email, valid := domain.Email(in.Email)
	if !s.authLimit(w, r, email) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	f := map[string]string{}
	if !valid {
		f["email"] = "Periksa format email."
	}
	if n := utf8.RuneCountInString(in.Name); n < 2 || n > 60 {
		f["name"] = "Isi nama sepanjang 2–60 karakter."
	}
	if n := utf8.RuneCountInString(in.Password); n < 12 || n > 128 {
		f["password"] = "Gunakan kata sandi 12–128 karakter."
	}
	if !domain.ValidTimezone(in.Timezone) {
		f["timezone"] = "Pilih zona waktu IANA."
	}
	if len(f) > 0 {
		fail(w, r, 422, "VALIDATION_ERROR", f)
		return
	}
	if !s.hashSlot(w, r) {
		return
	}
	hash, err := auth.Hash(in.Password)
	<-s.hashSlots
	if err != nil {
		s.internal(w, r, err)
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	u := user{ID: uuid.NewString(), Name: in.Name, Email: email, Timezone: in.Timezone}
	_, err = tx.Exec(r.Context(), `INSERT INTO users(id,name,normalized_email,password_hash,timezone) VALUES($1,$2,$3,$4,$5)`, u.ID, u.Name, u.Email, hash, u.Timezone)
	if duplicate(err) {
		fail(w, r, 409, "EMAIL_EXISTS", map[string]string{"email": "Email sudah digunakan."})
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	tokens, err := s.newSession(r.Context(), tx, u)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	data(w, r, 201, tokens)
}
func duplicate(err error) bool { var p *pgconn.PgError; return errors.As(err, &p) && p.Code == "23505" }
func (s *Server) newSession(ctx context.Context, tx pgx.Tx, u user) (auth.Tokens, error) {
	now := s.Now().UTC()
	sid := uuid.NewString()
	tokens, err := s.Signer.Issue(u.ID, sid, 1, now.Add(auth.RefreshTTL), now)
	if err != nil {
		return tokens, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sessions(id,user_id,expires_at) VALUES($1,$2,$3)`, sid, u.ID, tokens.RefreshExpiresAt); err != nil {
		return tokens, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO refresh_tokens(token_hash,session_id) VALUES($1,$2)`, auth.Digest([]byte(tokens.RefreshToken)), sid)
	tokens.User = u
	return tokens, err
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	email, valid := domain.Email(in.Email)
	if !s.authLimit(w, r, email) {
		return
	}
	if !valid || utf8.RuneCountInString(in.Password) > 128 {
		fail(w, r, 401, "UNAUTHENTICATED", nil)
		return
	}
	if !s.hashSlot(w, r) {
		return
	}
	defer func() { <-s.hashSlots }()
	var u user
	var hash string
	err := s.DB.QueryRow(r.Context(), `SELECT id::text,name,normalized_email,timezone,password_hash FROM users WHERE normalized_email=$1`, email).Scan(&u.ID, &u.Name, &u.Email, &u.Timezone, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		auth.Verify(in.Password, s.dummyHash)
		fail(w, r, 401, "UNAUTHENTICATED", nil)
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !auth.Verify(in.Password, hash) {
		fail(w, r, 401, "UNAUTHENTICATED", nil)
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	tokens, err := s.newSession(r.Context(), tx, u)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	data(w, r, 200, tokens)
}
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !s.authLimit(w, r, "refresh:"+auth.Digest([]byte(in.RefreshToken))) {
		return
	}
	if len(in.RefreshToken) != 43 {
		fail(w, r, 401, "SESSION_EXPIRED", nil)
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	hash := auth.Digest([]byte(in.RefreshToken))
	var sid, uid string
	var gen int
	var used, revoked bool
	var expires time.Time
	err = tx.QueryRow(r.Context(), `SELECT s.id::text,s.user_id::text,s.generation,s.expires_at,t.used_at IS NOT NULL,s.revoked_at IS NOT NULL FROM refresh_tokens t JOIN sessions s ON s.id=t.session_id WHERE t.token_hash=$1 FOR UPDATE OF s,t`, hash).Scan(&sid, &uid, &gen, &expires, &used, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, 401, "SESSION_EXPIRED", nil)
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if used {
		if _, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1`, sid); err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		fail(w, r, 401, "SESSION_EXPIRED", nil)
		return
	}
	if revoked || !expires.After(s.Now()) {
		fail(w, r, 401, "SESSION_EXPIRED", nil)
		return
	}
	tokens, err := s.Signer.Issue(uid, sid, gen+1, expires, s.Now().UTC())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE refresh_tokens SET used_at=now() WHERE token_hash=$1`, hash); err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET generation=generation+1 WHERE id=$1`, sid)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO refresh_tokens(token_hash,session_id) VALUES($1,$2)`, auth.Digest([]byte(tokens.RefreshToken)), sid)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	data(w, r, 200, tokens)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_, err := s.DB.Exec(r.Context(), `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1 AND user_id=$2`, who(r).SessionID, who(r).User.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := who(r).User
	p, err := progress(r.Context(), s.DB, u.ID, domain.Today(s.Now(), u.Timezone))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	u.Progress = &p
	data(w, r, 200, u)
}
func (s *Server) metadata(w http.ResponseWriter, r *http.Request) {
	var catalogVersion any
	enabled := false
	if s.Config.AIEnabled {
		catalog, err := s.AI.Catalog(r.Context(), requestID(r))
		if err == nil {
			catalogVersion = catalog["catalog_version"]
			enabled = true
		}
	}
	data(w, r, 200, map[string]any{"categories": domain.Categories, "methods": domain.Methods, "limits": map[string]any{"weight_g": map[string]int{"min": 1, "max": 100000}, "item_count": map[string]int{"min": 1, "max": 10000}, "name_length": map[string]int{"min": 2, "max": 60}, "email_max_length": 254, "password_length": map[string]int{"min": 12, "max": 128}, "notes_max_length": 500, "activity_date_past_days": 30, "kg_max_decimal_places": 3}, "capabilities": map[string]bool{"achievements": false, "reminders": false, "photos": false, "recycling_points": false, "ai_scan": enabled, "ai_recommendation": enabled}, "ai_object_catalog_version": catalogVersion})
}
