// Package cursorcli implements the opt-in, text-only Cursor CLI beta transport.
package cursorcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxBytes = 4 << 20

// Model metadata must be supplied explicitly: the CLI has no verified JSON
// catalog contract, and login alone does not establish model entitlement.
type Model struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int    `json:"contextWindow"`
	MaxTokens     int    `json:"maxTokens"`
}

type Config struct {
	CLI    string  `json:"cli"`
	Models []Model `json:"models"`
}

// Load returns nil when the opt-in file does not exist. Unknown fields fail
// closed so a misspelled security or model setting cannot silently take effect.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cursor beta config: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return nil, fmt.Errorf("cursor beta config: %w", err)
	}
	if len(data) > 65536 {
		return nil, errors.New("cursor beta config exceeds 64 KiB")
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("cursor beta config: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("cursor beta config must contain one JSON object")
	}
	if !filepath.IsAbs(cfg.CLI) {
		return nil, errors.New("cursor beta cli must be an absolute executable path")
	}
	if len(cfg.Models) == 0 || len(cfg.Models) > 100 {
		return nil, errors.New("cursor beta requires 1..100 explicit models")
	}
	seen := map[string]bool{}
	for _, model := range cfg.Models {
		if model.ID == "" || strings.HasPrefix(model.ID, "-") || strings.ContainsAny(model.ID, "\x00\r\n") || model.Name == "" || model.ContextWindow <= 0 || model.MaxTokens <= 0 || model.MaxTokens > model.ContextWindow || seen[model.ID] {
			return nil, errors.New("cursor beta model requires a unique ID, name and valid explicit token limits")
		}
		seen[model.ID] = true
	}
	return &cfg, nil
}

// Prompt supports text conversations only. It intentionally rejects tools,
// images and tool results rather than delegate PiG's tool loop to another agent.
func Prompt(request map[string]any) (string, error) {
	if tools, ok := request["tools"]; ok && tools != nil {
		data, err := json.Marshal(tools)
		if err != nil || string(data) != "[]" {
			return "", errors.New("cursor beta is text-only; start PiG with --no-tools")
		}
	}
	var prompt strings.Builder
	if rawSystem := request["systemPrompt"]; rawSystem != nil {
		system, ok := rawSystem.(string)
		if !ok {
			return "", errors.New("cursor beta requires a text system prompt")
		}
		prompt.WriteString("System instructions:\n" + system + "\n\n")
	}
	messages, ok := request["messages"].([]any)
	if !ok || len(messages) == 0 {
		return "", errors.New("cursor beta requires text messages")
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return "", errors.New("cursor beta invalid message")
		}
		role, _ := message["role"].(string)
		if role != "user" && role != "assistant" && role != "system" {
			return "", errors.New("cursor beta does not support tool-result or non-conversation messages")
		}
		for _, key := range []string{"toolsAdded", "toolsRemoved"} {
			if value := message[key]; value != nil {
				encoded, err := json.Marshal(value)
				if err != nil || string(encoded) != "[]" {
					return "", errors.New("cursor beta cannot accept transcript tool changes; use --no-tools in a fresh session")
				}
			}
		}
		prompt.WriteString(role + ":\n")
		switch content := message["content"].(type) {
		case nil:
			if role != "system" {
				return "", errors.New("cursor beta invalid empty content")
			}
		case string:
			prompt.WriteString(content)
		case []any:
			for _, rawPart := range content {
				part, ok := rawPart.(map[string]any)
				if !ok || part["type"] != "text" {
					return "", errors.New("cursor beta supports text content only")
				}
				text, ok := part["text"].(string)
				if !ok {
					return "", errors.New("cursor beta invalid text content")
				}
				prompt.WriteString(text)
			}
		default:
			return "", errors.New("cursor beta invalid content")
		}
		if sections := message["sections"]; sections != nil {
			values, ok := sections.(map[string]any)
			if role != "system" || !ok {
				return "", errors.New("cursor beta invalid system sections")
			}
			for _, value := range values {
				if value != nil {
					if _, ok := value.(string); !ok {
						return "", errors.New("cursor beta invalid system section text")
					}
				}
			}
			encoded, err := json.Marshal(values)
			if err != nil {
				return "", fmt.Errorf("cursor beta encode system sections: %w", err)
			}
			prompt.WriteString("\nSystem section updates (null removes the section):\n" + string(encoded))
		}
		prompt.WriteString("\n\n")
		if prompt.Len() > maxBytes {
			return "", errors.New("cursor beta prompt exceeds 4 MiB")
		}
	}
	return prompt.String(), nil
}

