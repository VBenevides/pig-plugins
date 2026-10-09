package imageview_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

// TestBundledImageView exercises the real native Go bundle, not source mocks.
// Build once with scripts/dev_build.sh and set PIG_IMAGE_SMOKE_BINARY to its path.
func TestBundledImageView(t *testing.T) {
	binary := os.Getenv("PIG_IMAGE_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set PIG_IMAGE_SMOKE_BINARY to the bundled development executable")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(
		pigtest.Text("first image received"),
		pigtest.Text("second image received"),
		pigtest.Calls(pigtest.Call("read", map[string]any{"path": "tiny.png"})),
		pigtest.Text("read image received"),
		pigtest.Text("fourth image received"),
	)
	defer mock.Close()

	var tiny, wide bytes.Buffer
	if err := png.Encode(&tiny, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&wide, image.NewRGBA(image.Rect(0, 0, 960, 2))); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home.Work, "tiny.png"), tiny.Bytes())
	write(filepath.Join(home.Work, "wide.png"), wide.Bytes())
	models, err := json.Marshal(map[string]any{"providers": map[string]any{"mock": map[string]any{
		"baseUrl": mock.URL, "apiKey": "x", "api": "openai-completions",
		"models": []any{map[string]any{"id": "mock-model", "name": "Image smoke", "input": []string{"text", "image"}, "contextWindow": 100000, "maxTokens": 4096}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(home.AgentDir(), "models.json"), models)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--mode", "rpc", "--provider", "mock", "--model", "mock-model", "--no-session")
	cmd.Dir = home.Work
	cmd.Env = home.Env(map[string]string{"DISPLAY": "", "WAYLAND_DISPLAY": ""})
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	events := make(chan map[string]any, 128)
	readErrors := make(chan error, 1)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 8<<20)
		for scanner.Scan() {
			var event map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				readErrors <- err
				return
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
		readErrors <- scanner.Err()
	}()
	send := func(value any) {
		t.Helper()
		if err := json.NewEncoder(stdin).Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	until := func(done func(map[string]any) bool) map[string]any {
		t.Helper()
		for {
			select {
			case event, open := <-events:
				if !open {
					t.Fatalf("host closed RPC output: %v", <-readErrors)
				}
				if event["type"] == "extension_error" || event["success"] == false {
					t.Fatalf("host error: %v", event)
				}
				if done(event) {
					return event
				}
			case <-ctx.Done():
				t.Fatal("image smoke timed out")
			}
		}
	}

	send(map[string]any{"type": "get_commands"})
	commands := until(func(event map[string]any) bool {
		return event["type"] == "response" && event["command"] == "get_commands"
	})
	encoded, _ := json.Marshal(commands)
	for _, command := range []string{"pi-image-view", "usage", "better-footer"} {
		if !strings.Contains(string(encoded), `"name":"`+command+`"`) {
			t.Fatalf("bundled native command %q missing: %s", command, encoded)
		}
	}
	prompts := []map[string]any{
		{"type": "prompt", "message": "inspect", "images": []any{map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(tiny.Bytes()), "mimeType": "image/png"}}},
		{"type": "prompt", "message": "inspect ./wide.png"},
		{"type": "prompt", "message": "read tiny.png using the read tool"},
		{"type": "prompt", "message": "inspect ./tiny.png again"},
		{"type": "prompt", "message": "inspect ./tiny.png", "images": []any{map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(tiny.Bytes()), "mimeType": "image/png"}}},
	}
	for _, prompt := range prompts {
		send(prompt)
		until(func(event map[string]any) bool { return event["type"] == "agent_settled" })
	}
	send(map[string]any{"type": "get_state"})
	until(func(event map[string]any) bool { return event["type"] == "response" && event["command"] == "get_state" })
	requests := mock.Requests()
	if len(requests) != 6 {
		t.Fatalf("model requests = %d, want 6", len(requests))
	}
	for index, expectedImages := range []int{1, 2, 2, 3, 4, 5} {
		data, _ := json.Marshal(requests[index]["messages"])
		if count := strings.Count(string(data), `"type":"image_url"`); count != expectedImages {
			t.Fatalf("request %d: images = %d, want %d: %s", index, count, expectedImages, data)
		}
		for number := 1; number <= expectedImages; number++ {
			marker := "[Image #" + strconv.Itoa(number) + "]"
			if !strings.Contains(string(data), marker) {
				t.Fatalf("request %d missing %s: %s", index, marker, data)
			}
		}
		if strings.Contains(string(data), "image-view/blobs/") {
			t.Fatalf("request %d leaks a persisted blob path", index)
		}
		assertThumbnails(t, requests[index]["messages"])
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("bundled host exited: %v\n%s", err, stderr.String())
	}
	seen := map[string]bool{}
	for _, name := range pigtest.ToolNames(mock) {
		if seen[name] {
			t.Fatalf("duplicate tool: %s", name)
		}
		seen[name] = true
	}
	if !seen["read"] || !seen["edit"] {
		t.Fatalf("native hashline tools missing: %v", seen)
	}
	if strings.Contains(stderr.String(), "extension failed") {
		t.Fatalf("extension failure: %s", stderr.String())
	}
}

func assertThumbnails(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			assertThumbnails(t, item)
		}
	case map[string]any:
		if value["type"] == "image_url" {
			attachment, _ := value["image_url"].(map[string]any)
			url, _ := attachment["url"].(string)
			if !strings.HasPrefix(url, "data:image/png;base64,") {
				t.Fatalf("not a PNG model thumbnail: %q", url)
			}
			data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, "data:image/png;base64,"))
			if err != nil {
				t.Fatal(err)
			}
			dimensions, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil || dimensions.Width > 480 || dimensions.Height > 480 {
				t.Fatalf("thumbnail dimensions: %+v, error: %v", dimensions, err)
			}
		}
		for _, item := range value {
			assertThumbnails(t, item)
		}
	}
}
