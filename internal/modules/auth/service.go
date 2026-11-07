package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	coreauth "github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/shared/ratelimit"
)

type Service interface {
	Register(ctx context.Context, req RegisterReq, normalizedEmail string) (coreauth.Tokens, error)
	Login(ctx context.Context, normalizedEmail, password string) (coreauth.Tokens, error)
	RefreshToken(ctx context.Context, refreshToken string) (coreauth.Tokens, error)
	Logout(ctx context.Context, sessionID, userID string) error
	GetMe(ctx context.Context, u middleware.UserIdentity) (UserRes, error)
}

type service struct {
	repo      Repository
	signer    coreauth.Signer
	limiter   *ratelimit.Limiter
	dummyHash string
}

func NewService(repo Repository, signer coreauth.Signer, limiter *ratelimit.Limiter, dummyHash string) Service {
	return &service{
		repo:      repo,
		signer:    signer,
		limiter:   limiter,
		dummyHash: dummyHash,
	}
}

func (s *service) Register(ctx context.Context, req RegisterReq, normalizedEmail string) (coreauth.Tokens, error) {
	hash, err := coreauth.Hash(req.Password)
	if err != nil {
		return coreauth.Tokens{}, err
	}
	u := UserRes{
		ID:       uuid.NewString(),
		Name:     req.Name,
		Email:    normalizedEmail,
		Timezone: req.Timezone,
	}
	return s.repo.CreateUserWithSession(ctx, u, hash, s.signer, s.limiter.Now())
}

func (s *service) Login(ctx context.Context, normalizedEmail, password string) (coreauth.Tokens, error) {
	rec, err := s.repo.GetUserByEmail(ctx, normalizedEmail)
	if errors.Is(err, ErrUnauthenticated) {
		coreauth.Verify(password, s.dummyHash)
		return coreauth.Tokens{}, ErrUnauthenticated
	}
	if err != nil {
		return coreauth.Tokens{}, err
	}
	if !coreauth.Verify(password, rec.PasswordHash) {
		return coreauth.Tokens{}, ErrUnauthenticated
	}
	return s.repo.CreateSessionForUser(ctx, rec.UserRes, s.signer, s.limiter.Now())
}

func (s *service) RefreshToken(ctx context.Context, refreshToken string) (coreauth.Tokens, error) {
	if len(refreshToken) != 43 {
		return coreauth.Tokens{}, ErrSessionExpired
	}
	return s.repo.RotateRefreshToken(ctx, refreshToken, s.signer, s.limiter.Now())
}

func (s *service) Logout(ctx context.Context, sessionID, userID string) error {
	return s.repo.RevokeSession(ctx, sessionID, userID)
}

func (s *service) GetMe(ctx context.Context, u middleware.UserIdentity) (UserRes, error) {
	today := domain.Today(s.limiter.Now(), u.Timezone)
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
