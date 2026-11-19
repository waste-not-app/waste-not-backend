package guides

import (
	"context"

	"github.com/waste-not-app/waste-not-backend/internal/cursor"
	"github.com/waste-not-app/waste-not-backend/internal/database"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

type Service interface {
	GetGuide(ctx context.Context, id string) (database.Guide, error)
	ListGuides(ctx context.Context, scope string, q ListGuidesQuery) (ListGuidesRes, error)
}

type service struct {
	repo  Repository
	codec *cursor.Codec
}

func NewService(repo Repository, codec *cursor.Codec) Service {
	return &service{
		repo:  repo,
		codec: codec,
	}
}

func (s *service) GetGuide(ctx context.Context, id string) (database.Guide, error) {
	if !domain.ValidUUID(id) {
		return database.Guide{}, ErrGuideNotFound
	}
	return s.repo.GetPublishedByID(ctx, id)
}

func (s *service) ListGuides(ctx context.Context, scope string, q ListGuidesQuery) (ListGuidesRes, error) {
	if q.Cursor != "" {
		tok, err := s.codec.Decode(q.Cursor, scope)
		if err != nil {
			return ListGuidesRes{}, ErrInvalidCursor
		}
		q.NamePos, q.IDPos = tok.Position, tok.ID
	}
	list, err := s.repo.ListPublished(ctx, q)
	if err != nil {
		return ListGuidesRes{}, err
	}
	more := len(list) > q.Limit
	var next any
	if more {
		list = list[:q.Limit]
		last := list[len(list)-1]
		next = s.codec.Encode(cursor.Token{
			Scope:    scope,
			Position: last.Name,
			ID:       last.ID,
		})
	}
	return ListGuidesRes{
		Items:      list,
		HasMore:    more,
		NextCursor: next,
	}, nil
}
