package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/modules/photos"
)

type stalledResetMailer struct {
	*testResetMailer
	started   chan resetMessage
	release   chan struct{}
	once      sync.Once
	failFirst bool
}

func (m *stalledResetMailer) Send(ctx context.Context, email, token string) error {
	first := false
	m.once.Do(func() { first = true })
	if first {
		m.started <- resetMessage{email, token}
		select {
		case <-m.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		if m.failFirst {
			return errors.New("delayed SMTP failure")
		}
		return nil
	}
	return m.testResetMailer.Send(ctx, email, token)
}

func TestPasswordResetSlowSMTPDoesNotBlockRequestsOrAcknowledgeReplacement(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "stale-success"
		if fail {
			name = "stale-failure"
		}
		t.Run(name, func(t *testing.T) {
			mailer := &stalledResetMailer{testResetMailer: &testResetMailer{messages: make(chan resetMessage, 2)}, started: make(chan resetMessage, 1), release: make(chan struct{}), failFirst: fail}
			_, sv := setup(t, nil, mailer)
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(mailer.release) }) }
			defer release()
			registerUser(t, sv.URL, "stalled@example.com")
			st, v, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset", "", map[string]string{"email": "stalled@example.com"}, nil)
			requireStatus(t, st, 202, v)
			var first resetMessage
			select {
			case first = <-mailer.started:
			case <-time.After(5 * time.Second):
				t.Fatal("SMTP send did not start")
			}
			for _, email := range []string{"stalled@example.com", "absent@example.com"} {
				body, _ := json.Marshal(map[string]string{"email": email})
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				req, err := http.NewRequestWithContext(ctx, "POST", sv.URL+"/v1/auth/password-reset", bytes.NewReader(body))
				if err != nil {
					cancel()
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					cancel()
					t.Fatalf("reset request blocked behind SMTP send: %v", err)
				}
				_ = response.Body.Close()
				cancel()
				requireStatus(t, response.StatusCode, 202, nil)
			}
			release()
			latest := mailer.next(t)
			if latest.email != "stalled@example.com" || latest.token == first.token {
				t.Fatal("replacement mail was lost or changed owner")
			}
			for _, entry := range []struct {
				token  string
				status int
			}{{first.token, 422}, {latest.token, 204}} {
				st, v, _ := call(t, sv.URL, "POST", "/v1/auth/password-reset/confirm", "", map[string]string{"token": entry.token, "password": "replacement-passphrase-456"}, nil)
				requireStatus(t, st, entry.status, v)
			}
		})
	}
}

func TestDeleteMeRecoversInterruptedUploadIntentAndTemporaryFile(t *testing.T) {
	for _, renamed := range []bool{false, true} {
		name := "temporary-file"
		if renamed {
			name = "renamed-file"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s, sv := setup(t, func(c *config.Config) { c.PhotoStorageDir = dir })
			a := registerUser(t, sv.URL, "interrupted@example.com")
			b := registerUser(t, sv.URL, "unrelated@example.com")
			ownerID := a["user"].(map[string]any)["id"].(string)
			st, v := uploadPhoto(t, sv.URL, b["access_token"].(string), "image/jpeg", testJPEG(8, 8))
			requireStatus(t, st, 201, v)
			otherPhoto := v["data"].(map[string]any)["id"].(string)
			storage, err := photos.NewLocalStorage(dir)
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.NewString()
			key := ownerID + "/" + id + ".jpg"
			interrupted := errors.New("process interrupted after private file write")
			_, err = photos.NewRepository(s.DB).Create(context.Background(), id, ownerID, key, "image/jpeg", 7, func() error {
				var durable bool
				if err := s.DB.QueryRow(context.Background(), "SELECT uploading FROM photo_cleanup_jobs WHERE object_key=$1", key).Scan(&durable); err != nil || !durable {
					t.Errorf("file write preceded durable upload intent: %v", err)
					return interrupted
				}
				if renamed {
					if err := storage.Put(key, []byte("private")); err != nil {
						return err
					}
				} else {
					if err := os.MkdirAll(filepath.Join(dir, ownerID), 0700); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(dir, key)+".upload", []byte("private"), 0600); err != nil {
						return err
					}
				}
				return interrupted
			})
			if !errors.Is(err, interrupted) {
				t.Fatalf("interruption not propagated: %v", err)
			}
			var persisted int
			if err := s.DB.QueryRow(context.Background(), "SELECT count(*) FROM photos WHERE id=$1", id).Scan(&persisted); err != nil || persisted != 0 {
				t.Fatalf("rolled-back metadata remains: %d %v", persisted, err)
			}
			st, v, _ = call(t, sv.URL, "DELETE", "/v1/me", a["access_token"].(string), map[string]string{"password": "strong-passphrase-123"}, nil)
			requireStatus(t, st, 204, v)
			for _, file := range []string{filepath.Join(dir, key), filepath.Join(dir, key) + ".upload"} {
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatalf("confirmed deletion retained interrupted upload: %s %v", file, err)
				}
			}
			status, _, _ := downloadPhoto(t, sv.URL, b["access_token"].(string), otherPhoto)
			requireStatus(t, status, 200, nil)
		})
	}
}

