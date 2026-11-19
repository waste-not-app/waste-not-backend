package achievements

import (
	"context"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/ratelimit"
)

type Service interface {
	ListAchievements(ctx context.Context, u domain.User) ([]AchievementRes, error)
}

type service struct {
	repo    Repository
	limiter *ratelimit.Limiter
}

func NewService(repo Repository, limiter *ratelimit.Limiter) Service {
	return &service{
		repo:    repo,
		limiter: limiter,
	}
}

func (s *service) ListAchievements(ctx context.Context, u domain.User) ([]AchievementRes, error) {
	today := domain.Today(s.limiter.Now(), u.Timezone)
	return s.repo.ListForUser(ctx, u.ID, today)
}
