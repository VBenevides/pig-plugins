package cursorbridge

import (
	"context"
	"encoding/json"
	"errors"
)

// Create starts a local agent with no built-in tools or inherited settings.
// Custom tools and callbacks are deliberately absent at this layer.
func (c *Client) Create(ctx context.Context, key string, model Selection) (string, error) {
	if key == "" || model.ID == "" {
		return "", errors.New("Cursor account key and model are required")
	}
	options := map[string]any{
		"apiKey": key, "model": model,
		"local": map[string]any{"settingSources": []string{}, "autoReview": false, "sandboxOptions": map[string]bool{"enabled": true}},
		"tools": map[string]any{"names": []string{}}, "mcpServers": map[string]any{}, "agents": map[string]any{},
	}
	var response struct {
		AgentID string `json:"agentId"`
	}
	if err := c.Unary(ctx, agents+"CreateAgent", map[string]any{"options": options}, &response); err != nil {
		return "", err
	}
	if response.AgentID == "" {
		return "", errors.New("Cursor bridge returned an empty agent identifier")
	}
	return response.AgentID, nil
}

type RunResult struct {
	AgentID string `json:"agentId"`
	RunID   string `json:"runId"`
	Status  string `json:"status"`
	Result  struct {
		Text string `json:"result"`
	} `json:"result"`
}

// Send returns only a terminal, successful SDK result. Consumers may inspect
// intermediate events, but they must not infer completion from text alone.
func (c *Client) Send(ctx context.Context, agentID, prompt string, consume func(json.RawMessage) error) (RunResult, error) {
	var result *RunResult
	err := c.Stream(ctx, agents+"Send", map[string]any{"agentId": agentID, "message": map[string]string{"text": prompt}, "options": map[string]bool{"enableDeltas": true, "enableSteps": true}}, func(data json.RawMessage) error {
		var envelope struct {
			Result *RunResult `json:"result"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
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
		return RunResult{}, err
	}
	if result == nil || result.AgentID != agentID || result.RunID == "" || result.Status != "RUN_LIFECYCLE_STATUS_FINISHED" {
		return RunResult{}, errors.New("Cursor stream did not finish successfully")
	}
	return *result, nil
}
