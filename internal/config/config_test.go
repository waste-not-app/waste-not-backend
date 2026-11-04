package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestRecommendationFeatureCanBeDisabledSeparately(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("JWT_SECRET_BASE64", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32))))
	t.Setenv("AI_ENABLED", "true")
	t.Setenv("AI_DETECTION_URL", "http://127.0.0.1:8091")
	t.Setenv("AI_RECOMMENDATION_URL", "http://127.0.0.1:8093")
	t.Setenv("AI_SERVICE_TOKEN", strings.Repeat("d", 32))
	t.Setenv("AI_RECOMMENDATION_TOKEN", strings.Repeat("r", 32))
	t.Setenv("AI_RECOMMENDATION_ENABLED", "false")
	c, e := Load()
	if e != nil || !c.AIEnabled || c.RecommendationEnabled {
		t.Fatalf("scan-only config %+v %v", c, e)
	}
	t.Setenv("AI_RECOMMENDATION_ENABLED", "true")
	c, e = Load()
	if e != nil || !c.RecommendationEnabled {
		t.Fatal("recommendations did not enable")
	}
}
