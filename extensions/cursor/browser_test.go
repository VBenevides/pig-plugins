package cursor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMintKeyFailureIsNotRetriedOrDisclosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"restricted", 403, `{"message":"private-session-token"}`},
		{"uncertain", 500, `private-session-token`},
		{"empty", 200, `{"apiKey":""}`},
		{"malformed", 200, `private-session-token`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			b := &browserAuth{http: server.Client(), apiBaseURL: server.URL}
			got, err := b.mintKey(context.Background(), "private-session-token")
			if err == nil || got.Access != "" || strings.Contains(err.Error(), "private-session-token") || calls.Load() != 1 {
				t.Fatalf("unexpected outcome: error=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestMintKeyUsesSDKExpiry(t *testing.T) {
	b, _, _ := cursorServer(t, 0, "browser-token")
	before := time.Now().Add(userKeyLifetime)
	got, err := b.mintKey(context.Background(), "browser-token")
	if err != nil {
		t.Fatal(err)
	}
	if got.Access != "minted-user-key" || got.Refresh != "" || got.Expires.Before(before) || got.Expires.After(time.Now().Add(userKeyLifetime)) {
		t.Fatal("incorrect API key or expiry")
	}
}
