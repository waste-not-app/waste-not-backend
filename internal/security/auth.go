package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const AccessTTL = 15 * time.Minute
const RefreshTTL = 7 * 24 * time.Hour

type Claims struct {
	SessionID  string `json:"sid"`
	Generation int    `json:"gen"`
	jwt.RegisteredClaims
}
type Tokens struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	SessionID        string    `json:"session_id"`
	User             any       `json:"user,omitempty"`
}
type Signer struct {
	Secret           []byte
	Issuer, Audience string
}

func (s Signer) Issue(uid, sid string, generation int, expires, timeNow time.Time) (Tokens, error) {
	accessEnd := timeNow.Add(AccessTTL)
	if accessEnd.After(expires) {
		accessEnd = expires
	}
	c := Claims{SessionID: sid, Generation: generation, RegisteredClaims: jwt.RegisteredClaims{Subject: uid, Issuer: s.Issuer, Audience: jwt.ClaimStrings{s.Audience}, IssuedAt: jwt.NewNumericDate(timeNow), ExpiresAt: jwt.NewNumericDate(accessEnd), ID: uuid.NewString()}}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.Secret)
	if err != nil {
		return Tokens{}, err
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return Tokens{}, err
	}
	return Tokens{signed, accessEnd, base64.RawURLEncoding.EncodeToString(raw), expires, sid, nil}, nil
}
func (s Signer) Parse(raw string) (*Claims, error) {
	c := new(Claims)
	_, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) { return s.Secret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(s.Issuer), jwt.WithAudience(s.Audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		return nil, err
	}
	if !UUID(c.Subject) || !UUID(c.SessionID) || c.Generation < 1 || c.IssuedAt == nil {
		return nil, errors.New("invalid claims")
	}
	return c, nil
}
func UUID(s string) bool       { u, e := uuid.Parse(s); return e == nil && u != uuid.Nil && u.String() == s }
func Digest(raw []byte) string { v := sha256.Sum256(raw); return hex.EncodeToString(v[:]) }

var PasswordParams = &argon2id.Params{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func Hash(password string) (string, error) { return argon2id.CreateHash(password, PasswordParams) }
func Verify(password, hash string) bool {
	ok, err := argon2id.ComparePasswordAndHash(password, hash)
	return err == nil && ok
}
