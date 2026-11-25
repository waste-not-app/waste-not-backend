package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/database"
	"github.com/waste-not-app/waste-not-backend/internal/modules/auth"
)

func setup(t *testing.T, change func(*config.Config), mailers ...auth.ResetMailer) (*Server, *httptest.Server) {
	t.Helper()
	return setupWithGoogleVerifier(t, change, nil, mailers...)
}

func setupWithGoogleVerifier(t *testing.T, change func(*config.Config), google auth.GoogleVerifier, mailers ...auth.ResetMailer) (*Server, *httptest.Server) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := database.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	if e = database.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	if e = database.Migrate(ctx, db); e != nil {
		t.Fatal("migration rerun", e)
	}
	c := config.Config{Secret: []byte(strings.Repeat("s", 32)), Issuer: "wastenot-api", Audience: "wastenot-android"}
	if change != nil {
		change(&c)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var s *Server
	if google != nil {
		var mailer auth.ResetMailer
		if len(mailers) > 0 {
			mailer = mailers[0]
		}
		s, e = newServerWithGoogle(db, c, log, mailer, google)
	} else if len(mailers) > 0 {
		s, e = newServer(db, c, log, mailers[0])
	} else {
		s, e = New(db, c, log)
	}
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	return s, server
}
func TestReadinessIdentifiesRelease(t *testing.T) {
	_, server := setup(t, func(c *config.Config) { c.Release = strings.Repeat("a", 40) })
	status, body, headers := call(t, server.URL, "GET", "/health/ready", "", nil, nil)
	if status != 200 || body["ready"] != true || headers.Get("X-WasteNot-Release") != strings.Repeat("a", 40) {
		t.Fatalf("readiness: %d %v %v", status, body, headers)
	}
}

