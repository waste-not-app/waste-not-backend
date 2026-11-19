package analytics

import (
	"context"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/cursor"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/ratelimit"
)

type Service interface {
	GetStatistics(ctx context.Context, u domain.User, from, to string) (StatisticsRes, error)
	GetDashboard(ctx context.Context, u domain.User) (DashboardRes, error)
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

func (s *service) GetStatistics(ctx context.Context, u domain.User, from, to string) (StatisticsRes, error) {
	today := domain.Today(s.limiter.Now(), u.Timezone)
	if from == "" {
		from = mustDate(today).AddDate(0, 0, -6).Format(time.DateOnly)
	}
	if to == "" {
		to = today
	}
	if !cursor.ValidDates(from, to, today) {
		return StatisticsRes{}, ErrInvalidDateRange
	}
	return s.repo.GetStatistics(ctx, u.ID, from, to, u.Timezone)
}

func (s *service) GetDashboard(ctx context.Context, u domain.User) (DashboardRes, error) {
	today := domain.Today(s.limiter.Now(), u.Timezone)
	return s.repo.GetDashboard(ctx, u.ID, today, u.Timezone)
}
