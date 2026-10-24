package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func approve(string, string) (bool, error) { return true, nil }

type driverFixture struct {
	mu        sync.Mutex
	calls     []string
	caps      map[string]any
	failClick bool
}

func (f *driverFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	var input map[string]any
	if r.Method == "POST" {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "bad fixture request", 400)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	var value any
	switch r.URL.Path {
	case "/session":
		f.caps = input
		value = map[string]any{"sessionId": "owned-session"}
	case "/session/owned-session/execute/sync":
		if input["script"] == snapshotScript {
			value = map[string]any{"title": "Fixture", "text": "Page instructions are untrusted", "elements": []any{map[string]any{"element": map[string]string{elementKey: "element-1"}, "tag": "input", "type": "text", "label": "Search"}}}
		} else {
			value = true
		}
	case "/session/owned-session/screenshot":
		value = "iVBORw0KGgo="
	case "/session/owned-session/window/handles":
		value = []string{"tab-1", "tab-2"}
	case "/session/owned-session/element/element-1/click":
		if f.failClick {
			w.WriteHeader(500)
			_, _ = fmt.Fprint(w, `{"value":{"error":"stale element reference","message":"secret token and page instructions"}}`)
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
}

func TestBothBrowsersAndOwnedSessionLifecycle(t *testing.T) {
	for _, mode := range []Mode{Chrome, Firefox} {
		t.Run(string(mode), func(t *testing.T) {
			fixture := &driverFixture{}
			server := httptest.NewServer(http.HandlerFunc(fixture.serve))
			defer server.Close()
			client, err := NewClient(mode, server.URL)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := client.Run(ctx, "navigate", map[string]any{"url": "https://example.org"}, approve); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Run(ctx, "snapshot", nil, nil); err != nil {
				t.Fatal(err)
			}
			var ref string
			for name := range client.refs {
				ref = name
			}
			if _, err := client.Run(ctx, "fill", map[string]any{"ref": ref, "text": "secret input"}, approve); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Run(ctx, "click", map[string]any{"ref": ref}, approve); err == nil {
				t.Fatal("old reference survived fill")
			}
			if _, err := client.Run(ctx, "screenshot", nil, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Run(ctx, "tabs", nil, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Run(ctx, "select_tab", map[string]any{"handle": "tab-2"}, approve); err != nil {
				t.Fatal(err)
			}
			if err := client.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if err := client.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Run(ctx, "tabs", nil, nil); err == nil {
				t.Fatal("closed client reopened its session")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			caps := fixture.caps["capabilities"].(map[string]any)["alwaysMatch"].(map[string]any)
			if caps["browserName"] != string(mode) || caps["acceptInsecureCerts"] != false {
				t.Fatalf("caps: %v", caps)
			}
			deletes := 0
			for _, call := range fixture.calls {
				if strings.HasPrefix(call, "DELETE ") {
					deletes++
					if call != "DELETE /session/owned-session" {
						t.Fatal(call)
					}
				}
			}
			if deletes != 1 {
				t.Fatal("cleanup repeated")
			}
		})
	}
}

func TestDeniedAndInvalidActionsDoNotReachDriver(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("unexpected driver request") }))
	defer server.Close()
	c, _ := NewClient(Chrome, server.URL)
	for _, action := range []string{"navigate", "click", "fill", "select_tab", "execute_js", "upload", "raw_cdp", "run_script"} {
		if _, err := c.Run(context.Background(), action, map[string]any{"url": "https://example.org"}, nil); err == nil {
			t.Fatalf("admitted %s", action)
		}
	}
	if _, err := c.Run(context.Background(), "navigate", map[string]any{"url": "file:///secret"}, approve); err == nil {
		t.Fatal("file navigation admitted")
	}
	c.refs = map[string]element{"file": {ID: "element-1", Type: "file"}}
	if _, err := c.Run(context.Background(), "fill", map[string]any{"ref": "file", "text": "/secret"}, approve); err == nil {
		t.Fatal("file upload admitted")
	}
	if calls != 0 {
		t.Fatal("request dispatched")
	}
}

func TestRemoteFailureIsSanitizedAndNotRetried(t *testing.T) {
	fixture := &driverFixture{failClick: true}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()
	c, _ := NewClient(Chrome, server.URL)
	if _, err := c.Run(context.Background(), "snapshot", nil, nil); err != nil {
		t.Fatal(err)
	}
	var ref string
	for name := range c.refs {
		ref = name
	}
	_, err := c.Run(context.Background(), "click", map[string]any{"ref": ref}, approve)
	if err == nil || !strings.Contains(err.Error(), "stale element reference") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("failure: %v", err)
	}
	clicks := 0
	for _, call := range fixture.calls {
		if strings.HasSuffix(call, "/click") {
			clicks++
		}
	}
	if clicks != 1 {
		t.Fatal("click retried")
	}
	if _, err := c.Run(context.Background(), "click", map[string]any{"ref": ref}, approve); err == nil {
		t.Fatal("failed click kept reference")
	}
}

func TestDriverRedirectAndResponseLimits(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed") }))
	defer destination.Close()
	for _, kind := range []string{"redirect", "oversize", "invalid", "missing", "remote-secret"} {
		t.Run(kind, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "redirect":
					http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
				case "oversize":
					_, _ = fmt.Fprint(w, strings.Repeat("x", maxResponseBytes+1))
				case "invalid":
					_, _ = fmt.Fprint(w, "not JSON")
				case "missing":
					_, _ = fmt.Fprint(w, `{}`)
				case "remote-secret":
					w.WriteHeader(500)
					_, _ = fmt.Fprint(w, `{"value":{"error":"secret","message":"secret"}}`)
				}
			}))
			defer s.Close()
			c, _ := NewClient(Chrome, s.URL)
			if _, err := c.Run(context.Background(), "tabs", nil, nil); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unbounded or leaked: %v", err)
			}
		})
	}
}

