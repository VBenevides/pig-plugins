package browseruse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileLocationPermissionsAndReuse(t *testing.T) {
	home := t.TempDir()
	get := func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}
	if ProfileDir(get, Chrome) != filepath.Join(home, ".pig", "agent", "browser-use", "chrome") {
		t.Fatal("incorrect default profile path")
	}
	if ProfileDir(get, Chrome) == ProfileDir(get, Firefox) {
		t.Fatal("browsers share a profile")
	}
	for _, mode := range []Mode{Chrome, Firefox} {
		t.Run(string(mode), func(t *testing.T) {
			profile := filepath.Join(t.TempDir(), string(mode))
			f := &driverFixture{}
			s := httptest.NewServer(http.HandlerFunc(f.serve))
			defer s.Close()
			for iteration := 0; iteration < 2; iteration++ {
				c, err := NewClient(mode, s.URL, profile)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.Run(context.Background(), "tabs", nil, nil); err != nil {
					t.Fatal(err)
				}
				caps := f.caps["capabilities"].(map[string]any)["alwaysMatch"].(map[string]any)
				key := "goog:chromeOptions"
				if mode == Firefox {
					key = "moz:firefoxOptions"
				}
				options := caps[key].(map[string]any)
				raw, _ := json.Marshal(options["args"])
				if !strings.Contains(string(raw), profile) || !strings.Contains(string(raw), "headless") {
					t.Fatalf("profile or default headless mode missing: %s", raw)
				}
				if _, cloned := options["profile"]; cloned {
					t.Fatal("Firefox profile was copied rather than used in-place")
				}
				marker := filepath.Join(profile, "site-session-marker")
				if iteration == 0 {
					if err := os.WriteFile(marker, []byte("preserved"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := c.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "preserved" {
					t.Fatal("closing deleted persistent profile data")
				}
				info, err := os.Stat(profile)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0700 {
					t.Fatalf("profile permissions %o", info.Mode().Perm())
				}
			}
		})
	}
}

func TestProfileRejectsSymlinkAndMissingDirectoryArgument(t *testing.T) {
	if _, err := NewClient(Chrome, "http://127.0.0.1:9515", ""); err == nil {
		t.Fatal("empty persistent profile admitted")
	}
	if err := prepareProfile("relative-profile"); err == nil {
		t.Fatal("relative profile admitted")
	}
	root := t.TempDir()
	link := filepath.Join(root, "profile")
	target := t.TempDir()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := prepareProfile(link); err == nil {
		t.Fatal("symlink profile admitted")
	}
}

func TestHeadlessFirefoxFailsBeforePageAccess(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"value":{"sessionId":"headed-required","capabilities":{"moz:headless":true}}}`))
	}))
	defer server.Close()
	c, _ := NewClient(Firefox, server.URL, t.TempDir())
	if err := c.SetVisible(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.Run(context.Background(), "tabs", nil, nil); err == nil || !strings.Contains(err.Error(), "headless") {
			t.Fatalf("headless browser admitted: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("headless browser accessed page or started again: %d", calls)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordFillRequiresManualLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("password reached the driver") }))
	defer server.Close()
	c, _ := NewClient(Firefox, server.URL, t.TempDir())
	c.refs = map[string]element{"password": {ID: "pw", Type: "password"}}
	_, err := c.Run(context.Background(), "fill", map[string]any{"ref": "password", "text": "must-not-reach-driver"}, approve)
	if err == nil || !strings.Contains(err.Error(), "browser_login") || strings.Contains(err.Error(), "must-not") {
		t.Fatalf("password handling: %v", err)
	}
}

func TestHeadlessChromeSnapshotCannotStartManualLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session" {
			_, _ = w.Write([]byte(`{"value":{"sessionId":"chrome-headless"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":{"headless":true,"loginRequired":true,"elements":[]}}`))
	}))
	defer server.Close()
	c, _ := NewClient(Chrome, server.URL, t.TempDir())
	if err := c.SetVisible(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run(context.Background(), "snapshot", nil, nil); err == nil || !strings.Contains(err.Error(), "visible browser") {
		t.Fatalf("headless Chrome accepted: %v", err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
