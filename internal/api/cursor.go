package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

type cursor struct {
	Scope    string `json:"scope"`
	Position string `json:"position"`
	ID       string `json:"id"`
}

func (s *Server) encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.Config.Secret)
	mac.Write([]byte("cursor:v1:"))
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Server) decodeCursor(raw, scope string) (cursor, error) {
	var c cursor
	p := strings.Split(raw, ".")
	if len(p) != 2 || len(raw) > 2048 {
		return c, errors.New("cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(p[0])
	if e != nil {
		return c, e
	}
	sig, e := base64.RawURLEncoding.DecodeString(p[1])
	if e != nil {
		return c, e
	}
	mac := hmac.New(sha256.New, s.Config.Secret)
	mac.Write([]byte("cursor:v1:"))
	mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, errors.New("cursor signature")
	}
	if e = json.Unmarshal(b, &c); e != nil || c.Scope != scope || !auth.UUID(c.ID) {
		return c, errors.New("cursor scope")
	}
	return c, nil
}
func scope(r *http.Request, kind string) string {
	q := r.URL.Query()
	q.Del("cursor")
	q.Del("limit")
	return auth.Digest([]byte(kind + ":" + who(r).User.ID + ":" + q.Encode()))
}
func limit(q url.Values) (int, bool) {
	n := 20
	var e error
	if raw := q.Get("limit"); raw != "" {
		n, e = strconv.Atoi(raw)
	}
	return n, e == nil && n >= 1 && n <= 100
}
func validDates(from, to, today string) bool {
	f, e := time.Parse(time.DateOnly, from)
	if e != nil {
		return false
	}
	t, e := time.Parse(time.DateOnly, to)
	return e == nil && !t.After(mustDate(today)) && !f.After(t) && int(t.Sub(f).Hours()/24) < 366
}
func mustDate(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }
func filters(w http.ResponseWriter, r *http.Request, now time.Time) (string, string, string, string, bool) {
	q := r.URL.Query()
	category, method, from, to := q.Get("category"), q.Get("method"), q.Get("from"), q.Get("to")
	valid := category == "" || domain.Contains(domain.Categories, category)
	valid = valid && (method == "" || domain.Contains(domain.Methods, method))
	today := domain.Today(now, who(r).User.Timezone)
	for _, v := range []string{from, to} {
		if v != "" {
			d, e := time.Parse(time.DateOnly, v)
			valid = valid && e == nil && !d.After(mustDate(today))
		}
	}
	if from != "" && to != "" {
		valid = valid && from <= to
	}
	if !valid {
		fail(w, r, 422, "VALIDATION_ERROR", map[string]string{"filters": "Periksa kategori, metode, dan rentang tanggal."})
	}
	return category, method, from, to, valid
}
func mustParseDate(s string) (time.Time, error) { return time.Parse(time.DateOnly, s) }