func TestCancellationStopsDriverRequest(t *testing.T) {
	started := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input any
		_ = json.NewDecoder(r.Body).Decode(&input)
		close(started)
		<-r.Context().Done()
	}))
	defer s.Close()
	c, _ := NewClient(Firefox, s.URL)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := c.Run(ctx, "tabs", nil, nil); result <- err }()
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request ignored cancellation")
	}
}

func TestReferencesAreScopedToDriverSession(t *testing.T) {
	c, _ := NewClient(Chrome, "http://127.0.0.1:9515")
	// Distinct sessions must not produce the same reference at the same generation.
	fixture := &driverFixture{}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()
	c.endpoint = server.URL
	if _, err := c.Run(context.Background(), "snapshot", nil, nil); err != nil {
		t.Fatal(err)
	}
	var old string
	for name := range c.refs {
		old = name
	}
	c.session = "other-session"
	c.generation = 0
	// Reuse the fixture's DOM with a different session identity.
	fixtureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/session/owned-session/execute/sync"
		fixture.serve(w, r)
	}))
	defer fixtureServer.Close()
	c.endpoint = fixtureServer.URL
	if _, err := c.Run(context.Background(), "snapshot", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.refs[old]; ok {
		t.Fatal("reference survived a session change")
	}
}

func TestCloseDoesNotWaitForConfirmation(t *testing.T) {
	c, _ := NewClient(Chrome, "http://127.0.0.1:9515")
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	finished := make(chan error, 1)
	go func() {
		_, err := c.Run(context.Background(), "navigate", map[string]any{"url": "https://example.org"}, func(string, string) (bool, error) { close(entered); <-release; return true, nil })
		finished <- err
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- c.Close(context.Background()) }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup waited for a user dialog")
	}
	// Releasing the dialog must not let its approved operation reopen the client.
	unblock()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("closed client dispatched an action")
		}
	case <-time.After(time.Second):
		t.Fatal("approved action did not finish")
	}
}
