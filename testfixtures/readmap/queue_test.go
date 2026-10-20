package readmap_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestBundledRPCSteerAndQueue(t *testing.T) {
	binary := os.Getenv("PIG_READMAP_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set PIG_READMAP_SMOKE_BINARY to a candidate fused binary")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"auto-model.json":   `{"primary":{"provider":"mock","model":"mock-model","thinking":"off"},"fallback":{"provider":"mock","model":"mock-model","thinking":"off"}}`,
		"check-update.json": `{"enabled":false}`,
	} {
		if err := os.WriteFile(filepath.Join(home.AgentDir(), name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mock := pigtest.NewMockLLM(pigtest.Reply{Text: "first", Delay: time.Second}, pigtest.Text("steered"), pigtest.Text("queued"))
	defer mock.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--mode", "rpc", "--provider", "mock", "--model", "mock-model", "--offline")
	cmd.Env = home.Env(nil)
	cmd.Dir = home.Work
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		in.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	events := make(chan map[string]any, 256)
	go func() {
		defer close(events)
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 4096), 4<<20)
		for s.Scan() {
			var e map[string]any
			if json.Unmarshal(s.Bytes(), &e) == nil {
				events <- e
			}
		}
	}()
	send := func(v any) {
		t.Helper()
		if err := json.NewEncoder(in).Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	send(map[string]any{"type": "prompt", "message": "initial work"})
	for len(mock.Requests()) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("no initial request")
		case <-time.After(10 * time.Millisecond):
		}
	}
	send(map[string]any{"type": "steer", "message": "steer marker"})
	send(map[string]any{"type": "follow_up", "message": "queue marker"})
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("RPC closed: %s", stderr.String())
			}
			if e["type"] == "response" && e["success"] == false {
				t.Fatalf("RPC rejection: %v", e)
			}
			if e["type"] == "agent_settled" {
				goto settled
			}
		case <-ctx.Done():
			t.Fatalf("queue timed out: %s", stderr.String())
		}
	}
settled:
	requests := mock.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests=%d, want exactly 3", len(requests))
	}
	for i, want := range []string{"initial work", "steer marker", "queue marker"} {
		data, _ := json.Marshal(requests[i]["messages"])
		if !strings.Contains(string(data), want) {
			t.Fatalf("request %d lacks %q: %s", i, want, data)
		}
	}
	in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("RPC exit: %v: %s", err, stderr.String())
	}
}
