package ratelimit

import (
	"context"
	"net"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
	"github.com/waste-not-app/waste-not-backend/internal/shared/response"
)

type Limiter struct {
	q          *db.Queries
	nowFn      func() time.Time
	hashSlots  chan struct{}
	imageSlots chan struct{}
}

func NewLimiter(dbPool *pgxpool.Pool, nowFn func() time.Time) *Limiter {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Limiter{
		q:          db.New(dbPool),
		nowFn:      nowFn,
		hashSlots:  make(chan struct{}, 4),
		imageSlots: make(chan struct{}, 2),
	}
}

func (l *Limiter) Now() time.Time {
	return l.nowFn()
}

func (l *Limiter) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		l.nowFn = fn
	}
}

func (l *Limiter) Allow(ctx context.Context, key string, n int, window time.Duration) (bool, error) {
	bucket := l.nowFn().UTC().Truncate(window)
	hits, err := l.q.IncrementRateLimit(ctx, db.IncrementRateLimitParams{
		Key:     auth.Digest([]byte(key)),
		Bucket:  bucket,
		MaxHits: n,
	})
	return hits <= n, err
}

func (l *Limiter) CheckAuthLimit(ctx context.Context, clientIP, email string) (bool, error) {
	host, _, err := net.SplitHostPort(clientIP)
	if err != nil {
		host = clientIP
	}
	for _, key := range []string{"auth-ip:" + host, "auth-email:" + email} {
		ok, err := l.Allow(ctx, key, 20, 15*time.Minute)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func (l *Limiter) AcquireHashSlot(c fiber.Ctx) bool {
	select {
	case l.hashSlots <- struct{}{}:
		return true
	default:
		c.Set("Retry-After", "1")
		_ = response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
		return false
	}
}

func (l *Limiter) ReleaseHashSlot() {
	<-l.hashSlots
}

func (l *Limiter) AcquireImageSlot(c fiber.Ctx) bool {
	select {
	case l.imageSlots <- struct{}{}:
		return true
	default:
		c.Set("Retry-After", "1")
		_ = response.Fail(c, fiber.StatusServiceUnavailable, "AI_BUSY", nil)
		return false
	}
}

func (l *Limiter) ReleaseImageSlot() {
	<-l.imageSlots
}
