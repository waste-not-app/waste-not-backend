package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/waste-not-app/waste-not-backend/internal/ai"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

type Server struct {
	DB         *pgxpool.Pool
	Config     config.Config
	Signer     auth.Signer
	AI         *ai.Client
	Now        func() time.Time
	Log        *slog.Logger
	hashSlots  chan struct{}
	imageSlots chan struct{}
	dummyHash  string
}
type user struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Email    string           `json:"email"`
	Timezone string           `json:"timezone"`
	Progress *domain.Progress `json:"progress,omitempty"`
}
type identity struct {
	User       user
	SessionID  string
	Generation int
}
type contextKey int

const identityKey contextKey = 1
const requestIDKey contextKey = 2

func New(db *pgxpool.Pool, c config.Config, log *slog.Logger) (*Server, error) {
	a, err := ai.New(c)
	if err != nil {
		return nil, err
	}
	dummy, err := auth.Hash(uuid.NewString())
	if err != nil {
		return nil, err
	}
	return &Server{db, c, auth.Signer{Secret: c.Secret, Issuer: c.Issuer, Audience: c.Audience}, a, time.Now, log, make(chan struct{}, 4), make(chan struct{}, 2), dummy}, nil
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]bool{"live": true}) })
	m.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if s.Config.Release != "" {
			w.Header().Set("X-WasteNot-Release", s.Config.Release)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		var ready bool
		err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='001_core.sql')`).Scan(&ready)
		if err != nil || !ready {
			fail(w, r, 503, "UNAVAILABLE", nil)
			return
		}
		respond(w, 200, map[string]bool{"ready": true})
	})
	m.HandleFunc("POST /v1/auth/register", s.register)
	m.HandleFunc("POST /v1/auth/login", s.login)
	m.HandleFunc("POST /v1/auth/refresh", s.refresh)
	routes := map[string]http.HandlerFunc{
		"POST /v1/auth/logout": s.logout, "GET /v1/me": s.me, "GET /v1/metadata": s.metadata,
		"GET /v1/waste-logs": s.listLogs, "POST /v1/waste-logs": s.createLog, "GET /v1/waste-logs/{id}": s.getLog, "PATCH /v1/waste-logs/{id}": s.updateLog, "DELETE /v1/waste-logs/{id}": s.deleteLog,
		"GET /v1/dashboard": s.dashboard, "GET /v1/statistics": s.statistics,
		"GET /v1/waste-guide": s.listGuides, "GET /v1/waste-guide/{id}": s.getGuide,
		"GET /v1/ai/objects": s.aiObjects, "POST /v1/ai/detections": s.aiDetection, "POST /v1/ai/recommendations": s.aiRecommendation, "DELETE /v1/ai/detections/{id}": s.aiDelete,
	}
	for route, h := range routes {
		m.Handle(route, s.authorize(h))
	}
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
			probe := r.Clone(r.Context())
			probe.Method = method
			if _, pattern := m.Handler(probe); pattern != "/" {
				w.Header().Set("Allow", method)
				fail(w, r, 405, "METHOD_NOT_ALLOWED", nil)
				return
			}
		}
		fail(w, r, 404, "NOT_FOUND", nil)
	})
	return s.middleware(m)
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (w *recorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *recorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		rw := &recorder{ResponseWriter: w}
		started := time.Now()
		defer func() {
			if recover() != nil {
				if rw.status == 0 {
					fail(rw, r, 500, "INTERNAL_ERROR", nil)
				}
				s.Log.Error("request panic", "request_id", id)
			}
			s.Log.Info("request", "request_id", id, "status", rw.status, "duration_ms", time.Since(started).Milliseconds())
		}()
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		next.ServeHTTP(rw, r)
	})
}
func (s *Server) authorize(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Authorization")
		if !strings.HasPrefix(raw, "Bearer ") || len(raw) > 4096 {
			fail(w, r, 401, "UNAUTHENTICATED", nil)
			return
		}
		c, err := s.Signer.Parse(strings.TrimPrefix(raw, "Bearer "))
		if err != nil {
			fail(w, r, 401, "SESSION_EXPIRED", nil)
			return
		}
		var u user
		var generation int
		var revoked bool
		var expires time.Time
		err = s.DB.QueryRow(r.Context(), `SELECT u.id::text,u.name,u.normalized_email,u.timezone,s.generation,s.revoked_at IS NOT NULL,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=$1 AND s.user_id=$2`, c.SessionID, c.Subject).Scan(&u.ID, &u.Name, &u.Email, &u.Timezone, &generation, &revoked, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, r, 401, "SESSION_EXPIRED", nil)
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		if !expires.After(s.Now()) || ((revoked || generation != c.Generation) && r.URL.Path != "/v1/auth/logout") {
			fail(w, r, 401, "SESSION_EXPIRED", nil)
			return
		}
		if ok, err := s.limit(r, "api:"+u.ID, 300, time.Minute); err != nil {
			s.internal(w, r, err)
			return
		} else if !ok {
			limited(w, r, 60)
			return
		}
		ctx := context.WithValue(r.Context(), identityKey, identity{u, c.SessionID, c.Generation})
		next(w, r.WithContext(ctx))
	})
}
func who(r *http.Request) identity     { return r.Context().Value(identityKey).(identity) }
func requestID(r *http.Request) string { v, _ := r.Context().Value(requestIDKey).(string); return v }
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status != 204 {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func data(w http.ResponseWriter, r *http.Request, status int, v any) {
	respond(w, status, map[string]any{"data": v, "request_id": requestID(r), "api_version": "1.0.0"})
}
func fail(w http.ResponseWriter, r *http.Request, status int, code string, fields map[string]string) {
	if fields == nil {
		fields = map[string]string{}
	}
	message := "Permintaan tidak dapat diproses."
	switch {
	case status == 401:
		message = "Silakan masuk kembali."
	case status == 404:
		message = "Data tidak ditemukan."
	case status == 422:
		message = "Periksa kembali isianmu."
	case status >= 500:
		message = "Layanan belum tersedia. Coba lagi nanti."
	}
	respond(w, status, map[string]any{"api_version": "1.0.0", "request_id": requestID(r), "error": map[string]any{"code": code, "message": message, "fields": fields, "retryable": status == 429 || status >= 500}})
}
func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("database operation failed", "request_id", requestID(r))
	fail(w, r, 503, "UNAVAILABLE", nil)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", nil)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	d.DisallowUnknownFields()
	err = d.Decode(v)
	if err == nil {
		if e := d.Decode(new(any)); e != io.EOF {
			err = errors.New("trailing JSON")
		}
	}
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			fail(w, r, 413, "REQUEST_TOO_LARGE", nil)
		} else {
			fail(w, r, 400, "INVALID_REQUEST", nil)
		}
		return false
	}
	return true
}
func limited(w http.ResponseWriter, r *http.Request, seconds int) {
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	fail(w, r, 429, "RATE_LIMITED", nil)
}
func (s *Server) limit(r *http.Request, key string, n int, window time.Duration) (bool, error) {
	bucket := s.Now().UTC().Truncate(window)
	var hits int
	err := s.DB.QueryRow(r.Context(), `INSERT INTO rate_limits(key,bucket,hits) VALUES($1,$2,1) ON CONFLICT(key,bucket) DO UPDATE SET hits=LEAST(rate_limits.hits+1,$3+1) RETURNING hits`, auth.Digest([]byte(key)), bucket, n).Scan(&hits)
	return hits <= n, err
}
func (s *Server) authLimit(w http.ResponseWriter, r *http.Request, email string) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	for _, key := range []string{"auth-ip:" + host, "auth-email:" + email} {
		ok, e := s.limit(r, key, 20, 15*time.Minute)
		if e != nil {
			s.internal(w, r, e)
			return false
		}
		if !ok {
			limited(w, r, 900)
			return false
		}
	}
	return true
}
func (s *Server) hashSlot(w http.ResponseWriter, r *http.Request) bool {
	select {
	case s.hashSlots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		fail(w, r, 503, "UNAVAILABLE", nil)
		return false
	}
}
