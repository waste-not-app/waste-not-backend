package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/jpeg"
	"time"

	"github.com/disintegration/imaging"

	coreai "github.com/waste-not-app/waste-not-backend/internal/ai"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/security"
	"github.com/waste-not-app/waste-not-backend/internal/shared/ratelimit"
)

type Service interface {
	IsAIEnabled() bool
	IsRecommendationEnabled() bool
	Now() time.Time
	GetCatalog(ctx context.Context, reqID string) (map[string]any, error)
	NormalizeRecommendationInput(in map[string]any) error
	PrepareDetectionSelection(ctx context.Context, userID string, in map[string]any) (*string, any, error)
	ClaimOrReplay(ctx context.Context, userID, kind, clientID, hash string, parent *string) (Operation, bool, error)
	WaitReplay(ctx context.Context, userID string, o Operation) (Operation, error)
	CompleteOperation(ctx context.Context, userID string, o Operation, result, snapshot any, opErr error) (int, error)
	ExecuteDetection(ctx context.Context, reqID string, o Operation, imageBytes []byte) (map[string]any, map[string]any, error)
	ExecuteRecommendation(ctx context.Context, reqID string, o Operation, in map[string]any, selection any) (map[string]any, map[string]any, error)
	DeleteDetection(ctx context.Context, userID, id string) error
}

type service struct {
	repo    Repository
	cfg     config.Config
	client  *coreai.Client
	limiter *ratelimit.Limiter
}

func NewService(repo Repository, cfg config.Config, client *coreai.Client, limiter *ratelimit.Limiter) Service {
	return &service{
		repo:    repo,
		cfg:     cfg,
		client:  client,
		limiter: limiter,
	}
}

func (s *service) IsAIEnabled() bool {
	return s.cfg.AIEnabled
}

func (s *service) IsRecommendationEnabled() bool {
	return s.cfg.RecommendationEnabled
}

func (s *service) Now() time.Time {
	return s.limiter.Now()
}

func (s *service) GetCatalog(ctx context.Context, reqID string) (map[string]any, error) {
	return s.client.Catalog(ctx, reqID)
}

func (s *service) NormalizeRecommendationInput(in map[string]any) error {
	return s.client.NormalizeInput(in)
}

func (s *service) PrepareDetectionSelection(ctx context.Context, userID string, in map[string]any) (*string, any, error) {
	if in["input_source"] != "DETECTION" {
		return nil, nil, nil
	}
	id, _ := in["detection_id"].(string)
	if !security.UUID(id) {
		return nil, nil, ErrInvalidAIReference
	}
	o, err := s.repo.GetOperation(ctx, userID, id, "DETECTION", s.limiter.Now())
	if err != nil {
		return nil, nil, err
	}
	if o.State != "COMPLETED" {
		return nil, nil, ErrInvalidAIReference
	}
	var snapshot map[string]any
	if json.Unmarshal(o.Snapshot, &snapshot) != nil {
		return nil, nil, coreai.ErrInvalid
	}
	objectID := in["object_id"]
	objects, _ := snapshot["objects"].([]any)
	model, modelOK := snapshot["model"].(map[string]any)
	if !modelOK {
		return nil, nil, coreai.ErrInvalid
	}
	var object map[string]any
	for _, entry := range objects {
		candidate, ok := entry.(map[string]any)
		if ok && candidate["object_id"] == objectID {
			object = candidate
			break
		}
	}
	if object == nil {
		return nil, nil, ErrInvalidAIReference
	}
	if object["supported_for_review"] != true {
		return nil, nil, ErrObjectRequiresManual
	}
	rawObject := map[string]any{}
	for _, key := range []string{"class_id", "raw_class", "confidence", "bbox"} {
		rawObject[key] = object[key]
	}
	selection := map[string]any{
		"model_version":   model["model_version"],
		"model_sha256":    model["sha256"],
		"mapping_version": snapshot["mapping_version"],
		"class_names":     model["class_names"],
		"object":          rawObject,
	}
	return &id, selection, nil
}

func (s *service) ClaimOrReplay(ctx context.Context, userID, kind, clientID, hash string, parent *string) (Operation, bool, error) {
	o, isOwner, err := s.repo.Claim(ctx, userID, kind, clientID, hash, parent, s.limiter.Now())
	if err != nil || !isOwner {
		return o, isOwner, err
	}

	for _, quota := range []struct {
		key    string
		n      int
		window time.Duration
	}{{"ai-minute:", 10, time.Minute}, {"ai-day:", 100, 24 * time.Hour}} {
		ok, qErr := s.limiter.Allow(ctx, quota.key+userID, quota.n, quota.window)
		if qErr != nil {
			_, _ = s.CompleteOperation(ctx, userID, o, nil, nil, &coreai.Error{Status: 503, Code: "AI_UNAVAILABLE"})
			return o, false, &coreai.Error{Status: 503, Code: "AI_UNAVAILABLE"}
		}
		if !ok {
			rateErr := &coreai.Error{Status: 429, Code: "RATE_LIMITED", RetryAfter: int(quota.window.Seconds())}
			_, _ = s.CompleteOperation(ctx, userID, o, nil, nil, rateErr)
			return o, false, rateErr
		}
	}
	return o, true, nil
}

