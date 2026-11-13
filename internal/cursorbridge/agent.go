package cursorbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Int64 decodes protobuf JSON 64-bit integers, which arrive as strings.
type Int64 int64

func (n *Int64) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		return nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Cursor integer: %w", err)
	}
	*n = Int64(value)
	return nil
}

// TokenUsage is the SDK's per-run token count. Absent fields stay zero and a
// nil pointer means the bridge reported nothing.
type TokenUsage struct {
	InputTokens      Int64 `json:"inputTokens"`
	OutputTokens     Int64 `json:"outputTokens"`
	CacheReadTokens  Int64 `json:"cacheReadTokens"`
	CacheWriteTokens Int64 `json:"cacheWriteTokens"`
	TotalTokens      Int64 `json:"totalTokens"`
	ReasoningTokens  Int64 `json:"reasoningTokens"`
}

// Create permits only the SDK's internal MCP group when PiG custom tools exist.
// No external MCP servers, built-in file/shell tools, agents, or settings are
// inherited. The SDK transports host callbacks through its MCP tool group.
func (c *Client) Create(ctx context.Context, key string, model Selection, tools []ToolDef) (string, error) {
	if key == "" || model.ID == "" {
		return "", errors.New("cursor account key and model are required")
	}
	custom := map[string]any{}
	for _, tool := range tools {
		if tool.Name == "" || custom[tool.Name] != nil {
			return "", errors.New("cursor tool names must be nonempty and unique")
		}
		schema := tool.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		custom[tool.Name] = map[string]any{"description": tool.Description, "inputSchema": schema}
	}
	names := []string{}
	if len(custom) > 0 {
		names = []string{"mcp"}
	}
	options := map[string]any{
		"apiKey": key, "model": model,
		"local": map[string]any{"settingSources": []string{}, "autoReview": false, "enableAgentRetries": false, "sandboxOptions": map[string]bool{"enabled": true}, "customTools": custom},
		"tools": map[string]any{"names": names}, "mcpServers": map[string]any{}, "agents": map[string]any{},
	}
	var response struct {
		AgentID string `json:"agentId"`
	}
	if err := c.Unary(ctx, agents+"CreateAgent", map[string]any{"options": options}, &response); err != nil {
		return "", err
	}
	if response.AgentID == "" {
		return "", errors.New("cursor bridge returned an empty agent identifier")
	}
	return response.AgentID, nil
}

// SetToolCallback registers the loopback endpoint that receives tool proposals.
func (c *Client) SetToolCallback(ctx context.Context, url, token string) error {
	var out struct{}
	return c.Unary(ctx, control+"SetToolCallback", map[string]string{"url": url, "authToken": token}, &out)
}

type RunResult struct {
	AgentID string `json:"agentId"`
	RunID   string `json:"runId"`
	Status  string `json:"status"`
	Result  struct {
		Text  string      `json:"result"`
		Usage *TokenUsage `json:"usage"`
	} `json:"result"`
}

// Image is base64 image data sent with a user message to a local agent.
type Image struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

func userMessage(prompt string, images []Image) map[string]any {
	message := map[string]any{"text": prompt}
	if len(images) == 0 {
		return message
	}
	sent := make([]map[string]any, 0, len(images))
	for _, image := range images {
		sent = append(sent, map[string]any{"data": map[string]string{"data": image.Data, "mimeType": image.MimeType}})
	}
	message["images"] = sent
	return message
}

// Send returns only a terminal, successful SDK result. Consumers may inspect
// intermediate events, but they must not infer completion from text alone.
func (c *Client) Send(ctx context.Context, agentID, prompt string, images []Image, consume func(json.RawMessage) error) (RunResult, error) {
	var result *RunResult
	liveRunID := ""
	err := c.Stream(ctx, agents+"Send", map[string]any{"agentId": agentID, "message": userMessage(prompt, images), "options": map[string]bool{"enableDeltas": true, "enableSteps": false}}, func(data json.RawMessage) error {
		var envelope struct {
			Result     *RunResult `json:"result"`
			SDKMessage struct {
				AgentID string `json:"agentId"`
				RunID   string `json:"runId"`
			} `json:"sdkMessage"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
		}
		if envelope.SDKMessage.AgentID != "" && envelope.SDKMessage.AgentID != agentID {
			return errors.New("cursor stream changed agent identity")
		}
		if id := envelope.SDKMessage.RunID; id != "" {
			if liveRunID != "" && liveRunID != id {
				return errors.New("cursor stream changed run identity")
			}
			liveRunID = id
		}
		if envelope.Result != nil {
			if result != nil {
				return errors.New("duplicate Cursor run result")
			}
			result = envelope.Result
		}
		if consume != nil {
			return consume(data)
		}
		return nil
	})
	if err != nil {
		return RunResult{AgentID: agentID, RunID: liveRunID}, err
	}
	if result == nil || result.AgentID != agentID || result.RunID == "" || (liveRunID != "" && result.RunID != liveRunID) || result.Status != "RUN_LIFECYCLE_STATUS_FINISHED" {
		return RunResult{AgentID: agentID, RunID: liveRunID}, errors.New("cursor stream did not finish successfully")
	}
	return *result, nil
}
