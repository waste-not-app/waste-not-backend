package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/database"
)

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, e := os.ReadFile("../../contracts/ai/examples/" + name)
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if e = json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	return v["data"].(map[string]any)
}
func requestFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, e := os.ReadFile("../../contracts/ai/examples/" + name)
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if e = json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	v["client_request_id"] = uuid.NewString()
	return v
}
func workerFixture(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	detections, recommendations := new(atomic.Int32), new(atomic.Int32)
	mode := new(atomic.Int32)
	sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer service-") {
			w.WriteHeader(401)
			return
		}
		var v map[string]any
		switch r.URL.Path {
		case "/internal/v1/objects":
			v = fixture(t, "catalog-response.json")
		case "/internal/v1/capabilities":
			v = map[string]any{"model": map[string]any{"model_version": "mock-four-class-v1", "sha256": strings.Repeat("a", 64), "class_names": []string{"pet_bottle", "glass_bottle", "metal_can", "hdpe_bottle"}}, "mapping_version": "drinking-waste-reviewed-02", "thresholds": map[string]float64{"lower": 0.5, "offered": 0.7}, "supported_visual_classes": 4, "supported_object_labels": map[string]string{"glass_bottle": "Botol kaca"}}
		case "/internal/v1/detect":
			detections.Add(1)
			raw, e := io.ReadAll(r.Body)
			if e != nil || r.Header.Get("Content-Type") != "image/jpeg" {
				t.Error("invalid detection transport")
			}
			if bytes.Contains(raw, []byte("Exif")) {
				t.Error("EXIF reached worker")
			}
			v = fixture(t, "detection-response.json")
			delete(v, "detection_id")
			delete(v, "expires_at")
			v["model"] = map[string]any{"model_version": "mock-four-class-v1", "sha256": strings.Repeat("a", 64), "class_names": []string{"pet_bottle", "glass_bottle", "metal_can", "hdpe_bottle"}}
		case "/internal/v1/recommendations":
			recommendations.Add(1)
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid recommendation body")
			}
			if body["selection"] != nil {
				sel := body["selection"].(map[string]any)
				if sel["model_sha256"] != strings.Repeat("a", 64) {
					t.Error("missing model checksum")
				}
			}
			if mode.Load() == 1 {
				time.Sleep(100 * time.Millisecond)
				respond(w, 504, map[string]any{"api_version": "1.0.0", "error": map[string]string{"code": "AI_TIMEOUT"}})
				return
			}
			if mode.Load() == 2 {
				time.Sleep(200 * time.Millisecond)
			}
			for mode.Load() == 3 {
				time.Sleep(time.Millisecond)
			}
			v = fixture(t, "internal-recommendation-response.json")
		default:
			w.WriteHeader(404)
			return
		}
		respond(w, 200, map[string]any{"api_version": "1.0.0", "request_id": r.Header.Get("X-Request-ID"), "data": v})
	}))
	t.Cleanup(sv.Close)
	return sv, detections, recommendations, mode
}
func upload(t *testing.T, base, token, id string, content []byte, mime string) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("client_request_id", id)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="image"; filename="capture.jpg"`)
	h.Set("Content-Type", mime)
	part, e := mw.CreatePart(h)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = part.Write(content)
	mw.Close()
	req, _ := http.NewRequest("POST", base+"/v1/ai/detections", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var v map[string]any
	if e = json.NewDecoder(resp.Body).Decode(&v); e != nil {
		t.Fatal(e)
	}
	return resp.StatusCode, v
}
func photo() []byte {
	im := image.NewRGBA(image.Rect(0, 0, 32, 32))
	im.Set(0, 0, color.RGBA{255, 100, 50, 255})
	var b bytes.Buffer
	_ = jpeg.Encode(&b, im, nil)
	return b.Bytes()
}
func TestAIIntegration(t *testing.T) {
	worker, detectCalls, recommendCalls, mode := workerFixture(t)
	s, sv := setup(t, func(c *config.Config) {
		c.AIEnabled = true
		c.RecommendationEnabled = true
		c.AllowPrototype = true
		c.DetectionURL = worker.URL
		c.RecommendationURL = worker.URL
		c.DetectionToken = "service-detection-secret-long"
		c.RecommendationToken = "service-recommendation-secret-long"
	})
	a := registerUser(t, sv.URL, "ai@example.com")
	b := registerUser(t, sv.URL, "ai-other@example.com")
	token, other := a["access_token"].(string), b["access_token"].(string)
	st, v, _ := call(t, sv.URL, "GET", "/v1/ai/objects", token, nil, nil)
	requireStatus(t, st, 200, v)
	id := uuid.NewString()
	st, v = upload(t, sv.URL, token, id, photo(), "image/jpeg")
	requireStatus(t, st, 201, v)
	detectionID := v["data"].(map[string]any)["detection_id"].(string)
	st, v = upload(t, sv.URL, token, id, photo(), "image/jpeg")
	requireStatus(t, st, 200, v)
	if detectCalls.Load() != 1 {
		t.Fatal("duplicate inference")
	}
	modified := photo()
	modified = append(modified, 0)
	st, v = upload(t, sv.URL, token, id, modified, "image/jpeg")
	requireStatus(t, st, 409, v)
	in := requestFixture(t, "detected-recycle-request.json")
	in["detection_id"] = detectionID
	st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", other, in, nil)
	requireStatus(t, st, 404, v)
	st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", token, in, nil)
	requireStatus(t, st, 201, v)
	recommendationID := v["data"].(map[string]any)["recommendation_id"].(string)
	st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", token, in, nil)
	requireStatus(t, st, 200, v)
	if recommendCalls.Load() != 1 {
		t.Fatal("duplicate generation")
	}
	in["context_revision"] = 2
	st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", token, in, nil)
	requireStatus(t, st, 409, v)
	in["context_revision"] = 1
	in["raw_class"] = "glass_bottle"
	st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", token, in, nil)
	requireStatus(t, st, 422, v)
	delete(in, "raw_class")
	today := time.Now().In(mustZone("Asia/Jakarta")).Format(time.DateOnly)
	log := logInput(today)
	log["category"] = "GLASS"
	log["ai_provenance"] = map[string]any{"recommendation_id": recommendationID, "user_confirmed_object": true, "user_confirmed_method": true}
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, log, nil)
	requireStatus(t, st, 201, v)
	log["client_request_id"] = uuid.NewString()
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, log, nil)
	requireStatus(t, st, 409, v)
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/ai/detections/"+detectionID, other, nil, nil)
	requireStatus(t, st, 404, v)
	for range 2 {
		st, v, _ = call(t, sv.URL, "DELETE", "/v1/ai/detections/"+detectionID, token, nil, nil)
		requireStatus(t, st, 204, v)
	}
	st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", token, in, nil)
	requireStatus(t, st, 410, v)
	var metadataCount int
	if e := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM ai_log_metadata`).Scan(&metadataCount); e != nil || metadataCount != 1 {
		t.Fatal("snapshot not preserved", e)
	}
	t.Run("cross-instance dedup", func(t *testing.T) {
		second, e := New(s.DB, s.Config, s.Log)
		if e != nil {
			t.Fatal(e)
		}
		sv2 := httptest.NewServer(second.Handler())
		defer sv2.Close()
		mode.Store(2)
		defer mode.Store(0)
		request := requestFixture(t, "recycle-request.json")
		before := recommendCalls.Load()
		var wg sync.WaitGroup
		codes := make(chan int, 2)
		for _, base := range []string{sv.URL, sv2.URL} {
			wg.Go(func() { st, _, _ := call(t, base, "POST", "/v1/ai/recommendations", token, request, nil); codes <- st })
		}
		wg.Wait()
		close(codes)
		created := 0
		for status := range codes {
			if status == 201 {
				created++
			} else if status != 200 {
				t.Fatalf("dedup status %d", status)
			}
		}
		if created != 1 || recommendCalls.Load() != before+1 {
			t.Fatal("multiple provider calls")
		}
	})
	t.Run("uncertain outcome cannot retry generation", func(t *testing.T) {
		mode.Store(1)
		defer mode.Store(0)
		request := requestFixture(t, "recycle-request.json")
		before := recommendCalls.Load()
		for range 2 {
			st, v, _ := call(t, sv.URL, "POST", "/v1/ai/recommendations", token, request, nil)
			requireStatus(t, st, 504, v)
		}
		if recommendCalls.Load() != before+1 {
			t.Fatal("retried uncertain generation")
		}
	})
	t.Run("expiry", func(t *testing.T) {
		request := requestFixture(t, "recycle-request.json")
		st, v, _ := call(t, sv.URL, "POST", "/v1/ai/recommendations", token, request, nil)
		requireStatus(t, st, 201, v)
		rid := v["data"].(map[string]any)["recommendation_id"]
		_, e := s.DB.Exec(context.Background(), `UPDATE ai_operations SET expires_at=now()-interval '1 hour' WHERE id=$1`, rid)
		if e != nil {
			t.Fatal(e)
		}
		st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", token, request, nil)
		requireStatus(t, st, 410, v)
	})
}
func TestMediaAndPrototypeGates(t *testing.T) {
	worker, _, recommendCalls, _ := workerFixture(t)
	_, sv := setup(t, func(c *config.Config) {
		c.AIEnabled = true
		c.RecommendationEnabled = true
		c.DetectionURL = worker.URL
		c.RecommendationURL = worker.URL
		c.DetectionToken = "service-detection-secret-long"
		c.RecommendationToken = "service-recommendation-secret-long"
	})
	a := registerUser(t, sv.URL, "media@example.com")
	token := a["access_token"].(string)
	for _, c := range []struct {
		body   []byte
		mime   string
		status int
	}{{[]byte("invalid image"), "image/jpeg", 422}, {photo(), "text/plain", 415}, {photo(), "image/png", 422}, {bytes.Repeat([]byte("x"), maxImage+1), "image/jpeg", 413}} {
		st, v := upload(t, sv.URL, token, uuid.NewString(), c.body, c.mime)
		requireStatus(t, st, c.status, v)
	}
	st, v, _ := call(t, sv.URL, "POST", "/v1/ai/recommendations", token, requestFixture(t, "recycle-request.json"), nil)
	requireStatus(t, st, 503, v)
	if recommendCalls.Load() != 1 {
		t.Fatal("prototype test not executed")
	}
}
func TestGuidePublication(t *testing.T) {
	s, sv := setup(t, nil)
	a := registerUser(t, sv.URL, "guides@example.com")
	token := a["access_token"].(string)
	today := time.Now().UTC().Format(time.DateOnly)
	guides := []database.Guide{{ID: uuid.NewString(), Slug: "test-reviewed", Name: "TEST FIXTURE Botol", Category: "PLASTIC", Keywords: []string{"PET"}, RecommendedMethod: "RECYCLE", Summary: "Synthetic integration fixture, not editorial content.", Steps: []string{"Fixture step"}, Cautions: []string{}, SourceURL: "https://example.org/test-fixture", ReviewedAt: today, Reviewer: "TEST FIXTURE", PublicationStatus: "PUBLISHED"}, {ID: uuid.NewString(), Slug: "test-draft", Name: "TEST FIXTURE draft", Category: "PLASTIC", RecommendedMethod: "RECYCLE", Summary: "Draft fixture", Steps: []string{"Fixture step"}, Cautions: []string{}, SourceURL: "https://example.org/test-fixture", ReviewedAt: today, Reviewer: "TEST FIXTURE", PublicationStatus: "DRAFT"}}
	if e := database.ImportGuides(context.Background(), s.DB, guides); e != nil {
		t.Fatal(e)
	}
	st, v, _ := call(t, sv.URL, "GET", "/v1/waste-guide?q=pet", token, nil, nil)
	requireStatus(t, st, 200, v)
	if len(v["data"].([]any)) != 1 {
		t.Fatal("guide search")
	}
	st, v, _ = call(t, sv.URL, "GET", "/v1/waste-guide/"+guides[1].ID, token, nil, nil)
	requireStatus(t, st, 404, v)
	st, v, _ = call(t, sv.URL, "GET", "/v1/waste-guide?q=%27%20OR%201=1--", token, nil, nil)
	requireStatus(t, st, 200, v)
	if len(v["data"].([]any)) != 0 {
		t.Fatal("query injection")
	}
	guides[0].Reviewer = ""
	if e := database.ImportGuides(context.Background(), s.DB, guides); e == nil {
		t.Fatal("unreviewed published guide accepted")
	}
}
func TestPythonDecisionWorker(t *testing.T) {
	workerURL := os.Getenv("TEST_AI_RECOMMENDATION_URL")
	if workerURL == "" {
		t.Skip("optional live Python decision worker test")
	}
	worker, _, _, _ := workerFixture(t)
	_, sv := setup(t, func(c *config.Config) {
		c.AIEnabled = true
		c.RecommendationEnabled = true
		c.AllowPrototype = true
		c.DetectionURL = worker.URL
		c.RecommendationURL = workerURL
		c.DetectionToken = "service-detection-secret-long"
		c.RecommendationToken = os.Getenv("TEST_AI_RECOMMENDATION_TOKEN")
	})
	a := registerUser(t, sv.URL, "python@example.com")
	token := a["access_token"].(string)
	for _, name := range []string{"recycle-request.json", "reuse-request.json", "reduce-request.json", "trash-request.json", "needs-context-request.json", "conditional-cup-request.json"} {
		t.Run(name, func(t *testing.T) {
			in := requestFixture(t, name)
			st, v, _ := call(t, sv.URL, "POST", "/v1/ai/recommendations", token, in, nil)
			if st != 200 && st != 201 {
				t.Fatalf("Python bridge %d %v", st, v)
			}
			expected := fixture(t, strings.Replace(name, "request", "response", 1))
			result := v["data"].(map[string]any)
			if result["status"] != expected["status"] || result["recommended_method"] != expected["recommended_method"] {
				t.Fatalf("Python mismatch got %v expected %v", result, expected)
			}
		})
	}
}
func TestAIDeleteDuringGenerationAndCrashRecovery(t *testing.T) {
	worker, _, calls, mode := workerFixture(t)
	s, sv := setup(t, func(c *config.Config) {
		c.AIEnabled = true
		c.RecommendationEnabled = true
		c.AllowPrototype = true
		c.DetectionURL = worker.URL
		c.RecommendationURL = worker.URL
		c.DetectionToken = "service-detection-secret-long"
		c.RecommendationToken = "service-recommendation-secret-long"
	})
	a := registerUser(t, sv.URL, "late@example.com")
	token := a["access_token"].(string)
	uid := a["user"].(map[string]any)["id"].(string)
	st, v := upload(t, sv.URL, token, uuid.NewString(), photo(), "image/jpeg")
	requireStatus(t, st, 201, v)
	did := v["data"].(map[string]any)["detection_id"].(string)
	request := requestFixture(t, "detected-recycle-request.json")
	request["detection_id"] = did
	mode.Store(3)
	defer mode.Store(0)
	done := make(chan int, 1)
	go func() { st, _, _ := call(t, sv.URL, "POST", "/v1/ai/recommendations", token, request, nil); done <- st }()
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("provider not reached")
	}
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/ai/detections/"+did, token, nil, nil)
	requireStatus(t, st, 204, v)
	mode.Store(0)
	if got := <-done; got != 410 {
		t.Fatalf("late generation status %d", got)
	}
	mode.Store(0)
	st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", token, request, nil)
	requireStatus(t, st, 410, v)
	if calls.Load() != 1 {
		t.Fatal("late response regenerated")
	}
	stale, expired := uuid.NewString(), uuid.NewString()
	ctx := context.Background()
	_, e := s.DB.Exec(ctx, `INSERT INTO ai_operations(id,user_id,operation,client_request_id,request_hash,state,created_at,expires_at) VALUES($1,$3,'RECOMMENDATION',$1,'hash','IN_PROGRESS',now()-interval '3 minutes',now()+interval '1 hour'),($2,$3,'DETECTION',$2,'hash','COMPLETED',now()-interval '2 days',now()-interval '1 hour')`, stale, expired, uid)
	if e != nil {
		t.Fatal(e)
	}
	if e = database.Cleanup(ctx, s.DB); e != nil {
		t.Fatal(e)
	}
	var state, code string
	if e = s.DB.QueryRow(ctx, `SELECT state,error_code FROM ai_operations WHERE id=$1`, stale).Scan(&state, &code); e != nil || state != "FAILED_UNCERTAIN" || code != "AI_TIMEOUT" {
		t.Fatal("stale job recovery", e, state, code)
	}
	var exists bool
	if e = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_operations WHERE id=$1)`, expired).Scan(&exists); e != nil || exists {
		t.Fatal("TTL cleanup", e)
	}
}
func TestScanOnlyDeployment(t *testing.T) {
	worker, _, calls, _ := workerFixture(t)
	_, sv := setup(t, func(c *config.Config) {
		c.AIEnabled = true
		c.RecommendationEnabled = false
		c.DetectionURL = worker.URL
		c.RecommendationURL = worker.URL
		c.DetectionToken = "service-detection-secret-long"
		c.RecommendationToken = "service-recommendation-secret-long"
	})
	a := registerUser(t, sv.URL, "scan-only@example.com")
	token := a["access_token"].(string)
	st, v, _ := call(t, sv.URL, "GET", "/v1/metadata", token, nil, nil)
	requireStatus(t, st, 200, v)
	caps := v["data"].(map[string]any)["capabilities"].(map[string]any)
	if caps["ai_scan"] != true || caps["ai_recommendation"] != false {
		t.Fatal("incorrect feature state", caps)
	}
	st, v = upload(t, sv.URL, token, uuid.NewString(), photo(), "image/jpeg")
	requireStatus(t, st, 201, v)
	st, v, _ = call(t, sv.URL, "POST", "/v1/ai/recommendations", token, requestFixture(t, "recycle-request.json"), nil)
	requireStatus(t, st, 503, v)
	if calls.Load() != 0 {
		t.Fatal("disabled recommendation called provider")
	}
}