func call(t *testing.T, base, method, path, token string, body any, headers map[string]string) (int, map[string]any, http.Header) {
	t.Helper()
	var raw []byte
	var e error
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
	}
	req, e := http.NewRequest(method, base+path, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	v := map[string]any{}
	if resp.StatusCode != 204 {
		if e = json.NewDecoder(resp.Body).Decode(&v); e != nil {
			t.Fatal("invalid response", resp.StatusCode, e)
		}
	}
	checkCoreResponse(t, method, path, resp.StatusCode, v)
	return resp.StatusCode, v, resp.Header
}
func requireStatus(t *testing.T, got, want int, v any) {
	t.Helper()
	if got != want {
		t.Fatalf("status=%d want=%d body=%v", got, want, v)
	}
}
func registerUser(t *testing.T, base, email string) map[string]any {
	t.Helper()
	st, v, _ := call(t, base, "POST", "/v1/auth/register", "", map[string]any{"name": "Pengguna Tes", "email": email, "password": "strong-passphrase-123", "timezone": "Asia/Jakarta"}, nil)
	requireStatus(t, st, 201, v)
	return v["data"].(map[string]any)
}
func logInput(date string) map[string]any {
	return map[string]any{"client_request_id": uuid.NewString(), "category": "PLASTIC", "method": "RECYCLE", "quantity_kind": "WEIGHT", "weight_g": 250, "item_count": nil, "activity_date": date, "notes": ""}
}
func TestCoreIntegration(t *testing.T) {
	s, sv := setup(t, nil)
	a := registerUser(t, sv.URL, "a@example.com")
	b := registerUser(t, sv.URL, "b@example.com")
	token := a["access_token"].(string)
	other := b["access_token"].(string)
	today := time.Now().In(mustZone("Asia/Jakarta")).Format(time.DateOnly)
	st, v, h := call(t, sv.URL, "GET", "/v1/metadata", token, nil, nil)
	requireStatus(t, st, 200, v)
	if h.Get("Cache-Control") != "no-store" || h.Get("X-Request-ID") == "" {
		t.Fatal("privacy headers missing")
	}
	if v["data"].(map[string]any)["capabilities"].(map[string]any)["ai_scan"] != false {
		t.Fatal("AI capability")
	}
	st, v, _ = call(t, sv.URL, "GET", "/v1/metadata", "", nil, nil)
	requireStatus(t, st, 401, v)
	in := logInput(today)
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
	requireStatus(t, st, 201, v)
	id := v["data"].(map[string]any)["id"].(string)
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
	requireStatus(t, st, 200, v)
	if v["data"].(map[string]any)["progress"].(map[string]any)["xp"] != float64(10) {
		t.Fatal("replay doubled XP")
	}
	in["weight_g"] = 251
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
	requireStatus(t, st, 409, v)
	in["weight_g"] = 250
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		var body any
		if method == "PATCH" {
			body = map[string]any{"version": 1, "notes": "attack"}
		}
		st, v, _ = call(t, sv.URL, method, "/v1/waste-logs/"+id, other, body, map[string]string{"If-Match": "\"1\""})
		requireStatus(t, st, 404, v)
	}
	st, v, _ = call(t, sv.URL, "PATCH", "/v1/waste-logs/"+id, token, map[string]any{"version": 1, "weight_g": 300, "notes": nil}, nil)
	requireStatus(t, st, 200, v)
	st, v, _ = call(t, sv.URL, "PATCH", "/v1/waste-logs/"+id, token, map[string]any{"version": 1, "weight_g": 400}, nil)
	requireStatus(t, st, 409, v)
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/waste-logs/"+id, token, nil, nil)
	requireStatus(t, st, 428, v)
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/waste-logs/"+id, token, nil, map[string]string{"If-Match": "\"1\""})
	requireStatus(t, st, 409, v)
	for range 2 {
		st, v, _ = call(t, sv.URL, "DELETE", "/v1/waste-logs/"+id, token, nil, map[string]string{"If-Match": "\"2\""})
		requireStatus(t, st, 204, v)
	}
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
	requireStatus(t, st, 410, v)
	st, v, _ = call(t, sv.URL, "GET", "/v1/me", token, nil, nil)
	requireStatus(t, st, 200, v)
	if v["data"].(map[string]any)["progress"].(map[string]any)["xp"] != float64(0) {
		t.Fatal("delete XP")
	}
	t.Run("parallel idempotency", func(t *testing.T) {
		input := logInput(today)
		const jobs = 8
		var wg sync.WaitGroup
		codes := make(chan int, jobs)
		ids := make(chan string, jobs)
		for range jobs {
			wg.Go(func() {
				st, v, _ := call(t, sv.URL, "POST", "/v1/waste-logs", token, input, nil)
				codes <- st
				if data, ok := v["data"].(map[string]any); ok {
					ids <- data["id"].(string)
				}
			})
		}
		wg.Wait()
		close(codes)
		close(ids)
		created := 0
		for status := range codes {
			if status == 201 {
				created++
			} else if status != 200 {
				t.Fatalf("concurrent status %d", status)
			}
		}
		if created != 1 {
			t.Fatalf("created %d logs", created)
		}
		unique := map[string]bool{}
		for id := range ids {
			unique[id] = true
		}
		if len(unique) != 1 {
			t.Fatal("parallel duplicated log")
		}
	})
	t.Run("validation and immutable fields", func(t *testing.T) {
		for _, change := range []map[string]any{{"category": "UNKNOWN"}, {"method": "BURN"}, {"weight_g": 0}, {"weight_g": 100001}, {"weight_g": 1.5}, {"item_count": 1}, {"activity_date": "2099-01-01"}, {"user_id": a["user"].(map[string]any)["id"]}, {"photo_id": uuid.NewString()}} {
			input := logInput(today)
			for k, v := range change {
				input[k] = v
			}
			st, v, _ := call(t, sv.URL, "POST", "/v1/waste-logs", token, input, nil)
			if st != 400 && st != 422 {
				t.Fatalf("bad validation %v: %d %v", change, st, v)
			}
		}
	})
	t.Run("pagination binding", func(t *testing.T) {
		for range 3 {
			st, v, _ := call(t, sv.URL, "POST", "/v1/waste-logs", token, logInput(today), nil)
			requireStatus(t, st, 201, v)
		}
		st, v, _ := call(t, sv.URL, "GET", "/v1/waste-logs?limit=2", token, nil, nil)
		requireStatus(t, st, 200, v)
		page := v["page"].(map[string]any)
		cursor := page["next_cursor"].(string)
		if page["has_more"] != true {
			t.Fatal("pagination")
		}
		for _, path := range []string{"/v1/waste-logs?limit=2&category=PLASTIC&cursor=" + url.QueryEscape(cursor), "/v1/waste-logs?cursor=" + url.QueryEscape(cursor+"x")} {
			st, v, _ = call(t, sv.URL, "GET", path, token, nil, nil)
			requireStatus(t, st, 400, v)
		}
		st, v, _ = call(t, sv.URL, "GET", "/v1/waste-logs?cursor="+url.QueryEscape(cursor), other, nil, nil)
		requireStatus(t, st, 400, v)
	})
	t.Run("date retained on old edit", func(t *testing.T) {
		input := logInput(today)
		st, v, _ := call(t, sv.URL, "POST", "/v1/waste-logs", token, input, nil)
		requireStatus(t, st, 201, v)
		id := v["data"].(map[string]any)["id"].(string)
		_, e := s.DB.Exec(context.Background(), `UPDATE waste_logs SET activity_date='2020-01-01' WHERE id=$1`, id)
		if e != nil {
			t.Fatal(e)
		}
		st, v, _ = call(t, sv.URL, "PATCH", "/v1/waste-logs/"+id, token, map[string]any{"version": 1, "notes": "corrected old log"}, nil)
		requireStatus(t, st, 200, v)
	})
}
func mustZone(zone string) *time.Location { loc, _ := time.LoadLocation(zone); return loc }
func TestReferenceStatistics(t *testing.T) {
	_, sv := setup(t, nil)
	a := registerUser(t, sv.URL, "stats@example.com")
	token := a["access_token"].(string)
	today := time.Now().In(mustZone("Asia/Jakarta")).Format(time.DateOnly)
	yesterday := mustDate(today).AddDate(0, 0, -1).Format(time.DateOnly)
	for _, c := range []struct {
		date, cat, method, kind string
		value                   int
	}{{today, "PLASTIC", "RECYCLE", "WEIGHT", 250}, {today, "GLASS", "REUSE", "WEIGHT", 400}, {today, "PLASTIC", "REDUCE", "COUNT", 2}, {yesterday, "ORGANIC", "TRASH", "WEIGHT", 600}, {yesterday, "PAPER", "RECYCLE", "WEIGHT", 150}} {
		in := logInput(c.date)
		in["category"] = c.cat
		in["method"] = c.method
		in["quantity_kind"] = c.kind
		if c.kind == "COUNT" {
			in["weight_g"] = nil
			in["item_count"] = c.value
		} else {
			in["weight_g"] = c.value
		}
		st, v, _ := call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
		requireStatus(t, st, 201, v)
	}
	st, v, _ := call(t, sv.URL, "GET", "/v1/statistics", token, nil, nil)
	requireStatus(t, st, 200, v)
	result := v["data"].(map[string]any)
	summary := result["summary"].(map[string]any)
	for key, want := range map[string]float64{"total_handled_weight_g": 1400, "total_handled_count": 0, "avoided_count": 2, "activity_count": 5, "handled_activity_count": 4, "recycling_rate_percent": 50} {
		if summary[key] != want {
			t.Fatalf("%s got %v want %v", key, summary[key], want)
		}
	}
	if len(result["daily"].([]any)) != 7 || len(result["categories"].([]any)) != 7 {
		t.Fatal("zero-filled bins missing")
	}
	st, v, _ = call(t, sv.URL, "GET", "/v1/dashboard", token, nil, nil)
	requireStatus(t, st, 200, v)
	result = v["data"].(map[string]any)
	if result["summary"].(map[string]any)["total_handled_weight_g"] != float64(650) || len(result["recent_logs"].([]any)) != 3 || result["progress"].(map[string]any)["current_streak"] != float64(2) {
		t.Fatal(result)
	}
	st, v, _ = call(t, sv.URL, "GET", "/v1/statistics?from=2020-01-01&to=2022-01-01", token, nil, nil)
	requireStatus(t, st, 422, v)
}
func TestSessionIntegration(t *testing.T) {
	_, sv := setup(t, nil)
	a := registerUser(t, sv.URL, "session@example.com")
	token := a["access_token"].(string)
	refresh := a["refresh_token"].(string)
	st, v, _ := call(t, sv.URL, "POST", "/v1/auth/login", "", map[string]any{"email": "session@example.com", "password": "incorrect-password"}, nil)
	requireStatus(t, st, 401, v)
	st, v, _ = call(t, sv.URL, "POST", "/v1/auth/refresh", "", map[string]string{"refresh_token": refresh}, nil)
	requireStatus(t, st, 200, v)
	next := v["data"].(map[string]any)
	if next["refresh_token"] == refresh {
		t.Fatal("refresh not rotated")
	}
	st, v, _ = call(t, sv.URL, "GET", "/v1/me", token, nil, nil)
	requireStatus(t, st, 401, v)
	newToken := next["access_token"].(string)
	st, v, _ = call(t, sv.URL, "GET", "/v1/me", newToken, nil, nil)
	requireStatus(t, st, 200, v)
	st, v, _ = call(t, sv.URL, "POST", "/v1/auth/refresh", "", map[string]string{"refresh_token": refresh}, nil)
	requireStatus(t, st, 401, v)
	st, v, _ = call(t, sv.URL, "GET", "/v1/me", newToken, nil, nil)
	requireStatus(t, st, 401, v)
	for range 2 {
		st, v, _ = call(t, sv.URL, "POST", "/v1/auth/logout", newToken, nil, nil)
		requireStatus(t, st, 204, v)
	}
	st, v, _ = call(t, sv.URL, "POST", "/v1/auth/login", "", map[string]any{"email": " SESSION@EXAMPLE.COM ", "password": "strong-passphrase-123"}, nil)
	requireStatus(t, st, 200, v)
}
func TestEmptyAndReduceOnly(t *testing.T) {
	_, sv := setup(t, nil)
	a := registerUser(t, sv.URL, "empty@example.com")
	token := a["access_token"].(string)
	for _, path := range []string{"/v1/statistics", "/v1/dashboard"} {
		st, v, _ := call(t, sv.URL, "GET", path, token, nil, nil)
		requireStatus(t, st, 200, v)
		if v["data"].(map[string]any)["summary"].(map[string]any)["recycling_rate_percent"] != nil {
			t.Fatal("empty percentage must be null")
		}
	}
	today := time.Now().In(mustZone("Asia/Jakarta")).Format(time.DateOnly)
	in := logInput(today)
	in["method"] = "REDUCE"
	st, v, _ := call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
	requireStatus(t, st, 201, v)
	st, v, _ = call(t, sv.URL, "GET", "/v1/statistics", token, nil, nil)
	requireStatus(t, st, 200, v)
	summary := v["data"].(map[string]any)["summary"].(map[string]any)
	if summary["recycling_rate_percent"] != nil || summary["total_handled_weight_g"] != float64(0) || summary["avoided_weight_g"] != float64(250) {
		t.Fatal(summary)
	}
}
func TestRateLimitAcrossInstances(t *testing.T) {
	s, sv := setup(t, nil)
	for range 20 {
		call(t, sv.URL, "POST", "/v1/auth/login", "", map[string]string{"email": "limited@example.com", "password": "invalid-password"}, nil)
	}
	second, e := New(s.DB, s.Config, s.Log)
	if e != nil {
		t.Fatal(e)
	}
	sv2 := httptest.NewServer(second.Handler())
	defer sv2.Close()
	st, v, h := call(t, sv2.URL, "POST", "/v1/auth/login", "", map[string]string{"email": "limited@example.com", "password": "invalid-password"}, nil)
	requireStatus(t, st, 429, v)
	if h.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}
