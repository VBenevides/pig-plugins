package cursorlogin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func TestOAuthRejectsRedirectsBeforeSendingSecrets(t *testing.T) {
	var received atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1); w.WriteHeader(401) }))
	defer destination.Close()
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL+"/SECRET", code) }))
			defer origin.Close()
			c := &client{http: origin.Client(), apiBaseURL: origin.URL}
			_, err := c.refresh(context.Background(), sdk.OAuthCredentials{Refresh: "SECRET"})
			if err == nil || !strings.Contains(err.Error(), "redirect") || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if received.Load() != 0 {
		t.Fatalf("redirect sent %d requests to foreign origin", received.Load())
	}
}

type oauthTransport func(*http.Request) (*http.Response, error)

func (f oauthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOAuthRedactsTransportErrorsAndPreservesCause(t *testing.T) {
	cause := errors.New("SECRET verifier and refresh token")
	c := defaultClient()
	c.http = &http.Client{Transport: oauthTransport(func(*http.Request) (*http.Response, error) { return nil, cause })}
	_, _, err := c.pollOnce(context.Background(), apiBaseURL+"/auth/poll?verifier=SECRET")
	if err == nil || strings.Contains(err.Error(), "SECRET") || !errors.Is(err, cause) {
		t.Fatalf("unsafe/lost error: %v", err)
	}
}

func TestOAuthRejectsOversizedResponses(t *testing.T) {
	c := defaultClient()
	c.http = &http.Client{Transport: oauthTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", maxResponseBytes+1)))}, nil
	})}
	_, _, err := c.pollOnce(context.Background(), apiBaseURL+"/auth/poll?verifier=SECRET")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err=%v", err)
	}
}

func TestOAuthRejectsUnexpectedRequestOrigin(t *testing.T) {
	c := defaultClient()
	c.http = &http.Client{Transport: oauthTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected origin received request")
		return nil, nil
	})}
	req, _ := http.NewRequest("GET", "https://attacker.invalid/auth/poll?verifier=SECRET", nil)
	if _, _, err := c.do(req); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err=%v", err)
	}
}

type oauthFailingBody struct{ close bool }

func (b oauthFailingBody) Read([]byte) (int, error) {
	if b.close {
		return 0, io.EOF
	}
	return 0, errors.New("SECRET")
}
func (b oauthFailingBody) Close() error {
	if b.close {
		return errors.New("SECRET")
	}
	return nil
}

func TestOAuthRedactsResponseIOFailures(t *testing.T) {
	for _, closing := range []bool{false, true} {
		c := defaultClient()
		c.http = &http.Client{Transport: oauthTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: oauthFailingBody{close: closing}}, nil
		})}
		req, _ := http.NewRequest("GET", apiBaseURL+"/auth/poll", nil)
		if _, _, err := c.do(req); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("err=%v", err)
		}
	}
}
