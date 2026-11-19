package aiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/config"
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

func TestRulesFallbackRetainsProductionGate(t *testing.T) {
	for _, prototype := range []bool{false, true} {
		t.Run(map[bool]string{false: "published rules accepted", true: "draft rules rejected"}[prototype], func(t *testing.T) {
			raw, err := os.ReadFile("../../contracts/ai/examples/internal-recommendation-response.json")
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]any
			if err = json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			result := envelope["data"].(map[string]any)
			result["provider"] = "rules"
			result["prototype_only"] = prototype
			result["reason_code"] = "LLM_UNAVAILABLE"
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(envelope)
			}))
			defer worker.Close()
			client, err := New(config.Config{RecommendationURL: worker.URL, AllowPrototype: false})
			if err != nil {
				t.Fatal(err)
			}
			value, err := client.Recommend(context.Background(), "rules-fallback-test", map[string]any{})
			if prototype {
				if err == nil {
					t.Fatal("production accepted a draft rules recommendation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if value["provider"] != "rules" || value["reason_code"] != "LLM_UNAVAILABLE" || value["recommended_method"] != "RECYCLE" {
				t.Fatalf("fallback provenance lost: %v", value)
			}
		})
	}
}

func TestCatalogServesStaleSnapshotDuringRefresh(t *testing.T) {
	client, err := New(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Catalog(context.Background(), "cold"); err == nil {
		t.Fatal("cold catalog without upstream must fail")
	}
	stale := map[string]any{"catalog_version": "stale"}
	client.catalogCache.Store(&catalogSnapshot{value: stale, until: time.Now().Add(-time.Second)})
	client.catalogMu.Lock()
	defer client.catalogMu.Unlock()
	got, err := client.Catalog(context.Background(), "concurrent")
	if err != nil || got["catalog_version"] != "stale" {
		t.Fatalf("expected stale catalog during refresh, got %v %v", got, err)
	}
}

func TestValidDetectionRejectsMalformedShapes(t *testing.T) {
	for _, v := range []map[string]any{
		{},
		{"model": "not-an-object"},
		{"model": map[string]any{"model_version": "x", "class_names": "bad"}, "model_version": "x"},
	} {
		if validDetection(v) {
			t.Fatalf("malformed detection accepted: %v", v)
		}
	}
}
