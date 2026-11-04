package ai

import (
	"encoding/json"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"os"
	"testing"
)

func TestContractExamples(t *testing.T) {
	c, e := New(config.Config{})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ file, schema string }{{"detection-response.json", "DetectionSession"}, {"recycle-response.json", "RecommendationSession"}, {"conditional-cup-response.json", "RecommendationSession"}, {"needs-context-response.json", "RecommendationSession"}, {"provider-unavailable-response.json", "RecommendationSession"}, {"internal-recommendation-response.json", "RecommendationResult"}, {"catalog-response.json", "ObjectCatalog"}} {
		t.Run(tc.file, func(t *testing.T) {
			raw, e := os.ReadFile("../../contracts/ai/examples/" + tc.file)
			if e != nil {
				t.Fatal(e)
			}
			var env map[string]any
			_ = json.Unmarshal(raw, &env)
			if e = c.Validate(tc.schema, env["data"]); e != nil {
				t.Fatal(e)
			}
		})
	}
	t.Run("invalid status and evidence", func(t *testing.T) {
		raw, _ := os.ReadFile("../../contracts/ai/examples/internal-recommendation-response.json")
		var env map[string]any
		_ = json.Unmarshal(raw, &env)
		v := env["data"].(map[string]any)
		v["source_urls"] = []any{}
		if c.Validate("RecommendationResult", v) == nil {
			t.Fatal("READY without sources accepted")
		}
		v["recommended_method"] = "BURN"
		if c.Validate("RecommendationResult", v) == nil {
			t.Fatal("unknown method accepted")
		}
	})
	t.Run("public spoof rejected", func(t *testing.T) {
		raw, _ := os.ReadFile("../../contracts/ai/examples/recycle-request.json")
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		v["confidence"] = 0.99
		if c.NormalizeInput(v) == nil {
			t.Fatal("spoofed confidence accepted")
		}
	})
}
