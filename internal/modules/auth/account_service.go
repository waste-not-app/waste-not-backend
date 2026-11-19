package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/security"
)

func (s *service) RequestPasswordReset(ctx context.Context, email string) error {
	if s.mailer == nil {
		return ErrMailUnavailable
	}
	// Probe before looking up the account: transport failures never disclose membership.
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := s.mailer.Check(probe)
	cancel()
	if err != nil {
		return ErrMailUnavailable
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	encrypted := s.mailKey.Seal(nil, nil, []byte(token), []byte(email))
	return s.repo.RequestPasswordReset(ctx, email, security.Digest([]byte(token)), encrypted, s.limiter.Now())
}

func (s *service) ConfirmPasswordReset(ctx context.Context, token, password string) error {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return ErrInvalidResetToken
	}
	hash, err := security.Hash(password)
	if err != nil {
		return err
	}
	return s.repo.ConfirmPasswordReset(ctx, security.Digest([]byte(token)), hash, s.limiter.Now())
}

func (s *service) DeleteMe(ctx context.Context, userID, password string) error {
	return s.repo.DeleteMe(ctx, userID, password)
}

func (s *service) RunResetMail(ctx context.Context, log *slog.Logger) {
	if s.mailer == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for range 20 {
				if ctx.Err() != nil {
					return
				}
				delivery, cancel := context.WithTimeout(ctx, 15*time.Second)
				worked, err := s.repo.DeliverResetMail(delivery, func(ctx context.Context, email string, encrypted []byte) error {
					token, err := s.mailKey.Open(nil, nil, encrypted, []byte(email))
					if err != nil {
						return ErrMailUnavailable
					}
					if err := s.mailer.Send(ctx, email, string(token)); err != nil {
						return ErrMailUnavailable
					}
					return nil
				})
				cancel()
				if err != nil {
					if ctx.Err() == nil && log != nil {
						log.Warn("password reset delivery pending; transport or queue unavailable")
					}
					break
				}
				if !worked {
					break
				}
			}
		}
	}
}
