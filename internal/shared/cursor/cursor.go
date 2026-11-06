package cursor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/auth"
)

type Token struct {
	Scope    string `json:"scope"`
	Position string `json:"position"`
	ID       string `json:"id"`
}

type Codec struct {
	secret []byte
}

func NewCodec(secret []byte) *Codec {
	return &Codec{secret: secret}
}

func (c *Codec) Encode(t Token) string {
	b, _ := json.Marshal(t)
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte("cursor:v1:"))
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (c *Codec) Decode(raw, scope string) (Token, error) {
	var t Token
	p := strings.Split(raw, ".")
	if len(p) != 2 || len(raw) > 2048 {
		return t, errors.New("cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(p[0])
	if e != nil {
		return t, e
	}
	sig, e := base64.RawURLEncoding.DecodeString(p[1])
	if e != nil {
		return t, e
	}
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte("cursor:v1:"))
	mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return t, errors.New("cursor signature")
	}
	if e = json.Unmarshal(b, &t); e != nil || t.Scope != scope || !auth.UUID(t.ID) {
		return t, errors.New("cursor scope")
	}
	return t, nil
}

func BuildScope(kind, userID, rawQuery string) string {
	q, _ := url.ParseQuery(rawQuery)
	q.Del("cursor")
	q.Del("limit")
	return auth.Digest([]byte(kind + ":" + userID + ":" + q.Encode()))
}

func ParseLimit(raw string) (int, bool) {
	n := 20
	var err error
	if raw != "" {
		n, err = strconv.Atoi(raw)
	}
	return n, err == nil && n >= 1 && n <= 100
}

func ValidDates(from, to, today string) bool {
	f, e := time.Parse(time.DateOnly, from)
	if e != nil {
		return false
	}
	t, e := time.Parse(time.DateOnly, to)
	todayTime, _ := time.Parse(time.DateOnly, today)
	return e == nil && !t.After(todayTime) && !f.After(t) && int(t.Sub(f).Hours()/24) < 366
}
