package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"unicode/utf8"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

const googleNonceLength = 43 // Unpadded base64url encoding of 32 random bytes.

func (s *service) GoogleEnabled() bool {
	return s.google != nil
}

func (s *service) GoogleChallenge(ctx context.Context) (GoogleChallengeRes, error) {
	if !s.GoogleEnabled() {
		return GoogleChallengeRes{}, ErrGoogleAuthUnavailable
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return GoogleChallengeRes{}, err
	}
	nonce := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := s.repo.IssueGoogleChallenge(ctx, security.Digest([]byte(nonce))); err != nil {
		return GoogleChallengeRes{}, err
	}
	return GoogleChallengeRes{Nonce: nonce}, nil
}

func (s *service) consumeGoogleToken(ctx context.Context, rawToken string) (GoogleIdentity, error) {
	if !s.GoogleEnabled() {
		return GoogleIdentity{}, ErrGoogleAuthUnavailable
	}
	if rawToken == "" {
		return GoogleIdentity{}, ErrUnauthenticated
	}
	identity, err := s.google.Verify(ctx, rawToken)
	if err != nil {
		return GoogleIdentity{}, err
	}
	// Commit consumption before account resolution: link-required, conflicts, and rejected proofs cannot reuse it.
	if err := s.repo.ClaimGoogleChallenge(ctx, security.Digest([]byte(identity.Nonce))); err != nil {
		return GoogleIdentity{}, err
	}
	return identity, nil
}

func (s *service) GoogleLogin(ctx context.Context, req GoogleLoginReq) (security.Tokens, error) {
	identity, err := s.consumeGoogleToken(ctx, req.IDToken)
	if err != nil {
		return security.Tokens{}, err
	}
	email, valid := domain.Email(identity.Email)
	if !valid {
		return security.Tokens{}, ErrUnauthenticated
	}
	identity.Email = email
	if req.CurrentPassword != nil {
		if n := utf8.RuneCountInString(*req.CurrentPassword); n == 0 || n > domain.MaxPasswordLength {
			return security.Tokens{}, ErrUnauthenticated
		}
		if allowed, err := s.limiter.CheckAuthIdentityLimit(ctx, email); err != nil {
			return security.Tokens{}, err
		} else if !allowed {
			return security.Tokens{}, ErrGoogleRateLimited
		}
		if !s.limiter.HashSlots.TryAcquire() {
			return security.Tokens{}, ErrGoogleHashBusy
		}
		defer s.limiter.HashSlots.Release()
	}
	return s.repo.ExchangeGoogleIdentity(ctx, identity, req.Timezone, req.CurrentPassword, s.signer, s.now())
}
