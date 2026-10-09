package imageview_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestBundledImageDraftTUI(t *testing.T) {
	for _, fixture := range []struct{ protocol, mode string }{
		{"none", "fullscreen"}, {"kitty", "fullscreen"},
		{"iterm2", "regular"}, {"iterm2", "fullscreen"},
	} {
		t.Run(fixture.protocol+"-"+fixture.mode, func(t *testing.T) {
			testBundledImageDraftTUI(t, fixture.protocol, fixture.mode)
		})
	}
}

func testBundledImageDraftTUI(t *testing.T, protocol, mode string) {
	binary := os.Getenv("PIG_IMAGE_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set PIG_IMAGE_SMOKE_BINARY to the bundled development executable")
	}
	if runtime.GOOS != "linux" {
		t.Skip("terminal smoke fixture requires Linux")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("terminal smoke fixture requires python3")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Text("draft images delivered"))
	defer mock.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}, Vision: true}})
	// Existing settings skip unrelated first-run theme/analytics setup.
	settings := "{}"
	if mode == "regular" {
		settings = `{"tuiMode":"regular"}`
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home.Work, "clipboard.png")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, filepath.Join(filepath.Dir(file), "draft_tui.py"), binary, path)
	cmd.Dir = home.Work
	expectedProtocol := protocol
	// PiG intentionally disables iTerm2 inline images in the alternate buffer.
	if protocol == "iterm2" && mode == "fullscreen" {
		expectedProtocol = "none"
	}
	cmd.Env = home.Env(map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "", "DISPLAY": "", "WAYLAND_DISPLAY": "", "PI_IMAGE_PROTOCOL": protocol, "PIG_TEST_PREVIEW_PROTOCOL": expectedProtocol})
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("draft TUI smoke: %v\n%s", err, output)
	}
	requests := mock.Requests()
	if len(requests) != 1 {
		t.Fatalf("model requests=%d, want one submission", len(requests))
	}
	messages, err := json.Marshal(requests[0]["messages"])
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(messages), `"type":"image_url"`); count != 2 {
		t.Fatalf("submitted draft delivered %d images to the model, want two", count)
	}
	for _, label := range []string{"[Image #1]", "[Image #2]"} {
		if !strings.Contains(string(messages), label) {
			t.Fatalf("model request lost %s", label)
		}
	}
	assertThumbnails(t, requests[0]["messages"])
	t.Log(string(output))
}
