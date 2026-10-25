package browseruse

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Live checks use a separately started, trusted driver. They never use a personal
// browser profile and never install or start software themselves.
func TestLiveBrowser(t *testing.T) {
	for _, mode := range []Mode{Chrome, Firefox} {
		t.Run(string(mode), func(t *testing.T) {
			endpoint := os.Getenv("PIG_BROWSER_LIVE_" + strings.ToUpper(string(mode)) + "_DRIVER_URL")
			if endpoint == "" {
				t.Skip("set the matching PIG_BROWSER_LIVE_*_DRIVER_URL to run a real browser check")
			}
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = fmt.Fprint(w, `<!doctype html><title>Browser fixture</title><input aria-label="Name" id="name" oninput="document.querySelector('output').textContent=this.value"><output>empty</output><button onclick="document.querySelector('output').textContent='clicked'">Submit test</button>`)
			}))
			defer page.Close()
			client, err := NewClient(mode, endpoint, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := client.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if _, err := client.Run(ctx, "navigate", map[string]any{"url": page.URL}, approve); err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Run(ctx, "snapshot", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			find := func(snapshot any, label string) string {
				t.Helper()
				for _, element := range snapshot.(map[string]any)["elements"].([]map[string]string) {
					if element["label"] == label {
						return element["ref"]
					}
				}
				t.Fatalf("reference for %s missing: %v", label, snapshot)
				return ""
			}
			if _, err := client.Run(ctx, "fill", map[string]any{"ref": find(snapshot, "Name"), "text": "native input event"}, approve); err != nil {
				t.Fatal(err)
			}
			snapshot, err = client.Run(ctx, "snapshot", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(snapshot.(map[string]any)["text"].(string), "native input event") {
				t.Fatal("controlled input event did not update page")
			}
			if _, err := client.Run(ctx, "click", map[string]any{"ref": find(snapshot, "Submit test")}, approve); err != nil {
				t.Fatal(err)
			}
			snapshot, err = client.Run(ctx, "snapshot", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(snapshot.(map[string]any)["text"].(string), "clicked") {
				t.Fatal("click did not update page")
			}
			value, err := client.Run(ctx, "screenshot", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			image, err := base64.StdEncoding.DecodeString(value.(Screenshot).Data)
			if err != nil {
				t.Fatal(err)
			}
			config, err := png.DecodeConfig(bytes.NewReader(image))
			if err != nil || config.Width < 1 || config.Height < 1 {
				t.Fatalf("invalid real screenshot: %v", err)
			}
			tabs, err := client.Run(ctx, "tabs", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			handles := tabs.([]string)
			if len(handles) == 0 {
				t.Fatal("no browser tabs")
			}
			if _, err := client.Run(ctx, "select_tab", map[string]any{"handle": handles[0]}, approve); err != nil {
				t.Fatal(err)
			}
		})
	}
}
