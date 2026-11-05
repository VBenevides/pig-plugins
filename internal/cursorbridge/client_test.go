package cursorbridge

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// realVersion is the reply the shipped v1.2.0 archive gives: it reports bridge
// version 1.0.0, so the release tag cannot be used as the contract.
const realVersion = `{"bridgeVersion":"1.0.0","protocolVersion":"sdk.v1","capabilities":["agent.create","agent.send","run.cancel","cursor.catalog","agent.usage"]}`

func TestBridgeContractIsCheckedByProtocolAndCapabilities(t *testing.T) {
	for name, reply := range map[string]string{
		"other protocol":     `{"protocolVersion":"sdk.v2","capabilities":["agent.create","agent.send","run.cancel","cursor.catalog"]}`,
		"missing capability": `{"protocolVersion":"sdk.v1","capabilities":["agent.create","agent.send","cursor.catalog"]}`,
		"no capabilities":    `{"protocolVersion":"sdk.v1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "GetVersion") {
					io.WriteString(w, reply)
					return
				}
				io.WriteString(w, `{}`)
			}))
			defer server.Close()
			client, _ := NewClient(server.URL, "token")
			defer client.Close()
			if err := client.Check(context.Background()); err == nil {
				t.Fatal("incompatible bridge accepted")
			}
		})
	}
}

func frame(w io.Writer, flags byte, value any) {
	body, _ := json.Marshal(value)
	header := make([]byte, 5)
	header[0] = flags
	binary.BigEndian.PutUint32(header[1:], uint32(len(body)))
	w.Write(header)
	w.Write(body)
}
func TestLoopbackOnly(t *testing.T) {
	for _, url := range []string{"https://127.0.0.1:80", "http://localhost:80", "http://example.com:80", "http://127.0.0.1:80/path", "http://user@127.0.0.1:80", "http://127.0.0.1:80?x=1"} {
		if _, err := NewClient(url, "token"); err == nil {
			t.Fatalf("accepted %s", url)
		}
	}
}
func TestCatalogAndLifecycle(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-secret" || r.ProtoMajor != 1 {
			t.Error("wrong bridge authentication or protocol")
		}
		calls = append(calls, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sdk.v1.SdkBridgeControlService/Ping":
			io.WriteString(w, `{"message":"pong"}`)
		case "/sdk.v1.SdkBridgeControlService/GetVersion":
			io.WriteString(w, realVersion)
		case "/sdk.v1.SdkCursorService/ListModels":
			var in struct {
				Options struct {
					APIKey string `json:"apiKey"`
				} `json:"options"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.Options.APIKey != "account-key" {
				t.Error("missing explicit account key")
			}
			io.WriteString(w, `{"items":[{"id":"grok-4.6"}]}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "local-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	if err = client.Check(ctx); err != nil {
		t.Fatal(err)
	}
	models, err := client.Models(ctx, "account-key")
	if err != nil || len(models) != 1 || models[0].ID != "grok-4.6" {
		t.Fatalf("catalog: %v %v", models, err)
	}
	if err = client.Cancel(ctx, "a", "r"); err != nil {
		t.Fatal(err)
	}
	if err = client.CloseAgent(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 5 {
		t.Fatal(calls)
	}
}
func TestStreamContract(t *testing.T) {
	for _, scenario := range []string{"success", "partial", "bad-flags", "oversize", "remote-error", "invalid-json", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/connect+json")
				switch scenario {
				case "bad-flags":
					frame(w, 1, map[string]any{})
				case "oversize":
					w.Write([]byte{0, 0xff, 0xff, 0xff, 0xff})
				case "remote-error":
					frame(w, 2, map[string]any{"error": map[string]string{"code": "unauthenticated", "message": "account-key"}})
				case "invalid-json":
					w.Write([]byte{0, 0, 0, 0, 1, '{'})
				case "cancel":
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				default:
					var header [5]byte
					io.ReadFull(r.Body, header[:])
					if header[0] != 0 {
						t.Error("request frame flags")
					}
					frame(w, 0, map[string]any{"sdkMessage": map[string]any{"type": "text", "message": map[string]string{"text": "hello"}}})
					if scenario == "success" {
						frame(w, 2, map[string]any{})
					}
				}
			}))
			defer server.Close()
			client, _ := NewClient(server.URL, "local-secret")
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			events := 0
			err := client.Stream(ctx, agents+"Send", map[string]any{"agentId": "a", "message": map[string]string{"text": "prompt"}}, func(json.RawMessage) error { events++; return nil })
			if scenario == "success" {
				if err != nil || events != 1 {
					t.Fatalf("events %d, error %v", events, err)
				}
			} else if err == nil {
				t.Fatal("invalid stream succeeded")
			}
			if err != nil && strings.Contains(err.Error(), "account-key") {
				t.Fatal("credential leaked")
			}
		})
	}
}
func TestErrorsDoNotRetryOrLeak(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(503)
		io.WriteString(w, `{"code":"unavailable","message":"SECRET"}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "token")
	defer c.Close()
	err := c.Check(context.Background())
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != "unavailable" || calls != 1 || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe error: %v", err)
	}
}
func TestProcessLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix test fixture")
	}
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("local-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "GetVersion") {
			io.WriteString(w, realVersion)
		} else {
			io.WriteString(w, `{}`)
		}
	}))
	defer server.Close()
	ready, _ := json.Marshal(Ready{1, "tcp", "connect", server.URL, token})
	script := filepath.Join(t.TempDir(), "bridge")
	content := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s' >&2\nexec sleep 60\n", "cursor-sdk-bridge ready "+string(ready))
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	p, err := start(context.Background(), script, "account-key", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(p.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state leaked")
	}
}
func TestStartupFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix test fixture")
	}
	for _, body := range []string{"exit 1", "exec sleep 60"} {
		script := filepath.Join(t.TempDir(), "bridge")
		os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0700)
		if _, err := start(context.Background(), script, "secret", 50*time.Millisecond); err == nil {
			t.Fatal("startup succeeded")
		}
	}
}
