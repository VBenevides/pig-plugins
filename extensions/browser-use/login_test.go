package browseruseext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestAutomaticManualLoginHandoff(t *testing.T) {
	pigtest.RequirePig(t)
	for _, visibility := range []string{"always", "login"} {
		for _, scenario := range []string{"logged-in", "other", "cancel", "still-login", "explicit"} {
			t.Run(visibility+"/"+scenario, func(t *testing.T) {
				var loginRequired atomic.Bool
				loginRequired.Store(true)
				var snapshots atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var input map[string]any
					if r.Method == "POST" {
						if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
							http.Error(w, "fixture request", 400)
							return
						}
					}
					var value any
					switch r.URL.Path {
					case "/session":
						value = map[string]any{"sessionId": "login-session"}
					case "/session/login-session/url":
						if r.Method == "GET" {
							value = "https://example.org/login"
						}
					case "/session/login-session/execute/sync":
						snapshots.Add(1)
						value = map[string]any{"title": "Sign in", "text": "Login form", "loginRequired": loginRequired.Load(), "elements": []any{}}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
				}))
				defer server.Close()
				name := "browser_navigate"
				args := map[string]any{"url": "https://example.org/login"}
				if scenario == "explicit" {
					name = "browser_login"
					args = map[string]any{}
					loginRequired.Store(false)
				}
				home := pigtest.NewHome(t)
				mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call(name, args)), pigtest.Text("done"))
				defer mock.Close()
				selections := 0
				result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensionPaths(t), Env: map[string]string{"PIG_BROWSER_CHROME_DRIVER_URL": server.URL}, Confirm: func(map[string]any) bool { return true },
					Dialog: func(request map[string]any) map[string]any {
						selections++
						options, _ := request["options"].([]any)
						if len(options) != 2 || options[0] != "Logged in" || options[1] != "Other" {
							t.Errorf("login options: %v", options)
						}
						if scenario == "cancel" {
							return map[string]any{"cancelled": true}
						}
						if scenario == "other" {
							return map[string]any{"value": "Other"}
						}
						if scenario != "still-login" {
							loginRequired.Store(false)
						}
						return map[string]any{"value": "Logged in"}
					}, Prompts: []string{"/browser-use visibility " + visibility, "/browser-use mode chrome", "use the secured site", "/browser-use mode off"}})
				if selections != 1 {
					t.Fatalf("login prompts %d; notices %v stderr %s", selections, result.Notices(), result.Stderr)
				}
				requests := mock.Requests()
				if len(requests) != 2 {
					t.Fatalf("unexpected model requests: %d", len(requests))
				}
				raw, _ := json.Marshal(requests[1]["messages"])
				confirmed := scenario == "logged-in" || scenario == "explicit"
				if strings.Contains(string(raw), "confirmed_by_user") != confirmed {
					t.Fatalf("incorrect login outcome: %s", raw)
				}
				if !confirmed && !strings.Contains(string(raw), "do not continue") && !strings.Contains(string(raw), "not confirmed") && !strings.Contains(string(raw), "not verified") {
					t.Fatalf("missing login refusal: %s", raw)
				}
				if scenario == "other" || scenario == "cancel" {
					expectedSnapshots := int32(1)
					if visibility == "login" {
						expectedSnapshots = 2
					}
					if snapshots.Load() != expectedSnapshots {
						t.Fatal("read page after unconfirmed login")
					}
				}
			})
		}
	}
}

func TestLoginToolIsHiddenWhileOff(t *testing.T) {
	if !slices.Contains(toolNames(), "browser_login") {
		t.Fatal("login tool missing from mode visibility controls")
	}
	if _, err := manualLogin(context.Background(), noLoginUI{}, nil); err == nil {
		t.Fatal("manual login admitted without a UI")
	}
}

type noLoginUI struct{}

func (noLoginUI) HasUI() bool { return false }
func (noLoginUI) SelectWithOptions(string, []string, sdk.DialogOptions) (string, bool, error) {
	panic("headless login must not open a dialog")
}
