package api

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/modules/ai"
	"github.com/waste-not-app/waste-not-backend/internal/modules/photos"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

type resetMessage struct{ email, token string }
type testResetMailer struct {
	messages    chan resetMessage
	mu          sync.Mutex
	unavailable bool
	failSends   int
	failed      chan struct{}
}

func (m *testResetMailer) Check(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return errors.New("SMTP unavailable")
	}
	return nil
}
func (m *testResetMailer) Send(ctx context.Context, email, token string) error {
	if err := m.Check(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	if m.failSends > 0 {
		m.failSends--
		m.mu.Unlock()
		if m.failed != nil {
			m.failed <- struct{}{}
		}
		return errors.New("SMTP failed after accepting request")
	}
	m.mu.Unlock()
	select {
	case m.messages <- resetMessage{email, token}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *testResetMailer) next(t *testing.T) resetMessage {
	t.Helper()
	select {
	case message := <-m.messages:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("reset mail was not delivered")
		return resetMessage{}
	}
}

func TestPasswordResetReplacementReplayAndSessionRevocation(t *testing.T) {
	mailer := &testResetMailer{messages: make(chan resetMessage, 8)}
	_, sv := setup(t, nil, mailer)
	a := registerUser(t, sv.URL, "reset@example.com")
	oldAccess := a["access_token"].(string)
	oldRefresh := a["refresh_token"].(string)
	st, v, _ := call(t, sv.URL, "POST", "/v1/auth/login", "", map[string]string{"email": "reset@example.com", "password": "strong-passphrase-123"}, nil)
	requireStatus(t, st, 200, v)
	second := v["data"].(map[string]any)
	request := func(email string) map[string]any {
		st, body, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": email}, nil)
		requireStatus(t, st, 202, body)
		if strings.Contains(body["data"].(map[string]any)["message"].(string), "token") {
			t.Fatal("acknowledgement leaks token detail")
		}
		return body
	}
	known := request("reset@example.com")
	first := mailer.next(t)
	unknown := request("absent@example.com")
	if known["data"].(map[string]any)["message"] != unknown["data"].(map[string]any)["message"] {
		t.Fatal("email enumeration")
	}
	select {
	case <-mailer.messages:
		t.Fatal("unknown account received recovery mail")
	case <-time.After(100 * time.Millisecond):
	}
	request("reset@example.com")
	latest := mailer.next(t)
	if latest.email != "reset@example.com" || len(latest.token) != 43 || latest.token == first.token {
		t.Fatal("invalid reset message")
	}
	confirm := func(token string, want int) {
		st, body, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset/confirm", "", map[string]string{"token": token, "password": "replacement-passphrase-456"}, nil)
		requireStatus(t, st, want, body)
	}
	confirm(first.token, 422)
	confirm(latest.token, 204)
	confirm(latest.token, 422)
	for _, token := range []string{oldAccess, second["access_token"].(string)} {
		st, v, _ = call(t, sv.URL, "GET", "/v1/me", token, nil, nil)
		requireStatus(t, st, 401, v)
	}
	for _, token := range []string{oldRefresh, second["refresh_token"].(string)} {
		st, v, _ = call(t, sv.URL, "POST", "/v1/auth/refresh", "", map[string]string{"refresh_token": token}, nil)
		requireStatus(t, st, 401, v)
	}
	for password, want := range map[string]int{"strong-passphrase-123": 401, "replacement-passphrase-456": 200} {
		st, v, _ = call(t, sv.URL, "POST", "/v1/auth/login", "", map[string]string{"email": "reset@example.com", "password": password}, nil)
		requireStatus(t, st, want, v)
	}
}

func TestPasswordResetExpiryAndUnavailableTransport(t *testing.T) {
	mailer := &testResetMailer{messages: make(chan resetMessage, 4)}
	s, sv := setup(t, nil, mailer)
	registerUser(t, sv.URL, "expiry@example.com")
	st, v, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": "expiry@example.com"}, nil)
	requireStatus(t, st, 202, v)
	message := mailer.next(t)
	_, err := s.DB.Exec(context.Background(), "UPDATE password_reset_tokens SET expires_at=now()-interval '1 second'")
	if err != nil {
		t.Fatal(err)
	}
	st, v, _ = call(t, sv.URL, "POST", "/v1/auth/password-reset/confirm", "", map[string]string{"token": message.token, "password": "replacement-passphrase-456"}, nil)
	requireStatus(t, st, 422, v)
	mailer.mu.Lock()
	mailer.unavailable = true
	mailer.mu.Unlock()
	for _, email := range []string{"expiry@example.com", "absent@example.com"} {
		st, v, _ = call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": email}, nil)
		requireStatus(t, st, 503, v)
	}
	_, disabled := setup(t, nil)
	st, v, _ = call(t, disabled.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": "absent@example.com"}, nil)
	requireStatus(t, st, 503, v)
}

func TestDeleteMePasswordOwnerIsolationAndPrivatePhotoCleanup(t *testing.T) {
	dir := t.TempDir()
	s, sv := setup(t, func(c *config.Config) { c.PhotoStorageDir = dir })
	a := registerUser(t, sv.URL, "delete-a@example.com")
	b := registerUser(t, sv.URL, "delete-b@example.com")
	token, other := a["access_token"].(string), b["access_token"].(string)
	var ownerPhoto, otherPhoto string
	for _, entry := range []struct {
		token string
		id    *string
	}{{token, &ownerPhoto}, {other, &otherPhoto}} {
		st, v := uploadPhoto(t, sv.URL, entry.token, "image/jpeg", testJPEG(8, 8))
		requireStatus(t, st, 201, v)
		*entry.id = v["data"].(map[string]any)["id"].(string)
	}
	today := time.Now().In(mustZone("Asia/Jakarta")).Format(time.DateOnly)
	input := logInput(today)
	input["photo_id"] = ownerPhoto
	st, v, _ := call(t, sv.URL, "POST", "/v1/waste-logs", token, input, nil)
	requireStatus(t, st, 201, v)
	ownerID := a["user"].(map[string]any)["id"].(string)
	otherID := b["user"].(map[string]any)["id"].(string)
	aiRepo := ai.NewRepository(s.DB)
	inflight, _, err := aiRepo.Claim(context.Background(), ownerID, "DETECTION", uuid.NewString(), "owner-operation", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := aiRepo.Claim(context.Background(), otherID, "DETECTION", uuid.NewString(), "other-operation", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	st, secondLogin, _ := call(t, sv.URL, "POST", "/v1/auth/login", "", map[string]string{"email": "delete-a@example.com", "password": "strong-passphrase-123"}, nil)
	requireStatus(t, st, 200, secondLogin)
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/me", token, map[string]string{"password": "incorrect-password"}, nil)
	requireStatus(t, st, 403, v)
	if v["error"].(map[string]any)["fields"].(map[string]any)["password"] == nil {
		t.Fatal("password error missing")
	}
	st, v, _ = call(t, sv.URL, "GET", "/v1/me", token, nil, nil)
	requireStatus(t, st, 200, v)
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/me", token, map[string]string{"password": "strong-passphrase-123"}, nil)
	requireStatus(t, st, 204, v)
	st, v, _ = call(t, sv.URL, "GET", "/v1/me", token, nil, nil)
	requireStatus(t, st, 401, v)
	if completed, err := aiRepo.Complete(context.Background(), ownerID, inflight.ID, "COMPLETED", []byte(`{}`), nil, 200, "", 0); err != nil || completed {
		t.Fatalf("in-flight AI recreated deleted data: %v %v", completed, err)
	}
	st, v, _ = call(t, sv.URL, "GET", "/v1/me", secondLogin["data"].(map[string]any)["access_token"].(string), nil, nil)
	requireStatus(t, st, 401, v)
	st, v, _ = call(t, sv.URL, "GET", "/v1/me", other, nil, nil)
	requireStatus(t, st, 200, v)
	status, _, _ := downloadPhoto(t, sv.URL, other, otherPhoto)
	requireStatus(t, status, 200, nil)
	entries, err := os.ReadDir(filepath.Join(dir, ownerID))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("owner private photo remains after confirmed deletion")
	}
	for _, table := range []string{"users", "sessions", "waste_logs", "photos", "user_achievements", "ai_operations", "password_reset_tokens", "password_reset_mail"} {
		column := "user_id"
		if table == "users" {
			column = "id"
		}
		var count int
		if err := s.DB.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE "+column+"=$1", ownerID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("owner data remains in %s: %d %v", table, count, err)
		}
	}
}

func TestDeleteMeStorageFailureRetainsDurableCleanup(t *testing.T) {
	dir := t.TempDir()
	s, sv := setup(t, func(c *config.Config) { c.PhotoStorageDir = dir })
	a := registerUser(t, sv.URL, "delete-failure@example.com")
	token := a["access_token"].(string)
	ownerID := a["user"].(map[string]any)["id"].(string)
	st, v := uploadPhoto(t, sv.URL, token, "image/jpeg", testJPEG(8, 8))
	requireStatus(t, st, 201, v)
	path := filepath.Join(dir, ownerID, v["data"].(map[string]any)["id"].(string)+".jpg")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "blocked"), []byte("storage failure"), 0600); err != nil {
		t.Fatal(err)
	}
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/me", token, map[string]string{"password": "strong-passphrase-123"}, nil)
	requireStatus(t, st, 503, v)
	if v["error"].(map[string]any)["code"] != "ACCOUNT_DELETION_PENDING" {
		t.Fatal("committed deletion not distinguished from an intact account")
	}
	var pending int
	if err := s.DB.QueryRow(context.Background(), "SELECT count(*) FROM photo_cleanup_jobs").Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("cleanup was lost: %d %v", pending, err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	storage, err := photos.NewLocalStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(context.Background(), "UPDATE photo_cleanup_jobs SET available_at=now() WHERE starts_with(object_key,$1)", ownerID+"/"); err != nil {
		t.Fatal(err)
	}
	if err := photos.DrainCleanup(context.Background(), s.DB, storage, ownerID, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(context.Background(), "SELECT count(*) FROM photo_cleanup_jobs").Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("cleanup acknowledgement: %d %v", pending, err)
	}
}

func TestDeleteMeWaitsForInFlightPrivateUpload(t *testing.T) {
	dir := t.TempDir()
	s, sv := setup(t, func(c *config.Config) { c.PhotoStorageDir = dir })
	a := registerUser(t, sv.URL, "upload-race@example.com")
	token := a["access_token"].(string)
	ownerID := a["user"].(map[string]any)["id"].(string)
	storage, err := photos.NewLocalStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	uploadDone := make(chan error, 1)
	id := uuid.NewString()
	key := ownerID + "/" + id + ".jpg"
	go func() {
		_, err := photos.NewRepository(s.DB).Create(context.Background(), id, ownerID, key, "image/jpeg", 1, func() error {
			close(started)
			<-release
			return storage.Put(key, []byte("private"))
		})
		uploadDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not acquire account lock")
	}
	deleted := make(chan int, 1)
	go func() {
		st, _, _ := call(t, sv.URL, "DELETE", "/v1/me", token, map[string]string{"password": "strong-passphrase-123"}, nil)
		deleted <- st
	}()
	select {
	case status := <-deleted:
		close(release)
		t.Fatalf("deletion overtook upload: %d", status)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-uploadDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not finish")
	}
	select {
	case status := <-deleted:
		requireStatus(t, status, 204, nil)
	case <-time.After(5 * time.Second):
		t.Fatal("deletion did not finish")
	}
	if _, err := storage.Get(key); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("in-flight upload survived deletion: %v", err)
	}
}

func TestPasswordResetRequestsAndTokenAttemptsAreBounded(t *testing.T) {
	mailer := &testResetMailer{messages: make(chan resetMessage, 1)}
	_, sv := setup(t, nil, mailer)
	for range 20 {
		st, v, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": "absent@example.com"}, nil)
		requireStatus(t, st, 202, v)
	}
	st, v, h := call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": "absent@example.com"}, nil)
	requireStatus(t, st, 429, v)
	if h.Get("Retry-After") == "" {
		t.Fatal("rate-limit retry hint missing")
	}
	_, attempts := setup(t, nil, mailer)
	for range 20 {
		st, v, _ := call(t, attempts.URL, "POST", "/v1/auth/password-reset/confirm", "", map[string]string{"token": strings.Repeat("A", 43), "password": "replacement-passphrase-456"}, nil)
		requireStatus(t, st, 422, v)
	}
	st, v, _ = call(t, attempts.URL, "POST", "/v1/auth/password-reset/confirm", "", map[string]string{"token": strings.Repeat("A", 43), "password": "replacement-passphrase-456"}, nil)
	requireStatus(t, st, 429, v)
}

func TestPasswordResetOutboxSurvivesWorkerRestartEncrypted(t *testing.T) {
	mailer := &testResetMailer{messages: make(chan resetMessage, 2)}
	s, sv := setup(t, nil, mailer)
	s.Close()
	registerUser(t, sv.URL, "durable@example.com")
	st, v, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": "durable@example.com"}, nil)
	requireStatus(t, st, 202, v)
	var encrypted []byte
	var digest string
	if err := s.DB.QueryRow(context.Background(), "SELECT m.encrypted_token,t.token_hash FROM password_reset_mail m JOIN password_reset_tokens t USING(user_id)").Scan(&encrypted, &digest); err != nil {
		t.Fatal(err)
	}
	restarted, err := newServer(s.DB, s.Config, s.Log, mailer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	restartedHTTP := httptest.NewServer(restarted.Handler())
	t.Cleanup(restartedHTTP.Close)
	message := mailer.next(t)
	if bytes.Contains(encrypted, []byte(message.token)) || digest != security.Digest([]byte(message.token)) {
		t.Fatal("plaintext token stored or verifier mismatched")
	}
	st, v, _ = call(t, restartedHTTP.URL, "POST", "/v1/auth/password-reset/confirm", "", map[string]string{"token": message.token, "password": "replacement-passphrase-456"}, nil)
	requireStatus(t, st, 204, v)
}

func TestPasswordResetConcurrentConsumptionHasOneWinner(t *testing.T) {
	mailer := &testResetMailer{messages: make(chan resetMessage, 1)}
	_, sv := setup(t, nil, mailer)
	registerUser(t, sv.URL, "concurrent@example.com")
	st, v, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": "concurrent@example.com"}, nil)
	requireStatus(t, st, 202, v)
	message := mailer.next(t)
	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			st, _, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset/confirm", "", map[string]string{"token": message.token, "password": "replacement-passphrase-456"}, nil)
			statuses <- st
		}()
	}
	first, second := <-statuses, <-statuses
	if !((first == 204 && second == 422) || (first == 422 && second == 204)) {
		t.Fatalf("single-use token had unexpected outcomes %d %d", first, second)
	}
}

