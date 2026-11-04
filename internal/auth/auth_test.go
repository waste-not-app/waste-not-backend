package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	a, e := Hash("password-example-123")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := Hash("password-example-123")
	if a == b || !Verify("password-example-123", a) || Verify("password-example-124", a) {
		t.Fatal("salt or verification failed")
	}
}
func TestJWT(t *testing.T) {
	s := Signer{[]byte(strings.Repeat("x", 32)), "issuer", "audience"}
	tokens, e := s.Issue(uuid.NewString(), uuid.NewString(), 1, time.Now().Add(time.Hour), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Parse(tokens.AccessToken); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []Signer{{[]byte(strings.Repeat("y", 32)), "issuer", "audience"}, {s.Secret, "other", "audience"}, {s.Secret, "issuer", "other"}} {
		if _, e = bad.Parse(tokens.AccessToken); e == nil {
			t.Fatal("accepted invalid JWT")
		}
	}
	expired, _ := s.Issue(uuid.NewString(), uuid.NewString(), 1, time.Now().Add(-time.Hour), time.Now().Add(-2*time.Hour))
	if _, e = s.Parse(expired.AccessToken); e == nil {
		t.Fatal("accepted expired token")
	}
	forged, _ := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": uuid.NewString()}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, e = s.Parse(forged); e == nil {
		t.Fatal("accepted alg none")
	}
}
