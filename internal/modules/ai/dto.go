package ai

import (
	"encoding/json"
	"time"
)

const MaxImageBytes = 5 * 1024 * 1024

type Operation struct {
	ID         string
	State      string
	Hash       string
	Response   json.RawMessage
	Snapshot   json.RawMessage
	Status     int
	Code       string
	RetryAfter int
	Expires    time.Time
	Deleted    bool
}

type RecommendationReq struct {
	ClientRequestID string `json:"client_request_id"`
	InputSource     string `json:"input_source"`
	DetectionID     string `json:"detection_id,omitempty"`
	SelectionMode   string `json:"selection_mode,omitempty"`
	CandidateIndex  *int   `json:"candidate_index,omitempty"`
	ObjectCode      string `json:"object_code,omitempty"`
	UserHint        string `json:"user_hint,omitempty"`
}

type AICatalogObjectRes struct {
	ObjectCode       string `json:"object_code"`
	DisplayNameID    string `json:"display_name_id"`
	DefaultCategory  string `json:"default_category"`
	SupportsDirectAI bool   `json:"supports_direct_ai"`
}

type AICatalogRes struct {
	CatalogVersion string               `json:"catalog_version"`
	Objects        []AICatalogObjectRes `json:"objects"`
}

type AIDetectionCandidateRes struct {
	ObjectCode    string  `json:"object_code"`
	DisplayNameID string  `json:"display_name_id"`
	Confidence    float64 `json:"confidence"`
	Category      string  `json:"category"`
}

type AIDetectionRes struct {
	DetectionID string                    `json:"detection_id"`
	Status      string                    `json:"status"`
	ExpiresAt   string                    `json:"expires_at"`
	Candidates  []AIDetectionCandidateRes `json:"candidates"`
}

type AICitationRes struct {
	GuideID string `json:"guide_id"`
	Title   string `json:"title"`
}

type AIRecommendationRes struct {
	RecommendationID  string          `json:"recommendation_id"`
	DetectionID       *string         `json:"detection_id,omitempty"`
	ExpiresAt         string          `json:"expires_at"`
	RecommendedMethod string          `json:"recommended_method"`
	Category          string          `json:"category"`
	Summary           string          `json:"summary"`
	Steps             []string        `json:"steps"`
	Citations         []AICitationRes `json:"citations"`
}
