package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func fixtures(t *testing.T) map[string][]map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../testfixtures/websearch.json")
	if err != nil {
		t.Fatal(err)
	}
	var data map[string][]map[string]any
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return data
}
func emit(w http.ResponseWriter, events []map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		raw, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", raw)
	}
}
func TestNativeStreamParity(t *testing.T) {
	raw, err := os.ReadFile("../../testfixtures/websearch.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]struct {
		Text    string   `json:"text"`
		Sources []Source `json:"sources"`
		Native  bool     `json:"nativeSearchUsed"`
		Queries []string `json:"searchQueries"`
	}
	if err = json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	for kind, events := range fixtures(t) {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("unexpected request %s", r.Method)
				}
				emit(w, events)
			}))
			defer server.Close()
			api := "openai-responses"
			provider := kind
			if kind == "google" {
				api = "google-generative-ai"
				provider = api
			}
			if kind == "anthropic" {
				api = "anthropic-messages"
			}
			result, err := Search(context.Background(), Request{Model: Model{ID: "search-model", Provider: provider, API: api, BaseURL: server.URL + "/v1", MaxTokens: 4096}, Auth: Auth{OK: true, Key: "test-key"}, Query: "query"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			text, _ := result.Format("search-model", false)
			want := golden[kind]
			if text != want.Text || !slices.Equal(result.Sources, want.Sources) || result.Native != want.Native || !slices.Equal(result.Queries, want.Queries) {
				t.Fatalf("got %q, sources=%v native=%v queries=%v; want %+v", text, result.Sources, result.Native, result.Queries, want)
			}
		})
	}
}
func TestFailuresAreNotSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, api string
		status    int
		events    []map[string]any
		want      string
	}{{"truncated", "openai-responses", 200, []map[string]any{{"type": "response.output_text.delta", "delta": "partial"}}, "without a terminal"}, {"incomplete", "openai-responses", 200, []map[string]any{{"type": "response.incomplete", "response": map[string]any{"incomplete_details": map[string]any{"reason": "max_output_tokens"}}}}, "max_output_tokens"}, {"google-limit", "google-generative-ai", 200, []map[string]any{{"candidates": []any{map[string]any{"finishReason": "MAX_TOKENS"}}}}, "MAX_TOKENS"}, {"unauthorized", "openai-responses", 401, nil, "HTTP 401"}, {"rate-limited", "openai-responses", 429, nil, "HTTP 429"}, {"provider-error", "openai-responses", 200, []map[string]any{{"type": "error", "message": "secret-key"}}, "[redacted]"}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 200 {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, "secret-key")
					return
				}
				emit(w, tc.events)
			}))
			defer server.Close()
			result, err := Search(context.Background(), Request{Model: Model{ID: "m", Provider: tc.api, API: tc.api, BaseURL: server.URL}, Auth: Auth{OK: true, Key: "secret-key"}}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret-key") || result.Text != "" {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}
func TestCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Search(ctx, Request{Model: Model{ID: "m", API: "openai-responses", BaseURL: server.URL}, Auth: Auth{OK: true}}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
}
func TestMetadataAndCodexAuthBoundaries(t *testing.T) {
	if boundedEvent(map[string]any{"items": make([]any, 257)}) == nil {
		t.Fatal("oversized metadata accepted")
	}
	_, _, _, err := Build(Request{Model: Model{ID: "m", API: "openai-codex-responses", BaseURL: "https://chatgpt.com/backend-api"}, Auth: Auth{OK: true, Key: "invalid-token"}})
	if err == nil || !strings.Contains(err.Error(), "account ID") {
		t.Fatalf("bad JWT accepted: %v", err)
	}
	endpoint, headers, body, err := Build(Request{Model: Model{ID: "m", API: "openai-codex-responses", BaseURL: "https://chatgpt.com/backend-api/codex/responses"}, Auth: Auth{OK: true, Headers: map[string]string{"Authorization": "Bearer opaque", "chatgpt-account-id": "account"}}})
	if err != nil || endpoint != "https://chatgpt.com/backend-api/codex/responses" || headers.Get("chatgpt-account-id") != "account" || body["tool_choice"] != "required" {
		t.Fatalf("header-only Codex auth: %s %v %v %v", endpoint, headers, body, err)
	}
}
