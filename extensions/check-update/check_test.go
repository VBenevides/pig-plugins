package checkupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckUpdatesIsolatesFailureAndSuggestsCompatibleUpdate(t *testing.T) {
	for _, failure := range []string{"pig", "plugins", ""} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/"+failure {
					w.WriteHeader(503)
					return
				}
				if r.URL.Path == "/pig" {
					_, _ = w.Write([]byte(`{"dependencies":{"pig":{"path":"github.com/MichaelKinsy/PiG","version":"0.5.0"}}}`))
				} else {
					_, _ = w.Write([]byte("0.2.0"))
				}
			}))
			defer server.Close()
			var messages []string
			checkUpdates(context.Background(), func(message, _ string) { messages = append(messages, message) }, server.Client(), "0.4.1+1.0.3", server.URL+"/pig", server.URL+"/plugins")
			if len(messages) != 2 {
				t.Fatalf("messages = %q", messages)
			}
			for i, source := range []string{"PiG", "PiG Plugins"} {
				failed := (i == 0 && failure == "pig") || (i == 1 && failure == "plugins")
				if failed {
					if !strings.Contains(messages[i], source+" update check failed: HTTP 503") {
						t.Fatalf("failure = %q", messages[i])
					}
				} else if !strings.Contains(messages[i], "pig-plugins --update") || !strings.Contains(messages[i], source+" ") {
					t.Fatalf("suggestion = %q", messages[i])
				}
			}
		})
	}
}

func TestCheckUpdatesDoesNotSuggestSameOrOlderVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pig" {
			_, _ = w.Write([]byte(`{"dependencies":{"pig":{"path":"github.com/MichaelKinsy/PiG","version":"0.4.1"}}}`))
		} else {
			_, _ = w.Write([]byte("0.0.9"))
		}
	}))
	defer server.Close()
	checkUpdates(context.Background(), func(message, _ string) { t.Errorf("unexpected notice: %s", message) }, server.Client(), "0.4.1+1.0.3", server.URL+"/pig", server.URL+"/plugins")
}

func TestCheckUpdatesUsesCompatibilityInsteadOfNewerUpstream(t *testing.T) {
	for _, tc := range []struct {
		current, plugins     string
		wantPiG, wantPlugins bool
	}{
		{"0.4.0+1.0.3", "0.1.0", true, false},
		{"0.4.1+1.0.3", "0.1.0", false, false},
		{"0.5.0+1.0.3", "0.1.0", false, false},
		{"0.4.1+1.0.3", "0.2.0", false, true},
	} {
		t.Run(tc.current+"/"+tc.plugins, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/compatibility":
					_, _ = w.Write([]byte(`{"dependencies":{"pig":{"path":"github.com/MichaelKinsy/PiG","version":"0.4.1"}}}`))
				case "/plugins":
					_, _ = w.Write([]byte(tc.plugins))
				case "/upstream":
					t.Error("upstream version must not be queried")
					_, _ = w.Write([]byte(`{"version":"9.0.0"}`))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			var messages []string
			checkUpdates(context.Background(), func(message, _ string) { messages = append(messages, message) }, server.Client(), tc.current, server.URL+"/compatibility", server.URL+"/plugins")
			gotPiG, gotPlugins := false, false
			for _, message := range messages {
				if strings.HasPrefix(message, "PiG 0.4.1 is available.") {
					gotPiG = true
				} else if strings.HasPrefix(message, "PiG Plugins 0.2.0 is available.") {
					gotPlugins = true
				} else {
					t.Errorf("unexpected notice: %s", message)
				}
			}
			if gotPiG != tc.wantPiG || gotPlugins != tc.wantPlugins {
				t.Fatalf("notices = %q", messages)
			}
		})
	}
}