func TestPhotoCleanupDefersFailureWithoutStarvingOtherOwners(t *testing.T) {
	dir := t.TempDir()
	s, sv := setup(t, func(c *config.Config) { c.PhotoStorageDir = dir })
	a := registerUser(t, sv.URL, "blocked-cleanup@example.com")
	b := registerUser(t, sv.URL, "healthy-cleanup@example.com")
	storage, err := photos.NewLocalStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := photos.NewRepository(s.DB)
	var blockedKey, healthyKey string
	for i, account := range []map[string]any{a, b} {
		st, v := uploadPhoto(t, sv.URL, account["access_token"].(string), "image/jpeg", testJPEG(8, 8))
		requireStatus(t, st, 201, v)
		id := v["data"].(map[string]any)["id"].(string)
		ownerID := account["user"].(map[string]any)["id"].(string)
		key, err := repo.DeleteIfUnused(context.Background(), id, ownerID)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			blockedKey = key
		} else {
			healthyKey = key
		}
	}
	blocked := filepath.Join(dir, blockedKey)
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "blocked"), []byte("storage failure"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(context.Background(), "UPDATE photo_cleanup_jobs SET queued_at=now()-interval '1 hour' WHERE object_key=$1", blockedKey); err != nil {
		t.Fatal(err)
	}
	if err := photos.DrainCleanup(context.Background(), s.DB, storage, "", 100); err == nil {
		t.Fatal("storage failure was hidden")
	}
	if _, err := storage.Get(healthyKey); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("later owner's cleanup starved: %v", err)
	}
	var deferred bool
	if err := s.DB.QueryRow(context.Background(), "SELECT attempts=1 AND available_at>now() FROM photo_cleanup_jobs WHERE object_key=$1", blockedKey).Scan(&deferred); err != nil || !deferred {
		t.Fatalf("bad cleanup job not durably deferred: %v", err)
	}
	st, v, _ := call(t, sv.URL, "DELETE", "/v1/me", a["access_token"].(string), map[string]string{"password": "strong-passphrase-123"}, nil)
	requireStatus(t, st, 503, v)
	if v["error"].(map[string]any)["code"] != "ACCOUNT_DELETION_PENDING" {
		t.Fatal("unresolved deferred owner cleanup claimed complete")
	}
	st, v, _ = call(t, sv.URL, "DELETE", "/v1/me", b["access_token"].(string), map[string]string{"password": "strong-passphrase-123"}, nil)
	requireStatus(t, st, 204, v)
}

func TestDeleteMeDoesNotConfirmLockedUploadIntent(t *testing.T) {
	dir := t.TempDir()
	s, sv := setup(t, func(c *config.Config) { c.PhotoStorageDir = dir })
	a := registerUser(t, sv.URL, "locked-intent@example.com")
	ownerID := a["user"].(map[string]any)["id"].(string)
	key := ownerID + "/" + uuid.NewString() + ".jpg"
	if _, err := s.DB.Exec(context.Background(), "INSERT INTO photo_cleanup_jobs(object_key,uploading,available_at) VALUES($1,true,now()+interval '2 minutes')", key); err != nil {
		t.Fatal(err)
	}
	storage, err := photos.NewLocalStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Put(key, []byte("private")); err != nil {
		t.Fatal(err)
	}
	worker, err := s.DB.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Rollback(context.Background())
	if _, err := worker.Exec(context.Background(), "SELECT object_key FROM photo_cleanup_jobs WHERE object_key=$1 FOR UPDATE", key); err != nil {
		t.Fatal(err)
	}
	st, v, _ := call(t, sv.URL, "DELETE", "/v1/me", a["access_token"].(string), map[string]string{"password": "strong-passphrase-123"}, nil)
	requireStatus(t, st, 503, v)
	if v["error"].(map[string]any)["code"] != "ACCOUNT_DELETION_PENDING" {
		t.Fatal("locked upload intent produced false purge confirmation")
	}
	if err := worker.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := photos.DrainCleanup(context.Background(), s.DB, storage, ownerID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Get(key); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("released upload intent was not cleaned: %v", err)
	}
}

func TestPhotoUploadIntentRejectsAnotherOwnersObjectKey(t *testing.T) {
	s, sv := setup(t, nil)
	a := registerUser(t, sv.URL, "intent-owner@example.com")
	b := registerUser(t, sv.URL, "intent-other@example.com")
	ownerID := a["user"].(map[string]any)["id"].(string)
	otherID := b["user"].(map[string]any)["id"].(string)
	wrote := false
	_, err := photos.NewRepository(s.DB).Create(context.Background(), uuid.NewString(), ownerID, otherID+"/"+uuid.NewString()+".jpg", "image/jpeg", 7, func() error { wrote = true; return nil })
	if err == nil || wrote {
		t.Fatal("upload intent crossed the authenticated owner")
	}
	var count int
	if err := s.DB.QueryRow(context.Background(), "SELECT count(*) FROM photo_cleanup_jobs").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cross-owner cleanup intent persisted: %d %v", count, err)
	}
}
