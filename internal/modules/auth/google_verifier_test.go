package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGoogleVerifierPinsSignedIdentity(t *testing.T) {
	const clientID = "test-only-audience"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	var unavailable atomic.Bool
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "unit-test-key", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	defer jwks.Close()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	verifier := newGoogleVerifier(clientID, jwks.URL, jwks.Client(), func() time.Time { return now })
	valid := jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": clientID, "sub": "123456789",
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": strings.Repeat("x", 43),
		"email": "identity@example.com", "name": "First Last", "email_verified": true,
	}
	sign := func(claims jwt.MapClaims, method jwt.SigningMethod, kid string) string {
		t.Helper()
		token := jwt.NewWithClaims(method, claims)
		token.Header["kid"] = kid
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	for _, issuer := range []string{"https://accounts.google.com", "accounts.google.com"} {
		claims := maps.Clone(valid)
		claims["iss"] = issuer
		identity, err := verifier.Verify(t.Context(), sign(claims, jwt.SigningMethodRS256, "unit-test-key"))
		want := GoogleIdentity{Subject: "123456789", Email: "identity@example.com", Name: "First Last", Nonce: strings.Repeat("x", 43)}
		if err != nil || identity != want {
			t.Fatal("valid Google claims did not resolve", err)
		}
	}
	if n := requests.Load(); n != 1 {
		t.Fatalf("JWKS requests = %d, want 1 for cached verification", n)
	}

	for _, tc := range []struct {
		name, claim string
		value       any
	}{
		{"wrong issuer", "iss", "https://identity.example.com"},
		{"wrong audience", "aud", "different-test-audience"},
		{"extra audience", "aud", []string{clientID, "different-test-audience"}},
		{"expired", "exp", now.Add(-time.Second).Unix()},
		{"unverified email", "email_verified", false},
		{"no email", "email", ""},
		{"no subject", "sub", ""},
		{"no nonce", "nonce", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := maps.Clone(valid)
			claims[tc.claim] = tc.value
			if _, err := verifier.Verify(t.Context(), sign(claims, jwt.SigningMethodRS256, "unit-test-key")); !errors.Is(err, ErrUnauthenticated) {
				t.Fatal("invalid claim was not rejected", err)
			}
		})
	}
	if _, err := verifier.Verify(t.Context(), sign(valid, jwt.SigningMethodRS384, "unit-test-key")); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("non-RS256 ID token was not rejected", err)
	}
	raw := sign(valid, jwt.SigningMethodRS256, "unit-test-key")
	i := strings.LastIndexByte(raw, '.') + 1
	wrongSignature := 'A'
	if raw[i] == 'A' {
		wrongSignature = 'B'
	}
	if _, err := verifier.Verify(t.Context(), raw[:i]+string(wrongSignature)+raw[i+1:]); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("invalid signature was not rejected", err)
	}

	unavailable.Store(true)
	if _, err := verifier.Verify(t.Context(), sign(valid, jwt.SigningMethodRS256, "unavailable-test-key")); !errors.Is(err, ErrGoogleAuthUnavailable) {
		t.Fatal("JWKS fetch failure was not distinguished from invalid proof", err)
	}
}