// Run never runs in the user's workspace or inherits CLI configuration, MCP
// servers, hooks, arbitrary endpoints, proxy settings, or ambient credentials.
// Output is buffered until success: no partial generation is retried/replayed.
func Run(ctx context.Context, cfg Config, model, token, prompt string) (string, error) {
	found := false
	for _, configured := range cfg.Models {
		found = found || configured.ID == model
	}
	if !found {
		return "", errors.New("cursor beta model is not configured")
	}
	if token == "" || strings.ContainsAny(token, "\x00\r\n") {
		return "", errors.New("cursor beta requires a selected Cursor account credential")
	}
	if len(prompt) > maxBytes {
		return "", errors.New("cursor beta prompt exceeds 4 MiB")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	root, err := os.MkdirTemp("", "pig-cursor-beta-")
	if err != nil {
		return "", fmt.Errorf("cursor beta isolate workspace: %w", err)
	}
	defer os.RemoveAll(root)
	cursorDir := filepath.Join(root, ".cursor")
	if err := os.Mkdir(cursorDir, 0700); err != nil {
		return "", fmt.Errorf("cursor beta isolate config: %w", err)
	}
	permissions := []byte(`{"permissions":{"allow":[],"deny":["Shell(*)","Read(**)","Write(**)","WebFetch(*)","Mcp(*:*)"]}}`)
	if err := os.WriteFile(filepath.Join(cursorDir, "cli-config.json"), permissions, 0600); err != nil {
		return "", fmt.Errorf("cursor beta deny tools: %w", err)
	}
	cmd := exec.CommandContext(ctx, cfg.CLI, "--mode", "ask", "--print", "--output-format", "stream-json", "--model", model, "--workspace", root)
	cmd.Dir = root
	cmd.Env = []string{"HOME=" + root, "USERPROFILE=" + root, "XDG_CONFIG_HOME=" + root, "PATH=" + os.Getenv("PATH"), "NO_COLOR=1", "NO_OPEN_BROWSER=1", "CURSOR_API_ENDPOINT=https://api2.cursor.sh"}
	if strings.HasPrefix(token, "crsr_") {
		cmd.Env = append(cmd.Env, "CURSOR_API_KEY="+token)
	} else {
		cmd.Env = append(cmd.Env, "CURSOR_AUTH_TOKEN="+token)
	}
	cmd.Stdin = strings.NewReader(prompt)
	// CLI error payloads may contain credentials or prompt content. Preserve the
	// process failure and operation, but do not forward untrusted stderr.
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("cursor beta stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("cursor beta start CLI: %w", err)
	}
	readDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = stdout.Close()
		case <-readDone:
		}
	}()
	text, decodeErr := decode(stdout)
	close(readDone)
	contextErr := ctx.Err()
	if decodeErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if contextErr != nil {
		return "", fmt.Errorf("cursor beta request: %w", contextErr)
	}
	if decodeErr != nil {
		return "", errors.Join(decodeErr, waitErr)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("cursor beta request: %w", ctx.Err())
	}
	if waitErr != nil {
		return "", fmt.Errorf("cursor beta CLI failed (stderr withheld for privacy): %w", waitErr)
	}
	return text, nil
}

func decode(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, maxBytes+1))
	scanner.Buffer(make([]byte, 4096), maxBytes)
	total := 0
	result := ""
	completed := false
	for scanner.Scan() {
		total += len(scanner.Bytes()) + 1
		if total > maxBytes {
			return "", errors.New("cursor beta response exceeds 4 MiB")
		}
		var event struct {
			Type    string  `json:"type"`
			Subtype string  `json:"subtype"`
			IsError *bool   `json:"is_error"`
			Result  *string `json:"result"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return "", errors.New("cursor beta malformed CLI event (payload withheld)")
		}
		if completed {
			return "", errors.New("cursor beta unexpected event after terminal result")
		}
		switch event.Type {
		case "system", "user", "assistant":
		case "tool_call":
			return "", errors.New("cursor beta refused unexpected CLI tool execution")
		case "result":
			if event.Subtype != "success" || event.IsError == nil || *event.IsError || event.Result == nil {
				return "", errors.New("cursor beta CLI reported unsuccessful result")
			}
			result, completed = *event.Result, true
		default:
			return "", errors.New("cursor beta unsupported CLI event type")
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("cursor beta read CLI events: %w", err)
	}
	if !completed {
		return "", errors.New("cursor beta CLI ended without a successful terminal result")
	}
	return result, nil
}
