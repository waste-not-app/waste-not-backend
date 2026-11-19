package logs

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/cursor"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

type Service interface {
	CreateLog(ctx context.Context, u domain.User, in domain.LogInput) (LogWithProgressRes, bool, error)
	GetLog(ctx context.Context, userID, id string) (domain.Log, error)
	ListLogs(ctx context.Context, u domain.User, scope string, p ListLogsParams) (ListLogsRes, error)
	UpdateLog(ctx context.Context, u domain.User, id string, version int, patch map[string]json.RawMessage) (LogWithProgressRes, error)
	DeleteLog(ctx context.Context, u domain.User, id string, version int) error
}

type service struct {
	repo  Repository
	cfg   config.Config
	now   func() time.Time
	codec *cursor.Codec
}

func NewService(repo Repository, cfg config.Config, now func() time.Time, codec *cursor.Codec) Service {
	return &service{
		repo:  repo,
		cfg:   cfg,
		now:   now,
		codec: codec,
	}
}

func (s *service) CreateLog(ctx context.Context, u domain.User, in domain.LogInput) (LogWithProgressRes, bool, error) {
	if !domain.ValidUUID(in.ClientRequestID) {
		return LogWithProgressRes{}, false, &ValidationError{
			Fields: map[string]string{"client_request_id": "Gunakan UUID baru untuk draf baru."},
		}
	}
	if in.PhotoID != nil && s.cfg.PhotoStorageDir == "" {
		return LogWithProgressRes{}, false, &ValidationError{
			Fields: map[string]string{"photo_id": "Lampiran foto belum diaktifkan."},
		}
	}
	in.Notes = strings.TrimSpace(in.Notes)
	raw, _ := json.Marshal(in)
	hash := security.Digest(raw)
	now := s.now()
	today := domain.Today(now, u.Timezone)

	return s.repo.CreateIdempotent(ctx, u.ID, u.Timezone, today, hash, in, now, s.cfg.AllowPrototype)
}

func (s *service) GetLog(ctx context.Context, userID, id string) (domain.Log, error) {
	if !domain.ValidUUID(id) {
		return domain.Log{}, ErrLogNotFound
	}
	return s.repo.GetByID(ctx, userID, id)
}

func (s *service) ListLogs(ctx context.Context, u domain.User, scope string, p ListLogsParams) (ListLogsRes, error) {
	today := domain.Today(s.now(), u.Timezone)
	if !validFilters(p.Category, p.Method, p.From, p.To, today) {
		return ListLogsRes{}, &ValidationError{
			Fields: map[string]string{"filters": "Periksa kategori, metode, dan rentang tanggal."},
		}
	}
	limit, ok := cursor.ParseLimit(p.Limit)
	if !ok {
		return ListLogsRes{}, &ValidationError{
			Fields: map[string]string{"limit": "Gunakan 1–100."},
		}
	}
	q := ListLogsQuery{
		Category: p.Category,
		Method:   p.Method,
		From:     p.From,
		To:       p.To,
		Limit:    limit,
	}
	if p.Cursor != "" {
		tok, err := s.codec.Decode(p.Cursor, scope)
		if err != nil {
			return ListLogsRes{}, ErrInvalidCursor
		}
		if _, err = time.Parse(time.DateOnly, tok.Position); err != nil {
			return ListLogsRes{}, ErrInvalidCursor
		}
		q.DatePos, q.IDPos = tok.Position, tok.ID
	}

	items, err := s.repo.List(ctx, u.ID, q)
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

func (s *service) UpdateLog(ctx context.Context, u domain.User, id string, version int, patch map[string]json.RawMessage) (LogWithProgressRes, error) {
	if raw, ok := patch["photo_id"]; ok && string(raw) != "null" && s.cfg.PhotoStorageDir == "" {
		return LogWithProgressRes{}, &ValidationError{
			Fields: map[string]string{"photo_id": "Lampiran foto belum diaktifkan."},
		}
	}
	today := domain.Today(s.now(), u.Timezone)
	return s.repo.Update(ctx, u.ID, u.Timezone, today, id, version, patch)
}

func (s *service) DeleteLog(ctx context.Context, u domain.User, id string, version int) error {
	today := domain.Today(s.now(), u.Timezone)
	return s.repo.Delete(ctx, u.ID, today, id, version)
}

func validFilters(category, method, from, to, today string) bool {
	valid := category == "" || slices.Contains(domain.Categories, category)
	valid = valid && (method == "" || slices.Contains(domain.Methods, method))
	todayTime, _ := time.Parse(time.DateOnly, today)
	for _, v := range []string{from, to} {
		if v != "" {
			d, e := time.Parse(time.DateOnly, v)
			valid = valid && e == nil && !d.After(todayTime)
		}
	}
	if from != "" && to != "" {
		valid = valid && from <= to
	}
	return valid
}
