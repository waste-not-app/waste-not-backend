package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/modules/auth"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

const (
	googleChallengePath = "/v1/auth/google/challenge"
	googleLoginPath     = "/v1/auth/google"
	legacyPassword      = "strong-passphrase-123"
)

type googleTokenFixture struct{ tokens sync.Map }

func (v *googleTokenFixture) Verify(_ context.Context, token string) (auth.GoogleIdentity, error) {
	identity, ok := v.tokens.Load(token)
	if !ok {
		return auth.GoogleIdentity{}, auth.ErrUnauthenticated
	}
	return identity.(auth.GoogleIdentity), nil
}

type googleVerifierFunc func(context.Context, string) (auth.GoogleIdentity, error)

func (verify googleVerifierFunc) Verify(ctx context.Context, token string) (auth.GoogleIdentity, error) {
	return verify(ctx, token)
}

func (v *googleTokenFixture) issue(t *testing.T, base, subject, email, name string) string {
	t.Helper()
	status, body, _ := call(t, base, http.MethodPost, googleChallengePath, "", nil, nil)
	requireStatus(t, status, http.StatusOK, body)
	nonce := body["data"].(map[string]any)["nonce"].(string)
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != nonce {
		t.Fatal("Google challenge did not contain a 32-byte base64url nonce")
	}
	token := "test-google-token-" + nonce
	identity := auth.GoogleIdentity{Subject: subject, Email: email, Name: name, Nonce: nonce}
	if _, used := v.tokens.LoadOrStore(token, identity); used {
		t.Fatal("Google challenge reused a nonce")
	}
	return token
}

func googleLogin(t *testing.T, base, token string, password ...string) (int, map[string]any) {
	t.Helper()
	payload := map[string]string{"id_token": token, "timezone": "Asia/Jakarta"}
	if len(password) > 0 {
		payload["current_password"] = password[0]
	}
	status, body, _ := call(t, base, http.MethodPost, googleLoginPath, "", payload, nil)
	return status, body
}

func requireGoogleError(t *testing.T, status, wantStatus int, body map[string]any, code string) {
	t.Helper()
	requireStatus(t, status, wantStatus, body)
	if got := body["error"].(map[string]any)["code"]; got != code {
		t.Fatalf("error code = %v, want %s", got, code)
	}
}

func TestGoogleAuthDisabledPreservesPasswordAccountDeletion(t *testing.T) {
	_, sv := setup(t, nil)
	for _, path := range []string{googleChallengePath, googleLoginPath} {
		status, body, headers := call(t, sv.URL, http.MethodPost, path, "", nil, nil)
		requireGoogleError(t, status, http.StatusServiceUnavailable, body, "GOOGLE_AUTH_UNAVAILABLE")
		if headers.Get("Retry-After") == "" {
			t.Fatal("Google service unavailable response has no retry header")
		}
	}
	registration := registerUser(t, sv.URL, "disabled-google@example.com")
	access := registration["access_token"].(string)
	status, body, _ := call(t, sv.URL, http.MethodDelete, "/v1/me", access, map[string]string{"google_id_token": "test-token"}, nil)
	requireGoogleError(t, status, http.StatusServiceUnavailable, body, "GOOGLE_AUTH_UNAVAILABLE")
	status, body, _ = call(t, sv.URL, http.MethodDelete, "/v1/me", access, map[string]string{"password": legacyPassword}, nil)
	requireStatus(t, status, http.StatusNoContent, body)
}

