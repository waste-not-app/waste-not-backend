package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/database"
)

func testJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 25, G: 91, B: 61, A: 255})
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}

func uploadPhoto(t *testing.T, base, token, contentType string, fileBytes []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="image"; filename="sample.jpg"`)
	h.Set("Content-Type", contentType)
	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(fileBytes); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	req, err := http.NewRequest("POST", base+"/v1/photos", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	v := map[string]any{}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode photo response %d: %v", resp.StatusCode, err)
	}
	checkCoreResponse(t, "POST", "/v1/photos", resp.StatusCode, v)
	return resp.StatusCode, v
}

func downloadPhoto(t *testing.T, base, token, id string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest("GET", base+"/v1/photos/"+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw, resp.Header
}

func findAchievement(items []any, code string) map[string]any {
	for _, entry := range items {
		m := entry.(map[string]any)
		if m["code"] == code {
			return m
		}
	}
	return nil
}

func TestAchievementsLifecycleAndRelock(t *testing.T) {
	_, sv := setup(t, nil)
	a := registerUser(t, sv.URL, "ach-a@example.com")
	b := registerUser(t, sv.URL, "ach-b@example.com")
	token := a["access_token"].(string)
	other := b["access_token"].(string)
	today := time.Now().In(mustZone("Asia/Jakarta")).Format(time.DateOnly)

	st, v, _ := call(t, sv.URL, "GET", "/v1/metadata", token, nil, nil)
	requireStatus(t, st, 200, v)
	caps := v["data"].(map[string]any)["capabilities"].(map[string]any)
	if caps["achievements"] != true || caps["photos"] != false {
		t.Fatalf("unexpected capabilities: %v", caps)
	}

	st, v, _ = call(t, sv.URL, "GET", "/v1/achievements", token, nil, nil)
	requireStatus(t, st, 200, v)
	items := v["data"].([]any)
	if len(items) != 4 {
		t.Fatalf("expected 4 baseline achievements, got %d", len(items))
	}
	if first := findAchievement(items, "FIRST_STEP"); first == nil || first["is_unlocked"] != false || first["unlocked_at"] != nil {
		t.Fatalf("unexpected initial FIRST_STEP: %v", first)
	}

	var logIDs []string
	for range 3 {
		st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, logInput(today), nil)
		requireStatus(t, st, 201, v)
		logIDs = append(logIDs, v["data"].(map[string]any)["id"].(string))
	}
	st, v, _ = call(t, sv.URL, "GET", "/v1/achievements", token, nil, nil)
	requireStatus(t, st, 200, v)
	items = v["data"].([]any)
	first := findAchievement(items, "FIRST_STEP")
	recycle := findAchievement(items, "SORT_TO_RECYCLE")
	gettingUsed := findAchievement(items, "GETTING_USED")
	if first["is_unlocked"] != true || first["unlocked_at"] == nil {
		t.Fatalf("FIRST_STEP should unlock: %v", first)
	}
	if recycle["is_unlocked"] != true || recycle["current_value"] != float64(3) || recycle["unlocked_at"] == nil {
		t.Fatalf("SORT_TO_RECYCLE should unlock: %v", recycle)
	}
	if gettingUsed["is_unlocked"] != false || gettingUsed["current_value"] != float64(3) {
		t.Fatalf("GETTING_USED should be 3/5 locked: %v", gettingUsed)
	}
	firstUnlockedAt := first["unlocked_at"].(string)

	// User B remains isolated at 0/1.
	st, v, _ = call(t, sv.URL, "GET", "/v1/achievements", other, nil, nil)
	requireStatus(t, st, 200, v)
	if otherFirst := findAchievement(v["data"].([]any), "FIRST_STEP"); otherFirst["is_unlocked"] != false || otherFirst["current_value"] != float64(0) {
		t.Fatalf("account isolation failed: %v", otherFirst)
	}

	// Edit one RECYCLE log to TRASH -> SORT_TO_RECYCLE relocks while FIRST_STEP preserves unlocked_at.
	st, v, _ = call(t, sv.URL, "PATCH", "/v1/waste-logs/"+logIDs[0], token, map[string]any{"version": 1, "method": "TRASH"}, nil)
	requireStatus(t, st, 200, v)

	st, v, _ = call(t, sv.URL, "GET", "/v1/achievements", token, nil, nil)
	requireStatus(t, st, 200, v)
	items = v["data"].([]any)
	first = findAchievement(items, "FIRST_STEP")
	recycle = findAchievement(items, "SORT_TO_RECYCLE")
	if first["is_unlocked"] != true || first["unlocked_at"] != firstUnlockedAt {
		t.Fatalf("FIRST_STEP unlocked_at changed: %v != %s", first["unlocked_at"], firstUnlockedAt)
	}
	if recycle["is_unlocked"] != false || recycle["current_value"] != float64(2) || recycle["unlocked_at"] != nil {
		t.Fatalf("SORT_TO_RECYCLE did not relock: %v", recycle)
	}

	// Delete all logs -> FIRST_STEP relocks too.
	for i, id := range logIDs {
		ver := 1
		if i == 0 {
			ver = 2
		}
		st, v, _ = call(t, sv.URL, "DELETE", "/v1/waste-logs/"+id, token, nil, map[string]string{"If-Match": fmt.Sprintf("%q", fmt.Sprintf("%d", ver))})
		requireStatus(t, st, 204, v)
	}

	st, v, _ = call(t, sv.URL, "GET", "/v1/achievements", token, nil, nil)
	requireStatus(t, st, 200, v)
	if first := findAchievement(v["data"].([]any), "FIRST_STEP"); first["is_unlocked"] != false || first["current_value"] != float64(0) || first["unlocked_at"] != nil {
		t.Fatalf("FIRST_STEP did not relock after delete: %v", first)
	}
}

func TestPhotosLifecycleAndIsolation(t *testing.T) {
	s, sv := setup(t, func(c *config.Config) {
		c.PhotoStorageDir = t.TempDir()
	})
	a := registerUser(t, sv.URL, "photo-a@example.com")
	b := registerUser(t, sv.URL, "photo-b@example.com")
	token := a["access_token"].(string)
	other := b["access_token"].(string)
	today := time.Now().In(mustZone("Asia/Jakarta")).Format(time.DateOnly)

	st, v, _ := call(t, sv.URL, "GET", "/v1/metadata", token, nil, nil)
	requireStatus(t, st, 200, v)
	if v["data"].(map[string]any)["capabilities"].(map[string]any)["photos"] != true {
		t.Fatal("photos capability should be true")
	}

	// Reject unsupported MIME and corrupt image.
	st, v = uploadPhoto(t, sv.URL, token, "text/plain", []byte("not-an-image"))
	requireStatus(t, st, 415, v)
	st, v = uploadPhoto(t, sv.URL, token, "image/jpeg", []byte("not-a-jpeg"))
	requireStatus(t, st, 422, v)

	// Upload valid JPEG.
	st, v = uploadPhoto(t, sv.URL, token, "image/jpeg", testJPEG(24, 24))
	requireStatus(t, st, 201, v)
	pData := v["data"].(map[string]any)
	photo1 := pData["id"].(string)
	if pData["status"] != "STAGED" || pData["preview_url"] != "/v1/photos/"+photo1 {
		t.Fatalf("unexpected photo payload: %v", pData)
	}

	// Owner download succeeds with private cache headers; cross-account download/delete returns 404.
	st, bytes1, hdr := downloadPhoto(t, sv.URL, token, photo1)
	if st != 200 || len(bytes1) == 0 || hdr.Get("Cache-Control") != "private, no-store" || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("owner download failed: status=%d len=%d hdr=%v", st, len(bytes1), hdr)
	}
	st, _, _ = downloadPhoto(t, sv.URL, other, photo1)
	requireStatus(t, st, 404, nil)
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/photos/"+photo1, other, nil, nil)
	requireStatus(t, st, 404, v)

	// Cross-account user cannot attach user A's photo.
	otherIn := logInput(today)
	otherIn["photo_id"] = photo1
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", other, otherIn, nil)
	requireStatus(t, st, 422, v)

	// Create log with photo1 + verify idempotent replay.
	in := logInput(today)
	in["photo_id"] = photo1
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
	requireStatus(t, st, 201, v)
	logID := v["data"].(map[string]any)["id"].(string)
	if v["data"].(map[string]any)["photo_id"] != photo1 {
		t.Fatalf("created log missing photo_id: %v", v["data"])
	}
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, in, nil)
	requireStatus(t, st, 200, v)

	// Second log cannot reuse already-attached photo1.
	in2 := logInput(today)
	in2["photo_id"] = photo1
	st, v, _ = call(t, sv.URL, "POST", "/v1/waste-logs", token, in2, nil)
	requireStatus(t, st, 409, v)

	// Cannot delete photo1 while attached.
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/photos/"+photo1, token, nil, nil)
	requireStatus(t, st, 409, v)

	// Replace photo1 with photo2 via PATCH -> photo1 immediately returns 404.
	st, v = uploadPhoto(t, sv.URL, token, "image/jpeg", testJPEG(20, 20))
	requireStatus(t, st, 201, v)
	photo2 := v["data"].(map[string]any)["id"].(string)

	st, v, _ = call(t, sv.URL, "PATCH", "/v1/waste-logs/"+logID, token, map[string]any{"version": 1, "photo_id": photo2}, nil)
	requireStatus(t, st, 200, v)
	if v["data"].(map[string]any)["photo_id"] != photo2 {
		t.Fatalf("patched log did not update photo_id: %v", v["data"])
	}
	st, _, _ = downloadPhoto(t, sv.URL, token, photo1)
	requireStatus(t, st, 404, nil)

	// Soft-deleting the log immediately revokes access to photo2.
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/waste-logs/"+logID, token, nil, map[string]string{"If-Match": `"2"`})
	requireStatus(t, st, 204, v)
	st, _, _ = downloadPhoto(t, sv.URL, token, photo2)
	requireStatus(t, st, 404, nil)

	// Staged photo delete is 204 and idempotent.
	st, v = uploadPhoto(t, sv.URL, token, "image/jpeg", testJPEG(18, 18))
	requireStatus(t, st, 201, v)
	photo3 := v["data"].(map[string]any)["id"].(string)
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/photos/"+photo3, token, nil, nil)
	requireStatus(t, st, 204, v)
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/photos/"+photo3, token, nil, nil)
	requireStatus(t, st, 204, v)
	st, _, _ = downloadPhoto(t, sv.URL, token, photo3)
	requireStatus(t, st, 404, nil)

	// Orphan staged photo (>24h) is revoked by database.Cleanup.
	st, v = uploadPhoto(t, sv.URL, token, "image/jpeg", testJPEG(16, 16))
	requireStatus(t, st, 201, v)
	photo4 := v["data"].(map[string]any)["id"].(string)
	if _, err := s.DB.Exec(context.Background(), `UPDATE photos SET created_at = now() - interval '25 hours' WHERE id = $1`, photo4); err != nil {
		t.Fatal(err)
	}
	if err := database.Cleanup(context.Background(), s.DB); err != nil {
		t.Fatal(err)
	}
	st, _, _ = downloadPhoto(t, sv.URL, token, photo4)
	requireStatus(t, st, 404, nil)
}
