package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"0.1.0", "0.1.0", false}, {"0.2.0", "0.1.9", true},
		{"0.10.0", "0.9.0", true}, {"0.1.0", "0.2.0", false},
		{"v1.0.0", "0.4.1+1.0.3", true}, {"0.4.1", "0.4.1+1.0.3", false},
	} {
		got, err := Newer(tc.latest, tc.current)
		if err != nil || got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, %v", tc.latest, tc.current, got, err)
		}
	}
	for _, value := range []string{"", "latest", "1.0", "1.0.0-rc.1", "999999999999999999999999.0.0", "1.0.0\nmalicious"} {
		if _, err := Newer(value, "0.1.0"); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestLatest(t *testing.T) {
	for _, tc := range []struct {
		body            string
		status          int
		json, wantError bool
	}{
		{"0.2.0\n", 200, false, false}, {`{"version":"0.4.2"}`, 200, true, false},
		{`{}`, 200, true, true}, {"bad", 200, false, true},
		{"error", 503, false, true}, {strings.Repeat("x", 4097), 200, false, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		value, err := Latest(context.Background(), server.Client(), server.URL, tc.json)
		server.Close()
		if (err != nil) != tc.wantError {
			t.Errorf("Latest(%q) = %q, %v", tc.body[:min(40, len(tc.body))], value, err)
		}
	}
}

func TestMissingVersionSourceIdentifiesURL(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	url := server.URL + "/VERSION"
	_, err := Latest(context.Background(), server.Client(), url, false)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") || !strings.Contains(err.Error(), url) {
		t.Fatalf("missing status or source URL: %v", err)
	}
}

func TestLatestCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := Latest(ctx, server.Client(), server.URL, false); err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestPreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent", "check-update.json")
	if enabled, err := Enabled(path); err != nil || !enabled {
		t.Fatalf("default = %v, %v", enabled, err)
	}
	for _, want := range []bool{false, false, true} {
		if err := SaveEnabled(path, want); err != nil {
			t.Fatal(err)
		}
		if got, err := Enabled(path); err != nil || got != want {
			t.Fatalf("preference = %v, %v", got, err)
		}
	}
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Enabled(path); err == nil {
		t.Fatal("invalid preference accepted")
	}
}
