package browseruse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseVisibility(t *testing.T) {
	for _, visibility := range []Visibility{Hidden, Login, Always} {
		if got, err := ParseVisibility("visibility " + string(visibility)); err != nil || got != visibility {
			t.Fatalf("parse %s: %s %v", visibility, got, err)
		}
	}
	for _, args := range []string{"", "hidden", "visibility", "visibility sometimes", "visibility hidden extra", "mode hidden"} {
		if _, err := ParseVisibility(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}

func TestVisibilityRestartsKeepProfileAndInvalidateReferences(t *testing.T) {
	for _, mode := range []Mode{Chrome, Firefox} {
		t.Run(string(mode), func(t *testing.T) {
			profile := t.TempDir()
			var starts []bool
			active := false
			deletes := 0
			restores := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var value any
				switch {
				case r.Method == "POST" && r.URL.Path == "/session":
					if active {
						t.Error("replacement started before previous browser closed")
					}
					active = true
					var input map[string]any
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					caps := input["capabilities"].(map[string]any)["alwaysMatch"].(map[string]any)
					key := "goog:chromeOptions"
					if mode == Firefox {
						key = "moz:firefoxOptions"
					}
					raw, _ := json.Marshal(caps[key])
					if !strings.Contains(string(raw), profile) {
						t.Error("profile changed")
					}
					starts = append(starts, strings.Contains(string(raw), "headless"))
					value = map[string]any{"sessionId": fmt.Sprintf("session-%d", len(starts))}
				case r.Method == "DELETE":
					active = false
					deletes++
				case strings.HasSuffix(r.URL.Path, "/url"):
					if r.Method == "GET" {
						value = "https://example.org/secured"
					} else {
						restores++
					}
				case strings.HasSuffix(r.URL.Path, "/execute/sync"):
					value = map[string]any{"elements": []any{map[string]any{"element": map[string]string{elementKey: "button"}, "type": "button", "tag": "button", "label": "safe"}}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
			}))
			defer server.Close()
			c, err := NewClient(mode, server.URL, profile)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := c.Close(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			page, err := c.Run(context.Background(), "snapshot", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			oldRef := page.(map[string]any)["elements"].([]map[string]string)[0]["ref"]
			for _, visible := range []bool{true, true, false, false} {
				if err := c.SetVisible(context.Background(), visible); err != nil {
					t.Fatal(err)
				}
			}
			if fmt.Sprint(starts) != "[true false true]" || deletes != 2 || restores != 2 {
				t.Fatalf("starts %v deletes %d restores %d", starts, deletes, restores)
			}
			if _, err := c.Run(context.Background(), "click", map[string]any{"ref": oldRef}, approve); err == nil {
				t.Fatal("old reference survived restart")
			}
		})
	}
}

func TestVisibilityShutdownFailureDoesNotStartReplacement(t *testing.T) {
	starts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := any(nil)
		switch {
		case r.URL.Path == "/session":
			starts++
			value = map[string]any{"sessionId": "owned"}
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/url"):
			value = "https://example.org/"
		case r.Method == "DELETE":
			w.WriteHeader(500)
			value = map[string]any{"error": "timeout"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
	}))
	defer server.Close()
	c, _ := NewClient(Firefox, server.URL, t.TempDir())
	if _, err := c.Run(context.Background(), "tabs", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SetVisible(context.Background(), true); err == nil {
		t.Fatal("shutdown failure hidden")
	}
	if _, err := c.Run(context.Background(), "snapshot", nil, nil); err == nil {
		t.Fatal("used uncertain profile ownership")
	}
	if starts != 1 {
		t.Fatal("started a browser after failed shutdown")
	}
}