func (s *service) WaitReplay(ctx context.Context, userID string, o Operation) (Operation, error) {
	timer := time.NewTimer(27 * time.Second)
	defer timer.Stop()
	delay := 100 * time.Millisecond
	poll := time.NewTimer(delay)
	defer poll.Stop()

	for o.State == "IN_PROGRESS" {
		select {
		case <-ctx.Done():
			return o, &coreai.Error{Status: 504, Code: "AI_TIMEOUT"}
		case <-timer.C:
			return o, &coreai.Error{Status: 503, Code: "AI_BUSY", RetryAfter: 2}
		case <-poll.C:
			delay = min(delay*2, time.Second)
			poll.Reset(delay)
			var err error
			o, err = s.repo.GetOperation(ctx, userID, o.ID, "", s.limiter.Now())
			if err != nil {
				return o, err
			}
		}
	}
	if !s.limiter.Now().Before(o.Expires) || o.Deleted {
		return o, ErrAISessionExpired
	}
	return o, nil
}

func (s *service) CompleteOperation(ctx context.Context, userID string, o Operation, result, snapshot any, opErr error) (int, error) {
	var raw, snap []byte
	state := "COMPLETED"
	code := ""
	status := 201
	retry := 0
	if opErr != nil {
		var a *coreai.Error
		if !errors.As(opErr, &a) {
			a = coreai.ErrInvalid
		}
		code, status, retry = a.Code, a.Status, a.RetryAfter
		state = "FAILED_UNCERTAIN"
	} else {
		raw, _ = json.Marshal(result)
		snap, _ = json.Marshal(snapshot)
		if v, ok := result.(map[string]any); ok && v["status"] != "READY" && v["recommendation_id"] != nil {
			status = 200
		}
	}

	updated, err := s.repo.Complete(ctx, userID, o.ID, state, raw, snap, status, code, retry)
	if err != nil {
		return 0, err
	}
	if !updated {
		return 0, ErrAISessionExpired
	}
	if opErr != nil {
		return status, opErr
	}
	return status, nil
}

func (s *service) ExecuteDetection(ctx context.Context, reqID string, o Operation, imageBytes []byte) (map[string]any, map[string]any, error) {
	normalized, err := imaging.Decode(bytes.NewReader(imageBytes), imaging.AutoOrientation(true))
	if err != nil {
		return nil, nil, &coreai.Error{Status: 422, Code: "INVALID_IMAGE"}
	}
	var clean bytes.Buffer
	if err = jpeg.Encode(&clean, normalized, &jpeg.Options{Quality: 90}); err != nil || clean.Len() > MaxImageBytes {
		return nil, nil, &coreai.Error{Status: 413, Code: "FILE_TOO_LARGE"}
	}
	snapshot, err := s.client.Detect(ctx, reqID, "image/jpeg", clean.Bytes())
	clean.Reset()
	if err != nil {
		return nil, nil, err
	}
	result := map[string]any{}
	for k, v := range snapshot {
		if k != "model" {
			result[k] = v
		}
	}
	result["detection_id"] = o.ID
	result["expires_at"] = o.Expires.Format(time.RFC3339Nano)
	if err = s.client.Validate("DetectionSession", result); err != nil {
		return nil, nil, err
	}
	return result, snapshot, nil
}

func (s *service) ExecuteRecommendation(ctx context.Context, reqID string, o Operation, in map[string]any, selection any) (map[string]any, map[string]any, error) {
	contextData := map[string]any{
		"object_key": in["confirmed_object_key"],
		"category":   in["confirmed_category"],
	}
	for _, key := range []string{"condition", "reuse_intention", "risk_flag", "risk_type", "recommendation_goal", "recycling_access", "allow_conditional"} {
		contextData[key] = in[key]
	}
	private := map[string]any{"context": contextData, "selection": selection}
	v, err := s.client.Recommend(ctx, reqID, private)
	if err != nil {
		return nil, nil, err
	}
	v["recommendation_id"] = o.ID
	v["detection_id"] = in["detection_id"]
	v["object_id"] = in["object_id"]
	v["context_revision"] = in["context_revision"]
	v["confirmed_object_key"] = in["confirmed_object_key"]
	v["category"] = in["confirmed_category"]
	v["expires_at"] = o.Expires.Format(time.RFC3339Nano)
	if err = s.client.Validate("RecommendationSession", v); err != nil {
		return nil, nil, err
	}
	snap := map[string]any{"request": in, "selection": selection, "result": v}
	return v, snap, nil
}

func (s *service) DeleteDetection(ctx context.Context, userID, id string) error {
	if !security.UUID(id) {
		return ErrOperationNotFound
	}
	return s.repo.DeleteDetection(ctx, userID, id)
}
