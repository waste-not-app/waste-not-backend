package ratelimit

import (
	"context"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/database/db"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

type Limiter struct {
	q          *db.Queries
	nowFn      func() time.Time
	HashSlots  Slots
	ImageSlots Slots
	PhotoSlots Slots
}

func NewLimiter(dbPool *pgxpool.Pool, nowFn func() time.Time) *Limiter {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Limiter{
		q:          db.New(dbPool),
		nowFn:      nowFn,
		HashSlots:  NewSlots(4),
		ImageSlots: NewSlots(2),
		PhotoSlots: NewSlots(2),
	}
}

func (l *Limiter) Allow(ctx context.Context, key string, n int, window time.Duration) (bool, error) {
	bucket := l.nowFn().UTC().Truncate(window)
	hits, err := l.q.IncrementRateLimit(ctx, db.IncrementRateLimitParams{
		Key:     security.Digest([]byte(key)),
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
