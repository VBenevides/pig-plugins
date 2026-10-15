package cursorlogin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/cursorcli"
)

// betaConfig augments the existing OAuth provider only when explicitly enabled.
// Keeping the provider ID preserves PiG's selected account and refresh behavior.
func betaConfig(config sdk.ProviderConfig) error {
	path := os.Getenv("PIG_CURSOR_BETA_CONFIG")
	if path == "" {
		agentDir := os.Getenv("PIG_CODING_AGENT_DIR")
		if agentDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cursor beta home: %w", err)
			}
			agentDir = filepath.Join(home, ".pig", "agent")
		}
		path = filepath.Join(agentDir, "cursor-beta.json")
	}
	cfg, err := cursorcli.Load(path)
	if err != nil || cfg == nil {
		return err
	}
	models := make([]map[string]any, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		models = append(models, map[string]any{
			"id": model.ID, "name": model.Name + " (Cursor beta, text only)",
			"reasoning": false, "input": []string{"text"},
			"contextWindow": model.ContextWindow, "maxTokens": model.MaxTokens,
		})
	}
	config["api"] = "cursor-cli-beta"
	config["models"] = models
	config["streamSimple"] = sdk.ProviderStreamSimpleFunc(func(ctx sdk.Context, model, request, options map[string]any) (*sdk.ModelEventStream, error) {
		return betaStream(ctx.Done(), *cfg, model, request, options)
	})
	return nil
}

func betaStream(done <-chan struct{}, cfg cursorcli.Config, model, request, options map[string]any) (*sdk.ModelEventStream, error) {
	prompt, err := cursorcli.Prompt(request)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"temperature", "topP", "topK", "seed", "reasoningEffort", "thinkingBudgets", "thinkingBudgetTokens", "thinking", "samplingParams", "toolChoice"} {
		if value, ok := options[key]; ok && value != nil {
			return nil, fmt.Errorf("cursor beta does not support request option %s", key)
		}
	}
	for _, key := range []string{"reasoning", "effort", "thinkingEnabled", "interleavedThinking"} {
		if value := options[key]; value != nil && value != false && value != "" && value != "off" && value != "none" {
			return nil, fmt.Errorf("cursor beta does not support request option %s", key)
		}
	}
	id, _ := model["id"].(string)
	token, _ := options["apiKey"].(string)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	text, err := cursorcli.Run(ctx, cfg, id, token, prompt)
	if err != nil {
		return nil, err
	}
	// Buffered beta: never turn a failed process or partial output into success.
	// The CLI does not report token usage in its documented result contract.
	message := map[string]any{
		"role": "assistant", "api": "cursor-cli-beta", "provider": Provider,
		"model": id, "timestamp": time.Now().UnixMilli(), "stopReason": "stop",
		"content": []any{map[string]any{"type": "text", "text": text}},
	}
	stream := sdk.CreateAssistantMessageEventStream()
	stream.Push(map[string]any{"type": "done", "reason": "stop", "message": message})
	stream.End(message)
	return stream, nil
}