func TestMixedHandledUnits(t *testing.T) {
	_, sv := setup(t, nil)
	a := registerUser(t, sv.URL, "mixed@example.com")
	token := a["access_token"].(string)
	today := time.Now().In(mustZone("Asia/Jakarta")).Format(time.DateOnly)
	for _, c := range []struct {
		method, kind string
		value        int
	}{{"RECYCLE", "WEIGHT", 250}, {"TRASH", "COUNT", 20}, {"REDUCE", "COUNT", 2}} {
		in := logInput(today)
		in["method"] = c.method
		in["quantity_kind"] = c.kind
		if c.kind == "COUNT" {
			in["weight_g"] = nil
			in["item_count"] = c.value
		}
		st, v, _ := call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
		requireStatus(t, st, 201, v)
	}
	st, v, _ := call(t, sv.URL, "GET", "/v1/statistics", token, nil, nil)
	requireStatus(t, st, 200, v)
	summary := v["data"].(map[string]any)["summary"].(map[string]any)
	for key, want := range map[string]float64{"total_handled_weight_g": 250, "total_handled_count": 20, "avoided_count": 2, "recycling_rate_percent": 50} {
		if summary[key] != want {
			t.Fatalf("mixed units %s=%v want %v", key, summary[key], want)
		}
	}
}
