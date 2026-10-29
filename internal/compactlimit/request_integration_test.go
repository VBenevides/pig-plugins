package compactlimit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestFinalRequestObservation(t *testing.T) {
	binary := os.Getenv("PIG_REQUEST_TEST_BINARY")
	if binary == "" {
		t.Skip("set PIG_REQUEST_TEST_BINARY to the patched host")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(bin, "pig")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
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
	fixture, err := filepath.Abs("../../testfixtures/request-observation")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Reply{Text: "first"}, pigtest.Reply{Text: "second"})
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{fixture}, Prompts: []string{"first é 😀", "follow-up"}})
	requests := mock.Requests()
	observations, isolated := 0, 0
	for _, notice := range result.Notices() {
		switch {
		case notice == "request-observer-isolated":
			isolated++
		case strings.HasPrefix(notice, "request-observation:"):
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(notice, "request-observation:")), &data); err != nil {
				t.Fatal(err)
			}
			payload, _ := data["payload"].(map[string]any)
			if observations >= len(requests) {
				t.Fatal("more observations than requests")
			}
			if !reflect.DeepEqual(payload, requests[observations]) {
				t.Fatalf("observed payload differs from provider request: %v / %v", payload, requests[observations])
			}
			measurement, _ := data["measurement"].(map[string]any)
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if measurement["SerializedBytes"] != float64(len(encoded)) || measurement["Tokens"] != float64(len(encoded)) {
				t.Fatalf("wrong complete payload size: %v", measurement)
			}
			observations++
		}
	}
	if len(requests) != 2 || observations != 2 || isolated != 2 {
		t.Fatalf("requests=%d observations=%d isolated=%d notices=%q stderr=%s", len(requests), observations, isolated, result.Notices(), result.Stderr)
	}
}
