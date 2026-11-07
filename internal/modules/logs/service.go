package logs

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/shared/cursor"
	"github.com/waste-not-app/waste-not-backend/internal/shared/ratelimit"
)

type Service interface {
	Now() time.Time
	CreateLog(ctx context.Context, u middleware.UserIdentity, in domain.LogInput) (LogWithProgressRes, bool, error)
	GetLog(ctx context.Context, userID, id string) (domain.Log, error)
	ListLogs(ctx context.Context, userID, scope string, q ListLogsQuery) (ListLogsRes, error)
	UpdateLog(ctx context.Context, u middleware.UserIdentity, id string, version int, patch map[string]json.RawMessage) (LogWithProgressRes, error)
	DeleteLog(ctx context.Context, userID, id string, version int) error
	DecodeCursor(raw, scope string) (cursor.Token, error)
}

type service struct {
	repo    Repository
	cfg     config.Config
	limiter *ratelimit.Limiter
	codec   *cursor.Codec
}

func NewService(repo Repository, cfg config.Config, limiter *ratelimit.Limiter, codec *cursor.Codec) Service {
	return &service{
		repo:    repo,
		cfg:     cfg,
		limiter: limiter,
		codec:   codec,
	}
}

func (s *service) Now() time.Time {
	return s.limiter.Now()
}

func (s *service) DecodeCursor(raw, scope string) (cursor.Token, error) {
	return s.codec.Decode(raw, scope)
}

func (s *service) CreateLog(ctx context.Context, u middleware.UserIdentity, in domain.LogInput) (LogWithProgressRes, bool, error) {
	if !auth.UUID(in.ClientRequestID) {
		return LogWithProgressRes{}, false, &ValidationError{
			Fields: map[string]string{"client_request_id": "Gunakan UUID baru untuk draf baru."},
		}
	}
	in.Notes = strings.TrimSpace(in.Notes)
	raw, _ := json.Marshal(in)
	hash := auth.Digest(raw)
	now := s.limiter.Now()
	today := domain.Today(now, u.Timezone)

	return s.repo.CreateIdempotent(ctx, u.ID, u.Timezone, today, hash, in, now, s.cfg.AllowPrototype)
}

func (s *service) GetLog(ctx context.Context, userID, id string) (domain.Log, error) {
	if !auth.UUID(id) {
		return domain.Log{}, ErrLogNotFound
	}
	return s.repo.GetByID(ctx, userID, id)
}

func (s *service) ListLogs(ctx context.Context, userID, scope string, q ListLogsQuery) (ListLogsRes, error) {
	items, err := s.repo.List(ctx, userID, q)
	if err != nil {
		return ListLogsRes{}, err
	}
	more := len(items) > q.Limit
	var next any
	if more {
		items = items[:q.Limit]
		last := items[len(items)-1]
		next = s.codec.Encode(cursor.Token{
			Scope:    scope,
			Position: last.ActivityDate,
			ID:       last.ID,
		})
	}
	return ListLogsRes{
		Items:      items,
		HasMore:    more,
		NextCursor: next,
	}, nil
}

func (s *service) UpdateLog(ctx context.Context, u middleware.UserIdentity, id string, version int, patch map[string]json.RawMessage) (LogWithProgressRes, error) {
	today := domain.Today(s.limiter.Now(), u.Timezone)
	return s.repo.Update(ctx, u.ID, u.Timezone, today, id, version, patch)
}

func (s *service) DeleteLog(ctx context.Context, userID, id string, version int) error {
	return s.repo.Delete(ctx, userID, id, version)
}
