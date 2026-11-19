package achievements

import (
	"context"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

type Service interface {
	ListAchievements(ctx context.Context, u domain.User) ([]AchievementRes, error)
}

type service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository, now func() time.Time) Service {
	return &service{
		repo: repo,
		now:  now,
	}
}

func (s *service) ListAchievements(ctx context.Context, u domain.User) ([]AchievementRes, error) {
	today := domain.Today(s.now(), u.Timezone)
	return s.repo.ListForUser(ctx, u.ID, today)
}
