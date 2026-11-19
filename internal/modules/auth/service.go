package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

type Service interface {
	Register(ctx context.Context, req RegisterReq, normalizedEmail string) (security.Tokens, error)
	Login(ctx context.Context, normalizedEmail, password string) (security.Tokens, error)
	RefreshToken(ctx context.Context, refreshToken string) (security.Tokens, error)
	Logout(ctx context.Context, sessionID, userID string) error
	GetMe(ctx context.Context, u domain.User) (UserRes, error)
	RequestPasswordReset(ctx context.Context, email string) error
	ConfirmPasswordReset(ctx context.Context, token, password string) error
	DeleteMe(ctx context.Context, userID, password string) error
	RunResetMail(ctx context.Context, log *slog.Logger)
}

type service struct {
	repo      Repository
	signer    security.Signer
	now       func() time.Time
	dummyHash string
	mailer    ResetMailer
	mailKey   cipher.AEAD
}

func NewService(repo Repository, signer security.Signer, now func() time.Time, dummyHash string, mailer ResetMailer) Service {
	keyHash := sha256.New()
	keyHash.Write([]byte("wastenot-password-reset-mail-v1\x00"))
	keyHash.Write(signer.Secret)
	block, _ := aes.NewCipher(keyHash.Sum(nil))
	mailKey, _ := cipher.NewGCMWithRandomNonce(block)
	return &service{
		repo:      repo,
		signer:    signer,
		now:       now,
		dummyHash: dummyHash,
		mailer:    mailer,
		mailKey:   mailKey,
	}
}

func (s *service) Register(ctx context.Context, req RegisterReq, normalizedEmail string) (security.Tokens, error) {
	hash, err := security.Hash(req.Password)
	if err != nil {
		return security.Tokens{}, err
	}
	u := UserRes{
		ID:       uuid.NewString(),
		Name:     req.Name,
		Email:    normalizedEmail,
		Timezone: req.Timezone,
	}
	return s.repo.CreateUserWithSession(ctx, u, hash, s.signer, s.now())
}

func (s *service) Login(ctx context.Context, normalizedEmail, password string) (security.Tokens, error) {
	rec, err := s.repo.GetUserByEmail(ctx, normalizedEmail)
	if errors.Is(err, ErrUnauthenticated) {
		security.Verify(password, s.dummyHash)
		return security.Tokens{}, ErrUnauthenticated
	}
	if err != nil {
		return security.Tokens{}, err
	}
	if !security.Verify(password, rec.PasswordHash) {
		return security.Tokens{}, ErrUnauthenticated
	}
	return s.repo.CreateSessionForUser(ctx, rec.UserRes, rec.PasswordHash, s.signer, s.now())
}

func (s *service) RefreshToken(ctx context.Context, refreshToken string) (security.Tokens, error) {
	if len(refreshToken) != 43 {
		return security.Tokens{}, ErrSessionExpired
	}
	return s.repo.RotateRefreshToken(ctx, refreshToken, s.signer, s.now())
}

func (s *service) Logout(ctx context.Context, sessionID, userID string) error {
	return s.repo.RevokeSession(ctx, sessionID, userID)
}

func (s *service) GetMe(ctx context.Context, u domain.User) (UserRes, error) {
	today := domain.Today(s.now(), u.Timezone)
	prog, err := s.repo.GetUserProgress(ctx, u.ID, today)
	if err != nil {
		return UserRes{}, err
	}
	return UserRes{
		ID:       u.ID,
		Name:     u.Name,
		Email:    u.Email,
		Timezone: u.Timezone,
		Progress: &prog,
	}, nil
}
