package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

const (
	AccessTTL  = 15 * time.Minute
	RefreshTTL = 7 * 24 * time.Hour
)

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
	Secret   []byte
	Issuer   string
	Audience string
}

func (s Signer) Issue(uid, sid string, generation int, expires, now time.Time) (Tokens, error) {
	accessEnd := now.Add(AccessTTL)
	if accessEnd.After(expires) {
		accessEnd = expires
	}
	claims := Claims{
		SessionID:  sid,
		Generation: generation,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uid,
			Issuer:    s.Issuer,
			Audience:  jwt.ClaimStrings{s.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessEnd),
			ID:        uuid.NewString(),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.Secret)
	if err != nil {
		return Tokens{}, err
	}
	refresh := make([]byte, 32)
	if _, err = rand.Read(refresh); err != nil {
		return Tokens{}, err
	}
	return Tokens{
		AccessToken:      signed,
		AccessExpiresAt:  accessEnd,
		RefreshToken:     base64.RawURLEncoding.EncodeToString(refresh),
		RefreshExpiresAt: expires,
		SessionID:        sid,
	}, nil
}

func (s Signer) Parse(raw string) (*Claims, error) {
	claims := new(Claims)
	_, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return s.Secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(s.Issuer),
		jwt.WithAudience(s.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return nil, err
	}
	if !domain.ValidUUID(claims.Subject) || !domain.ValidUUID(claims.SessionID) || claims.Generation < 1 || claims.IssuedAt == nil {
		return nil, errors.New("invalid claims")
	}
	return claims, nil
}
