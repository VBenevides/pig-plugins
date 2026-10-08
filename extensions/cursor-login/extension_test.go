package cursorlogin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

type fakeUI struct{ auth []sdk.OAuthAuthInfo }

func (f *fakeUI) OnAuth(i sdk.OAuthAuthInfo) { f.auth = append(f.auth, i) }
func (f *fakeUI) OnProgress(string)          {}

func testJWT(t *testing.T, exp time.Time) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"exp": exp.Unix()})
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString(payload) + ".sig"
}

func TestLoginPollsUntilApprovedAndVerifiesPKCE(t *testing.T) {
	exp := time.Now().Add(time.Hour).Truncate(time.Second)
	access := testJWT(t, exp)
	var polls atomic.Int32
	var pkceVerifier atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/poll" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if polls.Add(1) < 3 {
			http.NotFound(w, r)
			return
		}
		pkceVerifier.Store(r.URL.Query().Get("verifier"))
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: access, RefreshToken: "refresh-1"})
	}))
	defer srv.Close()

	c := &client{http: srv.Client(), loginURL: "https://login.test/x", apiBaseURL: srv.URL, pollInterval: time.Millisecond, timeout: time.Second}
	ui := &fakeUI{}
	creds, err := c.login(context.Background(), ui)
	if err != nil {
		t.Fatal(err)
	}
	if polls.Load() != 3 {
		t.Fatalf("polls = %d, want 3", polls.Load())
	}
	if creds.Access != access || creds.Refresh != "refresh-1" {
		t.Fatalf("unexpected credentials %+v", creds)
	}
	if want := exp.Add(-expiryMargin).UnixMilli(); creds.Expires != want {
		t.Fatalf("expires = %d, want %d", creds.Expires, want)
	}
	u, err := url.Parse(ui.auth[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(pkceVerifier.Load().(string)))
	if got := u.Query().Get("challenge"); got != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatalf("challenge does not match verifier")
	}
	if u.Query().Get("mode") != "login" || u.Query().Get("redirectTarget") != "cli" {
		t.Fatalf("unexpected login url %s", ui.auth[0].URL)
	}
}

func TestLoginTimesOutWhileStillPending(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := &client{http: srv.Client(), loginURL: "https://login.test/x", apiBaseURL: srv.URL, pollInterval: time.Millisecond, timeout: 20 * time.Millisecond}
	if _, err := c.login(context.Background(), &fakeUI{}); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestLoginReportsServerFailureWithoutTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "secret-body", 500) }))
	defer srv.Close()
	c := &client{http: srv.Client(), loginURL: "https://login.test/x", apiBaseURL: srv.URL, pollInterval: time.Millisecond, timeout: time.Second}
	_, err := c.login(context.Background(), &fakeUI{})
	if err == nil || err.Error() != "cursor login: poll failed (HTTP 500)" {
		t.Fatalf("err = %v", err)
	}
}

func TestRefreshUsesRefreshTokenAndKeepsItWhenNotRotated(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	access := testJWT(t, exp)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]string
		_ = json.NewDecoder(r.Body).Decode(&got)
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" || got["grant_type"] != "refresh_token" || got["refresh_token"] != "old-refresh" || got["client_id"] != refreshClientID {
			t.Errorf("bad request %s %s body=%v", r.Method, r.URL.Path, got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": access})
	}))
	defer srv.Close()
	c := &client{http: srv.Client(), apiBaseURL: srv.URL}
	creds, err := c.refresh(context.Background(), sdk.OAuthCredentials{Access: "old", Refresh: "old-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if creds.Access != access || creds.Refresh != "old-refresh" {
		t.Fatalf("unexpected credentials %+v", creds)
	}
}

func TestRefreshReportsEndedSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"","shouldLogout":true}`))
	}))
	defer srv.Close()
	c := &client{http: srv.Client(), apiBaseURL: srv.URL}
	if _, err := c.refresh(context.Background(), sdk.OAuthCredentials{Refresh: "r"}); err == nil {
		t.Fatal("expected ended-session error")
	}
}

func TestRefreshWithoutRefreshTokenFails(t *testing.T) {
	c := defaultClient()
	if _, err := c.refresh(context.Background(), sdk.OAuthCredentials{Access: "a"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCredentialsRejectNonJWTAccessToken(t *testing.T) {
	if _, err := credentialsFrom(tokenResponse{AccessToken: "opaque"}, ""); err == nil {
		t.Fatal("expected error")
	}
}