func TestGoogleAuthCreatesPasswordlessAccountWithoutPasswordRecovery(t *testing.T) {
	fixture := &googleTokenFixture{}
	mailer := &testResetMailer{messages: make(chan resetMessage, 1)}
	s, sv := setupWithGoogleVerifier(t, nil, fixture, mailer)
	const email = "google-only@example.com"
	token := fixture.issue(t, sv.URL, "101", "GOOGLE-ONLY@example.com", " Google User ")
	status, body := googleLogin(t, sv.URL, token)
	requireStatus(t, status, http.StatusOK, body)
	session := body["data"].(map[string]any)
	user := session["user"].(map[string]any)
	if user["email"] != email || user["name"] != "Google User" || user["timezone"] != "Asia/Jakarta" || user["has_password"] != false {
		t.Fatal("Google sign-up did not create the expected passwordless profile", user)
	}
	access := session["access_token"].(string)
	status, body, _ = call(t, sv.URL, http.MethodGet, "/v1/me", access, nil, nil)
	requireStatus(t, status, http.StatusOK, body)
	if current := body["data"].(map[string]any); current["id"] != user["id"] || current["has_password"] != false {
		t.Fatal("Google session did not authenticate the passwordless account", current)
	}
	status, body, _ = call(t, sv.URL, http.MethodPost, "/v1/auth/password-reset", "", map[string]string{"email": email}, nil)
	requireStatus(t, status, http.StatusAccepted, body)
	var queued bool
	if err := s.DB.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM password_reset_tokens WHERE user_id=$1) OR EXISTS(SELECT 1 FROM password_reset_mail WHERE user_id=$1)`, user["id"]).Scan(&queued); err != nil || queued {
		t.Fatal("password reset left recovery material for a Google-only account", err)
	}
	// An existing reset token still cannot add a password to an account that has none.
	var rawRecovery [32]byte
	rawRecovery[0] = 1
	recovery := base64.RawURLEncoding.EncodeToString(rawRecovery[:])
	if _, err := s.DB.Exec(t.Context(), `INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES ($1,$2,now()+interval '15 minutes')`, user["id"], security.Digest([]byte(recovery))); err != nil {
		t.Fatal(err)
	}
	status, body, _ = call(t, sv.URL, http.MethodPost, "/v1/auth/password-reset/confirm", "", map[string]string{"token": recovery, "password": legacyPassword}, nil)
	requireGoogleError(t, status, http.StatusUnprocessableEntity, body, "INVALID_RESET_TOKEN")
	status, body, _ = call(t, sv.URL, http.MethodPost, "/v1/auth/login", "", map[string]string{"email": email, "password": legacyPassword}, nil)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")
	status, body, _ = call(t, sv.URL, http.MethodPost, "/v1/auth/refresh", "", map[string]any{"refresh_token": session["refresh_token"]}, nil)
	requireStatus(t, status, http.StatusOK, body)
}

func TestGoogleAuthLinksOnlyWithCurrentPasswordAndKeepsCanonicalIdentity(t *testing.T) {
	fixture := &googleTokenFixture{}
	_, sv := setupWithGoogleVerifier(t, nil, fixture)
	const email = "legacy-google@example.com"
	legacy := registerUser(t, sv.URL, email)["user"].(map[string]any)
	first := fixture.issue(t, sv.URL, "201", email, "Google Profile")
	status, body := googleLogin(t, sv.URL, first)
	requireGoogleError(t, status, http.StatusConflict, body, "GOOGLE_LINK_REQUIRED")
	status, body = googleLogin(t, sv.URL, first, legacyPassword)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")
	bad := fixture.issue(t, sv.URL, "201", email, "Google Profile")
	status, body = googleLogin(t, sv.URL, bad, "incorrect-password")
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")
	status, body = googleLogin(t, sv.URL, bad, legacyPassword)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")
	wrongPicker := fixture.issue(t, sv.URL, "202", "selected-different@example.com", "Different Account")
	status, body = googleLogin(t, sv.URL, wrongPicker, legacyPassword)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")

	proven := fixture.issue(t, sv.URL, "201", email, "Google Profile")
	status, body = googleLogin(t, sv.URL, proven, legacyPassword)
	requireStatus(t, status, http.StatusOK, body)
	assertLegacy := func(body map[string]any) {
		t.Helper()
		user := body["data"].(map[string]any)["user"].(map[string]any)
		if user["id"] != legacy["id"] || user["email"] != email || user["name"] != legacy["name"] || user["has_password"] != true {
			t.Fatal("link/sign-in did not preserve the original password profile", user)
		}
	}
	assertLegacy(body)
	conflicting := fixture.issue(t, sv.URL, "202", email, "Different Account")
	status, body = googleLogin(t, sv.URL, conflicting, legacyPassword)
	requireGoogleError(t, status, http.StatusConflict, body, "GOOGLE_IDENTITY_CONFLICT")
	const updated = "existing-different-inbox@example.com"
	registerUser(t, sv.URL, updated)
	returning := fixture.issue(t, sv.URL, "201", updated, "Updated Name")
	status, body = googleLogin(t, sv.URL, returning)
	requireStatus(t, status, http.StatusOK, body)
	assertLegacy(body)
	status, body, _ = call(t, sv.URL, http.MethodPost, "/v1/auth/login", "", map[string]string{"email": email, "password": legacyPassword}, nil)
	requireStatus(t, status, http.StatusOK, body)
	assertLegacy(body)
}

func TestGoogleAuthRejectsUnknownExpiredAndReplayedChallenges(t *testing.T) {
	fixture := &googleTokenFixture{}
	s, sv := setupWithGoogleVerifier(t, nil, fixture)
	status, body := googleLogin(t, sv.URL, "invalid-test-token")
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")

	expired := fixture.issue(t, sv.URL, "301", "expires@example.com", "Expired Account")
	expiredIdentity, _ := fixture.Verify(t.Context(), expired)
	if _, err := s.DB.Exec(t.Context(), `UPDATE google_auth_challenges SET expires_at=clock_timestamp()-interval '1 second' WHERE nonce_hash=$1`, security.Digest([]byte(expiredIdentity.Nonce))); err != nil {
		t.Fatal(err)
	}
	status, body = googleLogin(t, sv.URL, expired)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")

	unknown := expiredIdentity
	unknown.Nonce = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	fixture.tokens.Store("unissued-test-token", unknown)
	status, body = googleLogin(t, sv.URL, "unissued-test-token")
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")

	valid := fixture.issue(t, sv.URL, "301", "expires@example.com", "Current Account")
	status, body = googleLogin(t, sv.URL, valid)
	requireStatus(t, status, http.StatusOK, body)
	status, body = googleLogin(t, sv.URL, valid)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")
}

func TestGoogleDeleteRequiresFreshMatchingSubjectAndPurgesPrivatePhotos(t *testing.T) {
	fixture := &googleTokenFixture{}
	dir := t.TempDir()
	_, sv := setupWithGoogleVerifier(t, func(c *config.Config) { c.PhotoStorageDir = dir }, fixture)
	const email = "reauth@example.com"
	signup := fixture.issue(t, sv.URL, "401", email, "Delete Me")
	status, body := googleLogin(t, sv.URL, signup)
	requireStatus(t, status, http.StatusOK, body)
	session := body["data"].(map[string]any)
	userID := session["user"].(map[string]any)["id"].(string)
	access := session["access_token"].(string)
	status, body = uploadPhoto(t, sv.URL, access, "image/jpeg", testJPEG(8, 8))
	requireStatus(t, status, http.StatusCreated, body)

	wrongSubject := fixture.issue(t, sv.URL, "402", email, "Wrong Google Subject")
	for _, tc := range []struct {
		fields map[string]string
		status int
		code   string
	}{
		{map[string]string{}, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{map[string]string{"google_id_token": signup, "password": legacyPassword}, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{map[string]string{"password": legacyPassword}, http.StatusForbidden, "WRONG_PASSWORD"},
		{map[string]string{"google_id_token": "invalid-test-token"}, http.StatusForbidden, "GOOGLE_REAUTH_FAILED"},
		{map[string]string{"google_id_token": signup}, http.StatusForbidden, "GOOGLE_REAUTH_FAILED"},
		{map[string]string{"google_id_token": wrongSubject}, http.StatusForbidden, "GOOGLE_REAUTH_FAILED"},
	} {
		status, body, _ = call(t, sv.URL, http.MethodDelete, "/v1/me", access, tc.fields, nil)
		requireGoogleError(t, status, tc.status, body, tc.code)
	}
	status, body = googleLogin(t, sv.URL, wrongSubject)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "UNAUTHENTICATED")

	fresh := fixture.issue(t, sv.URL, "401", "updated-reauth@example.com", "Updated Google Name")
	status, body, _ = call(t, sv.URL, http.MethodDelete, "/v1/me", access, map[string]string{"google_id_token": fresh}, nil)
	requireStatus(t, status, http.StatusNoContent, body)
	status, body, _ = call(t, sv.URL, http.MethodGet, "/v1/me", access, nil, nil)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "SESSION_EXPIRED")
	entries, err := os.ReadDir(filepath.Join(dir, userID))
	if (err != nil && !os.IsNotExist(err)) || len(entries) != 0 {
		t.Fatal("private photos were not purged on successful Google account deletion", err)
	}
	status, body, _ = call(t, sv.URL, http.MethodPost, "/v1/auth/refresh", "", map[string]any{"refresh_token": session["refresh_token"]}, nil)
	requireGoogleError(t, status, http.StatusUnauthorized, body, "SESSION_EXPIRED")
}

func TestGoogleAuthSerializesSharedChallengeAndConcurrentFirstSignIn(t *testing.T) {
	fixture := &googleTokenFixture{}
	s, first := setupWithGoogleVerifier(t, nil, fixture)
	replica, err := newServerWithGoogle(s.DB, s.Config, s.Log, nil, fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	second := httptest.NewServer(replica.Handler())
	defer second.Close()

	type result struct {
		status int
		body   map[string]any
	}
	exchangeBoth := func(left, right string) [2]result {
		t.Helper()
		start := make(chan struct{})
		var results [2]result
		var wg sync.WaitGroup
		for i, request := range [2]struct{ base, token string }{{first.URL, left}, {second.URL, right}} {
			wg.Go(func() {
				<-start
				results[i].status, results[i].body = googleLogin(t, request.base, request.token)
			})
		}
		close(start)
		wg.Wait()
		return results
	}
	one := fixture.issue(t, first.URL, "501", "one-consumer@example.com", "One Consumer")
	results := exchangeBoth(one, one)
	if results[0].status == http.StatusUnauthorized {
		results[0], results[1] = results[1], results[0]
	}
	requireStatus(t, results[0].status, http.StatusOK, results[0].body)
	requireGoogleError(t, results[1].status, http.StatusUnauthorized, results[1].body, "UNAUTHENTICATED")

	a := fixture.issue(t, first.URL, "502", "two-challenges@example.com", "Concurrent Sign-in")
	b := fixture.issue(t, second.URL, "502", "two-challenges@example.com", "Concurrent Sign-in")
	results = exchangeBoth(a, b)
	for _, res := range results {
		requireStatus(t, res.status, http.StatusOK, res.body)
	}
	left := results[0].body["data"].(map[string]any)
	right := results[1].body["data"].(map[string]any)
	if left["user"].(map[string]any)["id"] != right["user"].(map[string]any)["id"] || left["session_id"] == right["session_id"] {
		t.Fatal("concurrent Google sign-ins did not share one account with independent sessions")
	}

	a = fixture.issue(t, first.URL, "503", "same-email@example.com", "First Identity")
	b = fixture.issue(t, second.URL, "504", "same-email@example.com", "Second Identity")
	results = exchangeBoth(a, b)
	if results[0].status == http.StatusConflict {
		results[0], results[1] = results[1], results[0]
	}
	requireStatus(t, results[0].status, http.StatusOK, results[0].body)
	requireGoogleError(t, results[1].status, http.StatusConflict, results[1].body, "GOOGLE_IDENTITY_CONFLICT")
}

func TestGoogleDeleteRechecksSessionAfterVerifyingProof(t *testing.T) {
	fixture := &googleTokenFixture{}
	started, release := make(chan struct{}), make(chan struct{})
	resume := sync.OnceFunc(func() { close(release) })
	defer resume()
	const reauth = "blocking-test-token"
	verifier := googleVerifierFunc(func(ctx context.Context, token string) (auth.GoogleIdentity, error) {
		if token == reauth {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return auth.GoogleIdentity{}, ctx.Err()
			}
		}
		return fixture.Verify(ctx, token)
	})
	_, sv := setupWithGoogleVerifier(t, nil, verifier)
	const email = "reauth-session@example.com"
	signup := fixture.issue(t, sv.URL, "601", email, "Reauth User")
	status, body := googleLogin(t, sv.URL, signup)
	requireStatus(t, status, http.StatusOK, body)
	session := body["data"].(map[string]any)
	access := session["access_token"].(string)
	originalID := session["user"].(map[string]any)["id"]
	proof := fixture.issue(t, sv.URL, "601", email, "Reauth User")
	identity, _ := fixture.Verify(t.Context(), proof)
	fixture.tokens.Store(reauth, identity)

	var deleteStatus int
	var deleteBody map[string]any
	var pending sync.WaitGroup
	pending.Go(func() {
		deleteStatus, deleteBody, _ = call(t, sv.URL, http.MethodDelete, "/v1/me", access, map[string]string{"google_id_token": reauth}, nil)
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not start Google verification")
	}
	status, body, _ = call(t, sv.URL, http.MethodPost, "/v1/auth/logout", access, nil, nil)
	requireStatus(t, status, http.StatusNoContent, body)
	resume()
	pending.Wait()
	requireGoogleError(t, deleteStatus, http.StatusUnauthorized, deleteBody, "SESSION_EXPIRED")

	fresh := fixture.issue(t, sv.URL, "601", email, "Reauth User")
	status, body = googleLogin(t, sv.URL, fresh)
	requireStatus(t, status, http.StatusOK, body)
	if body["data"].(map[string]any)["user"].(map[string]any)["id"] != originalID {
		t.Fatal("a request using the revoked session deleted the account")
	}
}
