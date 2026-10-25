package browseruse

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLivePersistentLoginAcrossBrowserRestart(t *testing.T) {
	for _, mode := range []Mode{Chrome, Firefox} {
		t.Run(string(mode), func(t *testing.T) {
			endpoint := os.Getenv("PIG_BROWSER_LIVE_" + strings.ToUpper(string(mode)) + "_DRIVER_URL")
			if endpoint == "" {
				t.Skip("set a live driver URL to test persistent login")
			}
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				switch r.URL.Path {
				case "/login":
					_, _ = fmt.Fprint(w, `<!doctype html><title>Fixture login</title><input type="password" aria-label="Password"><button onclick="location.href='/authenticate'">Test login</button>`)
				case "/authenticate":
					http.SetCookie(w, &http.Cookie{Name: "test_session", Value: "fixture-only", Path: "/", MaxAge: 3600, HttpOnly: true, SameSite: http.SameSiteStrictMode})
					_, _ = fmt.Fprint(w, `<script>localStorage.setItem('test_login','saved');location.replace('/secured')</script>`)
				case "/secured":
					cookie, err := r.Cookie("test_session")
					if err != nil || cookie.Value != "fixture-only" {
						http.Redirect(w, r, "/login", http.StatusFound)
						return
					}
					_, _ = fmt.Fprint(w, `<!doctype html><title>Fixture secure page</title><p>Authorized fixture</p>`)
				}
			}))
			defer site.Close()
			profile := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			for iteration := 0; iteration < 2; iteration++ {
				client, err := NewClient(mode, endpoint, profile)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := client.Close(cleanup); err != nil {
						t.Error(err)
					}
				}()
				if _, err := client.Run(ctx, "navigate", map[string]any{"url": site.URL + "/secured"}, approve); err != nil {
					t.Fatal(err)
				}
				value, err := client.Run(ctx, "snapshot", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				page := value.(map[string]any)
				if mode == Firefox && !client.headless {
					t.Fatal("initial Firefox session was not headless")
				}
				if iteration == 0 {
					if page["loginRequired"] != true {
						t.Fatal("password form did not trigger manual login detection")
					}
					if err := client.SetVisible(ctx, true); err != nil {
						t.Fatal(err)
					}
					if client.headless {
						t.Fatal("login browser remained headless")
					}
					value, err = client.Run(ctx, "snapshot", nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					page = value.(map[string]any)
					ref := ""
					for _, element := range page["elements"].([]map[string]string) {
						if element["label"] == "Test login" {
							ref = element["ref"]
						}
					}
					if ref == "" {
						t.Fatal("fixture login button missing")
					}
					if _, err := client.Run(ctx, "click", map[string]any{"ref": ref}, approve); err != nil {
						t.Fatal(err)
					}
					// The fixture authentication endpoint performs a client-side redirect.
					for attempts := 0; attempts < 20; attempts++ {
						value, err = client.Run(ctx, "snapshot", nil, nil)
						if err != nil {
							t.Fatal(err)
						}
						page = value.(map[string]any)
						if strings.Contains(page["text"].(string), "Authorized fixture") {
							break
						}
						time.Sleep(50 * time.Millisecond)
					}
					if err := client.SetVisible(ctx, false); err != nil {
						t.Fatal(err)
					}
					if mode == Firefox && !client.headless {
						t.Fatal("Firefox did not return to headless mode")
					}
					value, err = client.Run(ctx, "snapshot", nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					page = value.(map[string]any)
				}
				if page["loginRequired"] != false || !strings.Contains(page["text"].(string), "Authorized fixture") {
					t.Fatalf("iteration %d: persistent authentication failed", iteration)
				}
				var storage string
				if err := client.request(ctx, "POST", "/session/"+client.session+"/execute/sync", map[string]any{"script": "return localStorage.getItem('test_login')", "args": []any{}}, &storage); err != nil || storage != "saved" {
					t.Fatalf("iteration %d: browser storage did not persist: %v", iteration, err)
				}
				if err := client.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