func TestPasswordResetFailedDeliveryRemainsQueuedAndRetries(t *testing.T) {
	mailer := &testResetMailer{messages: make(chan resetMessage, 1), failSends: 1, failed: make(chan struct{}, 1)}
	s, sv := setup(t, nil, mailer)
	registerUser(t, sv.URL, "retry@example.com")
	st, v, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": "retry@example.com"}, nil)
	requireStatus(t, st, 202, v)
	select {
	case <-mailer.failed:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery attempt missing")
	}
	var attempts int
	if err := s.DB.QueryRow(context.Background(), "SELECT attempts FROM password_reset_mail").Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("failed mail not durably retained: %d %v", attempts, err)
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var message resetMessage
retry:
	for {
		select {
		case message = <-mailer.messages:
			break retry
		case <-deadline.C:
			t.Fatal("durable retry was not delivered")
		case <-tick.C:
			if _, err := s.DB.Exec(context.Background(), "UPDATE password_reset_mail SET available_at=now()"); err != nil {
				t.Fatal(err)
			}
		}
	}
	st, v, _ = call(t, sv.URL, "POST", "/v1/auth/password-reset/confirm", "", map[string]string{"token": message.token, "password": "replacement-passphrase-456"}, nil)
	requireStatus(t, st, 204, v)
}
