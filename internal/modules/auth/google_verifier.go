package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	googleIssuer = "https://accounts.google.com"
	googleJWKS   = "https://www.googleapis.com/oauth2/v3/certs"
)

type GoogleIdentity struct {
	Subject, Email, Name, Nonce string
}

type GoogleVerifier interface {
	Verify(ctx context.Context, rawToken string) (GoogleIdentity, error)
}

type googleVerifier struct {
	verifier *oidc.IDTokenVerifier
}

func NewGoogleVerifier(clientID string) GoogleVerifier {
	if clientID == "" {
		return nil
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return newGoogleVerifier(clientID, googleJWKS, client, time.Now)
}

func newGoogleVerifier(clientID, keysURL string, client *http.Client, now func() time.Time) *googleVerifier {
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), keysURL)
	return &googleVerifier{verifier: oidc.NewVerifier(googleIssuer, keys, &oidc.Config{
		ClientID:             clientID,
		SupportedSigningAlgs: []string{oidc.RS256},
		Now:                  now,
	})}
}

func (v *googleVerifier) Verify(ctx context.Context, rawToken string) (GoogleIdentity, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		// go-oidc flattens key-set errors; distinguish a failed fetch from an invalid signature.
		if strings.HasPrefix(err.Error(), "failed to verify signature: fetching keys ") {
			return GoogleIdentity{}, ErrGoogleAuthUnavailable
		}
		return GoogleIdentity{}, ErrUnauthenticated
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := token.Claims(&claims); err != nil || !claims.EmailVerified || claims.Email == "" ||
		len(token.Audience) != 1 || !validGoogleSubject(token.Subject) || len(token.Nonce) != googleNonceLength {
		return GoogleIdentity{}, ErrUnauthenticated
	}
	return GoogleIdentity{Subject: token.Subject, Email: claims.Email, Name: claims.Name, Nonce: token.Nonce}, nil
}

func validGoogleSubject(subject string) bool {
	if len(subject) == 0 || len(subject) > 255 {
		return false
	}
	for i := range len(subject) {
		if subject[i] <= ' ' || subject[i] > '~' {
			return false
		}
	}
	return true
}
