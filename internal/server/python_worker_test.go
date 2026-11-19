package server

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/waste-not-app/waste-not-backend/internal/config"
)

func TestPythonCombinedDetectionWorker(t *testing.T) {
	detectionURL := os.Getenv("TEST_AI_DETECTION_URL")
	recommendationURL := os.Getenv("TEST_AI_RECOMMENDATION_URL")
	imagePath := os.Getenv("TEST_AI_IMAGE")
	if detectionURL == "" || recommendationURL == "" || imagePath == "" {
		t.Skip("optional real Python combined detector and decision worker test")
	}
	image, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	server, httpServer := setup(t, func(c *config.Config) {
		c.AIEnabled = true
		c.RecommendationEnabled = true
		c.AllowPrototype = true
		c.DetectionURL = detectionURL
		c.RecommendationURL = recommendationURL
		c.DetectionToken = os.Getenv("TEST_AI_DETECTION_TOKEN")
		c.RecommendationToken = os.Getenv("TEST_AI_RECOMMENDATION_TOKEN")
	})
	account := registerUser(t, httpServer.URL, "combined-python@example.com")
	token := account["access_token"].(string)
	status, value := upload(t, httpServer.URL, token, uuid.NewString(), image, "image/jpeg")
	requireStatus(t, status, 201, value)
	detection := value["data"].(map[string]any)
	if detection["model_version"] != "broad-coco-b0f633193403" {
		t.Fatalf("unexpected combined model %v", detection["model_version"])
	}
	var selected map[string]any
	for _, entry := range detection["objects"].([]any) {
		object := entry.(map[string]any)
		for _, key := range object["suggested_object_keys"].([]any) {
			if object["supported_for_review"] == true && key == "glass_bottle" {
				selected = object
				break
			}
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		t.Fatal("public bottle image has no selectable glass-bottle hypothesis")
	}
	input := requestFixture(t, "detected-recycle-request.json")
	input["detection_id"] = detection["detection_id"]
	input["object_id"] = selected["object_id"]
	input["risk_flag"] = true
	input["risk_type"] = "SHARP"
	status, value, _ = call(t, httpServer.URL, "POST", "/v1/ai/recommendations", token, input, nil)
	requireStatus(t, status, 201, value)
	result := value["data"].(map[string]any)
	if result["status"] != "READY" || result["recommended_method"] != "RECYCLE" || result["recommendation_version"] != "pilot-policy-09" || result["caution"] == "" || len(result["source_urls"].([]any)) == 0 {
		t.Fatalf("sharp-glass policy09 bridge mismatch %v", result)
	}
	status, value, _ = call(t, httpServer.URL, "POST", "/v1/ai/recommendations", token, input, nil)
	requireStatus(t, status, 200, value)
	var logs int
	if err := server.DB.QueryRow(context.Background(), `SELECT count(*) FROM waste_logs`).Scan(&logs); err != nil || logs != 0 {
		t.Fatalf("recommendation must await explicit save, logs=%d error=%v", logs, err)
	}
	status, value, _ = call(t, httpServer.URL, "DELETE", "/v1/ai/detections/"+detection["detection_id"].(string), token, nil, nil)
	requireStatus(t, status, 204, value)
}
