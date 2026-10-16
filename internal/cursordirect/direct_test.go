package cursordirect

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func fakeClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewUnstartedServer(handler)
	s.EnableHTTP2 = true
	s.StartTLS()
	t.Cleanup(s.Close)
	return &Client{http: s.Client(), base: s.URL, slots: make(chan struct{}, 4)}
}
func mustParse(t *testing.T, b []byte) message {
	t.Helper()
	m, err := parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func model() map[string]any { return map[string]any{"id": "test-model", "name": "Test"} }
func prompt() map[string]any {
	return map[string]any{"systemPrompt": "Be careful.", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
}
func toolsPrompt() map[string]any {
	r := prompt()
	r["tools"] = []any{map[string]any{"name": "read", "description": "Read through approvals", "parameters": map[string]any{"type": "object"}}}
	return r
}
func update(n int, b []byte) []byte  { return bytesField(1, bytesField(n, b)) }
func terminal(payload string) []byte { b := frame([]byte(payload)); b[0] = 2; return b }
func writeFrames(w http.ResponseWriter, frames ...[]byte) {
	w.Header().Set("Content-Type", "application/connect+proto")
	w.WriteHeader(200)
	w.(http.Flusher).Flush()
	for _, b := range frames {
		for len(b) > 0 {
			n := 3
			if len(b) < n {
				n = len(b)
			}
			_, _ = w.Write(b[:n])
			b = b[n:]
		}
		w.(http.Flusher).Flush()
	}
}
func result(t *testing.T, c *Client, ctx context.Context, r map[string]any) (map[string]any, []map[string]any) {
	t.Helper()
	stream, err := c.Stream(model(), r, sdk.ProviderStreamOptions{Signal: ctx, Values: map[string]any{"apiKey": "synthetic-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	for e := range stream.Events(ctx) {
		events = append(events, e)
	}
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	default:
	}
	return stream.Result(), events
}
func TestCatalogAccountExactIDsAndMetadata(t *testing.T) {
	c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer account-a" && r.Header.Get("Authorization") != "Bearer account-b" {
			t.Error("wrong auth")
		}
		if r.Header.Get("Cookie") != "" {
			t.Error("unexpected dashboard cookie")
		}
		if r.Header.Get("X-Cursor-Client-Version") != clientVersion {
			t.Error("missing version")
		}
		if r.URL.Path == usablePath {
			id := "slug-a"
			if strings.HasSuffix(r.Header.Get("Authorization"), "account-b") {
				id = "slug-b"
			}
			_, _ = w.Write(join(bytesField(1, join(str(1, id), str(4, "Account model"), bytesField(2, nil))), bytesField(1, str(1, id))))
			return
		}
		_, _ = w.Write(bytesField(2, join(str(1, "base"), str(36, "slug-a"), integer(15, 123456), integer(9, 1))))
	})
	a, err := c.Catalog(context.Background(), "account-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || a[0]["id"] != "slug-a" || a[0]["contextWindow"] != uint64(123456) || a[0]["cursorContextEstimated"] != false {
		t.Fatalf("catalog %#v", a)
	}
	b, err := c.Catalog(context.Background(), "account-b")
	if err != nil || len(b) != 1 || b[0]["id"] != "slug-b" {
		t.Fatalf("account isolation: %#v %v", b, err)
	}
}
func TestCatalogEmptyMalformedDeniedAndOptionalMetadata(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       []byte
		richStatus int
		wantErr    bool
		count      int
	}{{"empty", 200, nil, 200, false, 0}, {"malformed", 200, []byte{10, 50}, 200, true, 0}, {"denied", 403, []byte("synthetic-secret"), 200, true, 0}, {"metadata unavailable", 200, bytesField(1, str(1, "slug")), 500, false, 1}, {"invalid id", 200, bytesField(1, nil), 200, true, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == availablePath {
					w.WriteHeader(tc.richStatus)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.body)
			})
			models, err := c.Catalog(context.Background(), "synthetic-secret")
			if (err != nil) != tc.wantErr || len(models) != tc.count {
				t.Fatalf("%#v %v", models, err)
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("secret leaked")
			}
		})
	}
}
func TestStreamFragmentedTextThinkingUsageAndSnapshots(t *testing.T) {
	c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _, err := readFrame(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		writeFrames(w, frame(update(4, str(1, "thought"))), frame(update(1, str(1, "hel"))), frame(update(1, str(1, "lo"))), frame(update(14, join(integer(1, 12), integer(2, 3)))), terminal("{}"))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, events := result(t, c, ctx, prompt())
	if m["stopReason"] != "stop" {
		t.Fatal(m)
	}
	parts := objects(m["content"])
	if len(parts) != 2 || parts[0]["thinking"] != "thought" || parts[1]["text"] != "hello" {
		t.Fatal(parts)
	}
	if m["usage"].(map[string]any)["input"] != uint64(12) {
		t.Fatal(m)
	}
	var deltas []string
	for _, e := range events {
		if e["type"] == "text_delta" {
			deltas = append(deltas, text(e["delta"]))
		}
	}
	if strings.Join(deltas, "") != "hello" {
		t.Fatal(events)
	}
	if len(array(events[0]["partial"].(map[string]any)["content"])) != 0 {
		t.Fatal("mutated earlier snapshot")
	}
}
func TestStreamToolHandshakeDuplicateAndResultContinuation(t *testing.T) {
	var requests atomic.Int32
	c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, b, err := readFrame(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		run := mustParse(t, mustParse(t, b).data(1))
		state := mustParse(t, run.data(1))
		if requests.Add(1) == 2 {
			if !mustParse(t, run.data(2)).has(2) {
				t.Error("expected resume action")
			}
			// Ask the client for every root-history blob and inspect the actual tool result.
			found := false
			idx := uint64(0)
			w.Header().Set("Content-Type", "application/connect+proto")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			for _, f := range state {
				if f.number != 1 {
					continue
				}
				idx++
				_, _ = w.Write(frame(bytesField(4, join(integer(1, idx), bytesField(2, bytesField(1, f.data))))))
				w.(http.Flusher).Flush()
				_, reply, err := readFrame(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				kv := mustParse(t, mustParse(t, reply).data(3))
				data := mustParse(t, kv.data(2)).data(1)
				if bytes.Contains(data, []byte("approved file content")) {
					found = true
				}
			}
			if !found {
				t.Error("tool result absent from replay")
			}
			_, _ = w.Write(frame(update(1, str(1, "done"))))
			_, _ = w.Write(frame(update(14, nil)))
			_, _ = w.Write(terminal("{}"))
			return
		}
		w.Header().Set("Content-Type", "application/connect+proto")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		_, _ = w.Write(frame(bytesField(2, join(integer(1, 1), bytesField(10, nil)))))
		w.(http.Flusher).Flush()
		_, reply, err := readFrame(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		exec := mustParse(t, mustParse(t, reply).data(2))
		contextResult := mustParse(t, exec.data(10))
		contextSuccess := mustParse(t, contextResult.data(1))
		rc := mustParse(t, contextSuccess.data(1))
		if !rc.has(2) || !rc.has(7) {
			t.Error("rules/tools missing from handshake")
		}
		args, _ := mcpArgs("call-1", "read", map[string]any{"path": "file.txt"})
		for i := 0; i < 2; i++ {
			_, _ = w.Write(frame(bytesField(2, join(integer(1, uint64(i+2)), bytesField(11, args)))))
			w.(http.Flusher).Flush()
			_, reply, err = readFrame(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Contains(reply, []byte(handoff)) {
				t.Error("missing external-executor handoff")
			}
		}
		_, _ = w.Write(frame(update(14, nil)))
		_, _ = w.Write(terminal("{}"))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := toolsPrompt()
	m, _ := result(t, c, ctx, r)
	if m["stopReason"] != "toolUse" || len(objects(m["content"])) != 1 {
		t.Fatal(m)
	}
	r["messages"] = append(array(r["messages"]), m, map[string]any{"role": "toolResult", "toolCallId": "call-1", "toolName": "read", "content": []any{map[string]any{"type": "text", "text": "approved file content"}}})
	final, _ := result(t, c, ctx, r)
	if final["stopReason"] != "stop" || objects(final["content"])[0]["text"] != "done" {
		t.Fatal(final)
	}
}
func TestStreamFailuresNeverBecomeSuccessOrRetry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
	}{{"truncated", frame(update(1, str(1, "partial")))}, {"terminal error", join(frame(update(14, nil)), terminal(`{"error":{"code":"permission_denied","message":"synthetic-secret"}}`))}, {"no turn end", terminal("{}")}, {"unknown exec", frame(bytesField(2, join(integer(1, 1), bytesField(2, str(1, "touch unauthorized")))))}, {"unknown tool", frame(bytesField(2, join(integer(1, 1), bytesField(11, join(str(3, "id"), str(5, "not-advertised"))))))}, {"malformed", frame([]byte{10, 255})}, {"compressed", []byte{1, 0, 0, 0, 0}}, {"bad terminal", terminal("nonsense")}} {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				_, _, _ = readFrame(r.Body)
				writeFrames(w, tc.payload)
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			m, _ := result(t, c, ctx, toolsPrompt())
			if m["stopReason"] != "error" || count.Load() != 1 {
				t.Fatalf("%#v requests=%d", m, count.Load())
			}
			if strings.Contains(text(m["errorMessage"]), "synthetic-secret") {
				t.Fatal("leaked secret")
			}
		})
	}
}
func TestCancellationClosesDuplexRequest(t *testing.T) {
	started := make(chan struct{})
	closed := make(chan struct{})
	c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _, _ = readFrame(r.Body)
		writeFrames(w)
		close(started)
		<-r.Context().Done()
		close(closed)
	})
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := c.Stream(model(), prompt(), sdk.ProviderStreamOptions{Signal: ctx, Values: map[string]any{"apiKey": "synthetic-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	cancel()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("request not closed")
	}
	done := make(chan struct{})
	go func() {
		if stream.Result()["stopReason"] != "aborted" {
			t.Error("not aborted")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream not ended")
	}
}
func TestRedirectAndHTTPFailureRedaction(t *testing.T) {
	var leaked atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer foreign.Close()
	c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/synthetic-secret", 307)
	})
	_, err := c.Catalog(context.Background(), "synthetic-secret")
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") || leaked.Load() != 0 {
		t.Fatalf("%v leaked=%d", err, leaked.Load())
	}
}
func TestWireBoundsAndHistoryValidation(t *testing.T) {
	head := make([]byte, 5)
	binary.BigEndian.PutUint32(head[1:], maxFrame+1)
	if _, _, err := readFrame(bytes.NewReader(head)); err == nil {
		t.Fatal("oversized accepted")
	}
	for _, b := range [][]byte{{0}, {8, 255}, {10, 255}, {15}, {9, 0}} {
		if _, err := parse(b); err == nil {
			t.Fatalf("malformed accepted %x", b)
		}
	}
	for _, change := range []func(map[string]any){func(r map[string]any) {
		r["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image", "data": "secret"}}}}
	}, func(r map[string]any) {
		r["messages"] = []any{map[string]any{"role": "toolResult", "toolCallId": "orphan", "content": "result"}}
	}, func(r map[string]any) { r["systemPrompt"] = make(chan int) }} {
		r := prompt()
		change(r)
		if _, _, err := buildHistory(model(), r); err == nil {
			t.Fatal("invalid history accepted")
		}
	}
}
func FuzzParseWire(f *testing.F) {
	for _, b := range [][]byte{nil, {10, 0}, {8, 1}, {255}} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxFrame {
			return
		}
		_, _ = parse(b)
	})
}

// Keep the imported JSON/io helpers exercised against wire fixtures as well.
func TestRootHistoryBlobShape(t *testing.T) {
	h, run, err := buildHistory(model(), prompt())
	if err != nil {
		t.Fatal(err)
	}
	m := mustParse(t, mustParse(t, run).data(1))
	state := mustParse(t, m.data(1))
	var root map[string]any
	if err = json.Unmarshal(h.blobs[string(state.data(1))], &root); err != nil || root["role"] != "system" {
		t.Fatalf("%v %#v", err, root)
	}
	if _, _, err = readFrame(io.LimitReader(bytes.NewReader(frame(run)), 3)); err == nil {
		t.Fatal(fmt.Sprint("truncation accepted"))
	}
}
