package photos

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
)

type Service interface {
	Enabled() bool
	Upload(ctx context.Context, userID, declaredMIME string, raw []byte) (PhotoRes, error)
	GetContent(ctx context.Context, userID, id string) ([]byte, string, error)
	Delete(ctx context.Context, userID, id string) error
}

type service struct {
	repo    Repository
	storage Storage
}

func NewService(repo Repository, storage Storage) Service {
	return &service{
		repo:    repo,
		storage: storage,
	}
}

func (s *service) Enabled() bool {
	return s.storage != nil
}

func (s *service) Upload(ctx context.Context, userID, declaredMIME string, raw []byte) (PhotoRes, error) {
	if s.storage == nil {
		return PhotoRes{}, ErrPhotosDisabled
	}
	clean, outMIME, ext, err := NormalizeAndStripImage(declaredMIME, raw)
	if err != nil {
		return PhotoRes{}, err
	}
	id := uuid.NewString()
	objectKey := fmt.Sprintf("%s/%s.%s", userID, id, ext)
	if err := s.storage.Put(objectKey, clean); err != nil {
		return PhotoRes{}, err
	}
	rec, err := s.repo.Create(ctx, id, userID, objectKey, outMIME, len(clean))
	if err != nil {
		_ = s.storage.Delete(objectKey)
		return PhotoRes{}, err
	}
	return PhotoRes{
		ID:         rec.ID,
		MimeType:   rec.MimeType,
		SizeBytes:  rec.SizeBytes,
		Status:     rec.Status,
		PreviewURL: "/v1/photos/" + rec.ID,
		CreatedAt:  rec.CreatedAt,
	}, nil
}

func (s *service) GetContent(ctx context.Context, userID, id string) ([]byte, string, error) {
	if s.storage == nil {
		return nil, "", ErrPhotosDisabled
	}
	if !auth.UUID(id) {
		return nil, "", ErrPhotoNotFound
	}
	rec, err := s.repo.GetActive(ctx, id, userID)
	if err != nil {
		return nil, "", err
	}
	data, err := s.storage.Get(rec.ObjectKey)
	if err != nil {
		return nil, "", err
	}
	return data, rec.MimeType, nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	if s.storage == nil {
		return ErrPhotosDisabled
	}
	if !auth.UUID(id) {
		return ErrPhotoNotFound
	}
	objectKey, err := s.repo.DeleteIfUnused(ctx, id, userID)
	if err != nil {
		return err
	}
	if objectKey != "" {
		_ = s.storage.Delete(objectKey)
	}
	return nil
}
