package imageviewext

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBlocksCopy(t *testing.T) {
	original := []any{map[string]any{"text": "history"}}
	copy := blocks(original)
	copy[0]["text"] = "model copy"
	if original[0].(map[string]any)["text"] != "history" {
		t.Fatal("mutated history")
	}
}
func TestBlobRoot(t *testing.T) {
	t.Setenv("PIG_CODING_AGENT_DIR", "")
	home := t.TempDir()
	t.Setenv("PIG_HOME", home)
	want := filepath.Join(home, "agent", "image-view", "blobs")
	if got := blobRoot(); got != want {
		t.Fatalf("root=%s", got)
	}
}

func TestRPCDetailClearAndFailedNeighbor(t *testing.T) {
	pigtest.RequirePig(t)
	_, file, _, _ := runtime.Caller(0)
	home := pigtest.NewHome(t)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1600, 800))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.Work, "wide.png"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("ok"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Vision: true, Extensions: []string{filepath.Dir(file)}, Prompts: []string{"/pi-image-view detail", "inspect ./wide.png", "/pi-image-view clear", "inspect ./wide.png", "inspect ./missing.png and ./wide.png"}})
	requests := mock.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests=%d", len(requests))
	}
	for i, count := range []int{1, 1, 2} {
		data, err := json.Marshal(requests[i]["messages"])
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(string(data), `"type":"image_url"`); got != count {
			t.Fatalf("request %d images=%d want=%d", i, got, count)
		}
		if strings.Contains(string(data), "image-view://") {
			t.Fatal("internal reference leaked")
		}
	}
	data, _ := json.Marshal(requests[0]["messages"])
	var contents []map[string]any
	if err := json.Unmarshal(data, &contents); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range contents {
		for _, block := range blocks(message["content"]) {
			value, ok := block["image_url"].(map[string]any)
			if !ok {
				continue
			}
			url, _ := value["url"].(string)
			parts := strings.SplitN(url, ",", 2)
			if len(parts) != 2 {
				t.Fatal(url)
			}
			raw, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(raw))
			if err != nil || cfg.Width != 1280 {
				t.Fatalf("detail dimensions=%v err=%v", cfg, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("detail attachment missing")
	}
	if !strings.Contains(strings.Join(result.Notices(), "\n"), "missing.png") {
		t.Fatal("missing image failure not observable")
	}
}

// A session start removes previews unused for two days, keeps recent ones and unrelated files, and storing a new
// preview refreshes an identical old blob instead of letting the cleanup delete it.
func TestSessionCleansOldPreviewsAndKeepsUsedOnes(t *testing.T) {
	pigtest.RequirePig(t)
	_, file, _, _ := runtime.Caller(0)
	home := pigtest.NewHome(t)
	blobs := filepath.Join(home.AgentDir(), "image-view", "blobs")
	if err := os.MkdirAll(blobs, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := func(name string, age time.Duration) string {
		path := filepath.Join(blobs, name)
		if err := os.WriteFile(path, []byte("blob"), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
		return path
	}
	old := seed(strings.Repeat("a", 64)+".png", 50*time.Hour)
	recent := seed(strings.Repeat("b", 64)+".png", 10*time.Hour)
	note := seed("keep-me.txt", 500*time.Hour)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 40, 20))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.Work, "small.png"), encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("ok"))
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{Vision: true, Extensions: []string{filepath.Dir(file)}, Prompts: []string{"inspect ./small.png"}})

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("a preview unused for 50 hours must be removed: %v", err)
	}
	for name, path := range map[string]string{"recent preview": recent, "unrelated file": note} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s must stay: %v", name, err)
		}
	}
	entries, err := os.ReadDir(blobs)
	if err != nil {
		t.Fatal(err)
	}
	stored := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".png") && len(entry.Name()) == 68 && entry.Name() != filepath.Base(recent) {
			stored++
		}
	}
	if stored != 1 {
		t.Fatalf("the new image must be stored once, found %d new blobs in %v", stored, entries)
	}
}
