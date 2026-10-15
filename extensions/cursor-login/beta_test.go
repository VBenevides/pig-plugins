package cursorlogin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/cursorcli"
)

func TestMain(m *testing.M) {
	if os.Getenv("CURSOR_AUTH_TOKEN") == "fake-extension" {
		fmt.Println(`{"type":"result","subtype":"success","result":"beta answer","is_error":false}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func betaTestConfig(t *testing.T) cursorcli.Config {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return cursorcli.Config{CLI: path, Models: []cursorcli.Model{{ID: "test-model", Name: "Test", ContextWindow: 8192, MaxTokens: 1024}}}
}

func TestBetaConfigOptInAndRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor-beta.json")
	t.Setenv("PIG_CURSOR_BETA_CONFIG", path)
	provider := sdk.ProviderConfig{}
	if err := betaConfig(provider); err != nil || len(provider) != 0 {
		t.Fatalf("disabled beta: %v, %v", provider, err)
	}
	data, err := json.Marshal(betaTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := betaConfig(provider); err != nil {
		t.Fatal(err)
	}
	if provider["api"] != "cursor-cli-beta" || provider["streamSimple"] == nil {
		t.Fatalf("provider missing beta transport: %v", provider)
	}
	models, ok := provider["models"].([]map[string]any)
	if !ok || len(models) != 1 || models[0]["id"] != "test-model" || models[0]["reasoning"] != false {
		t.Fatalf("models: %v", provider["models"])
	}
	// The SDK accepts and serializes the custom streaming registration offline.
	ext := sdk.New("cursor-beta-registration-test")
	ext.RegisterProvider(Provider, provider)
	if err := os.WriteFile(path, []byte(`{"bad":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := betaConfig(sdk.ProviderConfig{}); err == nil {
		t.Fatal("invalid config ignored")
	}
}

func TestBetaStreamSuccessAndFailures(t *testing.T) {
	cfg := betaTestConfig(t)
	model := map[string]any{"id": "test-model"}
	request := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	stream, err := betaStream(nil, cfg, model, request, map[string]any{"apiKey": "fake-extension"})
	if err != nil {
		t.Fatal(err)
	}
	result := stream.Result()
	if result["stopReason"] != "stop" || result["provider"] != Provider || result["usage"] != nil {
		t.Fatalf("message: %v", result)
	}
	if content := result["content"].([]any)[0].(map[string]any)["text"]; content != "beta answer" {
		t.Fatalf("text: %v", content)
	}
	for _, options := range []map[string]any{{}, {"apiKey": "fake-extension", "temperature": 0.3}, {"apiKey": "fake-extension", "reasoning": "high"}, {"apiKey": "fake-extension", "thinkingEnabled": true}} {
		if _, err := betaStream(nil, cfg, model, request, options); err == nil {
			t.Fatal("unsupported request accepted")
		}
	}
	request["tools"] = []any{map[string]any{"name": "bash"}}
	if _, err := betaStream(nil, cfg, model, request, map[string]any{"apiKey": "fake-extension"}); err == nil || !strings.Contains(err.Error(), "--no-tools") {
		t.Fatalf("tools: %v", err)
	}
}
