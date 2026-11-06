package system

import (
	"context"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/ai"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

type Service interface {
	ReleaseVersion() string
	CheckReadiness(ctx context.Context) error
	GetMetadata(ctx context.Context, requestID string) MetadataRes
}

type service struct {
	repo     Repository
	cfg      config.Config
	aiClient *ai.Client
}

func NewService(repo Repository, cfg config.Config, aiClient *ai.Client) Service {
	return &service{
		repo:     repo,
		cfg:      cfg,
		aiClient: aiClient,
	}
}

func (s *service) ReleaseVersion() string {
	return s.cfg.Release
}

func (s *service) CheckReadiness(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	ready, err := s.repo.IsSchemaMigrated(timeoutCtx)
	if err != nil || !ready {
		return ErrDatabaseNotReady
	}
	return nil
}

func (s *service) GetMetadata(ctx context.Context, requestID string) MetadataRes {
	var catalogVersion any
	enabled := false
	if s.cfg.AIEnabled && s.aiClient != nil {
		catalog, err := s.aiClient.Catalog(ctx, requestID)
		if err == nil {
			catalogVersion = catalog["catalog_version"]
			enabled = true
		}
	}

	return MetadataRes{
		Categories: domain.Categories,
		Methods:    domain.Methods,
		Limits: LimitsRes{
			WeightG:              RangeLimit{Min: 1, Max: 100000},
			ItemCount:            RangeLimit{Min: 1, Max: 10000},
			NameLength:           RangeLimit{Min: 2, Max: 60},
			EmailMaxLength:       254,
			PasswordLength:       RangeLimit{Min: 12, Max: 128},
			NotesMaxLength:       500,
			ActivityDatePastDays: 30,
			KgMaxDecimalPlaces:   3,
		},
		Capabilities: CapabilitiesRes{
			Achievements:     false,
			Reminders:        false,
			Photos:           false,
			RecyclingPoints:  false,
			AIScan:           enabled,
			AIRecommendation: enabled && s.cfg.RecommendationEnabled,
		},
		AIObjectCatalogVersion: catalogVersion,
	}
}
