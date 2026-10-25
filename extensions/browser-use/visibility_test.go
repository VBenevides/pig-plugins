package browseruseext

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/browseruse"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestLoginVisibilityReturnsToHeadless(t *testing.T) {
	for _, scenario := range []string{"logged-in", "other", "cancel", "still-login", "lost-session", "dialog-error"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			var starts []bool
			authenticated := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var value any
				switch {
				case r.Method == "POST" && r.URL.Path == "/session":
					var input map[string]any
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					raw, _ := json.Marshal(input)
					headless := strings.Contains(string(raw), "headless")
					starts = append(starts, headless)
					if scenario == "lost-session" && len(starts) == 3 {
						authenticated = false
					}
					value = map[string]any{"sessionId": fmt.Sprintf("session-%d", len(starts))}
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/url"):
					value = "https://example.org/secured"
				case strings.HasSuffix(r.URL.Path, "/execute/sync"):
					value = map[string]any{"loginRequired": !authenticated, "elements": []any{}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
			}))
			defer server.Close()
			client, err := browseruse.NewClient(browseruse.Firefox, server.URL, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := client.Close(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			if _, err := client.Run(context.Background(), "snapshot", nil, nil); err != nil {
				t.Fatal(err)
			}
			ui := loginTestUI{selectFn: func() (string, bool, error) {
				mu.Lock()
				defer mu.Unlock()
				if fmt.Sprint(starts) != "[true false]" {
					t.Errorf("login browser was not made visible: %v", starts)
				}
				switch scenario {
				case "other":
					return "Other", true, nil
				case "cancel":
					return "", false, nil
				case "dialog-error":
					return "", false, fmt.Errorf("fixture dialog failure")
				case "still-login":
					return "Logged in", true, nil
				default:
					authenticated = true
					return "Logged in", true, nil
				}
			}}
			value, err := loginWithVisibility(context.Background(), ui, client, browseruse.Login)
			if (err == nil) != (scenario == "logged-in") {
				t.Fatalf("%s: %v %v", scenario, value, err)
			}
			mu.Lock()
			defer mu.Unlock()
			wantStarts := "[true false]"
			if scenario == "logged-in" || scenario == "lost-session" {
				wantStarts = "[true false true]"
			}
			if fmt.Sprint(starts) != wantStarts {
				t.Fatalf("did not return to headless: %v", starts)
			}
			if scenario == "lost-session" && !strings.Contains(err.Error(), "did not survive") {
				t.Fatal(err)
			}
		})
	}
}

func TestHiddenLoginHasNoBrowserOrDialogSideEffects(t *testing.T) {
	if _, err := loginWithVisibility(context.Background(), nil, nil, browseruse.Hidden); err == nil || !strings.Contains(err.Error(), "another authorized method") {
		t.Fatalf("hidden login: %v", err)
	}
}

func TestVisibilityCommandAndSessionReset(t *testing.T) {
	pigtest.RequirePig(t)
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprint(reset), func(t *testing.T) {
			var mu sync.Mutex
			starts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				var value any
				if r.URL.Path == "/session" {
					var input map[string]any
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					raw, _ := json.Marshal(input)
					if !strings.Contains(string(raw), "headless") {
						t.Error("hidden browser requested visible capabilities")
					}
					starts++
					value = map[string]any{"sessionId": "hidden"}
				} else if strings.HasSuffix(r.URL.Path, "/execute/sync") {
					value = map[string]any{"loginRequired": true, "elements": []any{}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
			}))
			defer server.Close()
			home := pigtest.NewHome(t)
			mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("browser_navigate", map[string]any{"url": "https://example.org/login"})), pigtest.Text("done"))
			defer mock.Close()
			prompts := []string{"/browser-use visibility hidden", "/browser-use visibility invalid", "/browser-use mode chrome", "open secured site", "/browser-use mode off"}
			if reset {
				prompts = append([]string{"/browser-use visibility always", "/browser-test-new"}, prompts[1:]...)
			}
			result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: append(extensionPaths(t), probePath(t)), Env: map[string]string{"PIG_BROWSER_CHROME_DRIVER_URL": server.URL}, Confirm: func(map[string]any) bool { return true }, Dialog: func(map[string]any) map[string]any {
				t.Error("hidden login opened a dialog")
				return map[string]any{"cancelled": true}
			}, Prompts: prompts})
			requests := mock.Requests()
			if len(requests) != 2 {
				t.Fatalf("requests %d stderr %s", len(requests), result.Stderr)
			}
			raw, _ := json.Marshal(requests[1]["messages"])
			if !strings.Contains(string(raw), "another authorized method") {
				t.Fatalf("missing hidden-login guidance: %s", raw)
			}
			mu.Lock()
			defer mu.Unlock()
			if starts != 1 {
				t.Fatalf("browser restarted for hidden login: %d", starts)
			}
		})
	}
}
