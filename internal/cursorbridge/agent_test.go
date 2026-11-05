package cursorbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentContinuation(t *testing.T) {
	sends := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk.v1.SdkAgentService/CreateAgent" {
			w.Header().Set("Content-Type", "application/json")
			var in struct {
				Options map[string]json.RawMessage `json:"options"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if string(in.Options["apiKey"]) != `"account-key"` || string(in.Options["tools"]) != `{"names":[]}` || string(in.Options["mcpServers"]) != `{}` {
				t.Error("unsafe agent options", in.Options)
			}
			io.WriteString(w, `{"agentId":"agent-one"}`)
			return
		}
		w.Header().Set("Content-Type", "application/connect+json")
		var header [5]byte
		io.ReadFull(r.Body, header[:])
		var in struct {
			AgentID string `json:"agentId"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if in.AgentID != "agent-one" || in.Message.Text == "" {
			t.Error("continuation lost")
		}
		sends++
		frame(w, 0, map[string]any{"result": map[string]any{"agentId": "agent-one", "runId": "run", "status": "RUN_LIFECYCLE_STATUS_FINISHED", "result": map[string]any{"result": "answer"}}})
		frame(w, 2, map[string]any{})
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "token")
	defer c.Close()
	id, err := c.Create(context.Background(), "account-key", Selection{ID: "grok"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"user question", "host tool result"} {
		result, err := c.Send(context.Background(), id, prompt, nil, nil)
		if err != nil || result.Result.Text != "answer" {
			t.Fatal(result, err)
		}
	}
	if sends != 2 {
		t.Fatal(sends)
	}
}
func TestMissingTerminalResultFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/connect+json")
		frame(w, 0, map[string]any{"sdkMessage": map[string]string{"type": "text"}})
		frame(w, 2, map[string]any{})
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "token")
	defer c.Close()
	if _, err := c.Send(context.Background(), "agent", "prompt", nil, nil); err == nil {
		t.Fatal("incomplete SDK stream passed")
	}
}

func TestUserMessageCarriesBase64ImagesOnlyWhenPresent(t *testing.T) {
	if got, _ := json.Marshal(userMessage("hi", nil)); string(got) != `{"text":"hi"}` {
		t.Fatalf("%s", got)
	}
	got, _ := json.Marshal(userMessage("hi", []Image{{Data: "AAAA", MimeType: "image/png"}}))
	if string(got) != `{"images":[{"data":{"data":"AAAA","mimeType":"image/png"}}],"text":"hi"}` {
		t.Fatalf("%s", got)
	}
}
