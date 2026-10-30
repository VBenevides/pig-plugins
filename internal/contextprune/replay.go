package contextprune

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
)

// ReductionScope contains only trusted host metadata. Never build it from tool text.
// Account is an opaque credential identity, not a token, label, or an email address.
// Anchor identifies the checkpoint in the current branch, not the changing leaf.
type ReductionScope struct {
	Session  string
	Anchor   string
	Provider string
	Model    string
	API      string
	Endpoint string
	Account  string
}

// Fingerprint refuses unknown credential identity and endpoints with credentials.
// Callers must also prove that the anchor is an ancestor of the current branch.
func (scope ReductionScope) Fingerprint() (string, error) {
	if scope.Session == "" || scope.Anchor == "" || scope.Provider == "" || scope.Model == "" || scope.API == "" || scope.Account == "" {
		return "", fmt.Errorf("folding requires complete session, checkpoint, model, and credential identity")
	}
	endpoint, err := url.Parse(scope.Endpoint)
	if err != nil {
		return "", endpointScopeError{cause: err}
	}
	if endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", fmt.Errorf("folding requires a verified endpoint without embedded credentials or query parameters")
	}
	data, err := json.Marshal(scope)
	if err != nil {
		return "", fmt.Errorf("encode reduction scope: %w", err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

type endpointScopeError struct{ cause error }

func (endpointScopeError) Error() string     { return "folding endpoint is invalid" }
func (err endpointScopeError) Unwrap() error { return err.cause }

// FoldCompatibility is deliberately narrow. OpenAI Responses, Codex, Anthropic,
// Gemini, and unknown APIs remain excluded until their complete replay contracts
// and provider fixtures pass. Normal host compaction remains available.
func FoldCompatibility(current ReductionScope, checkpoint string, messages []any) error {
	if current.API != "openai-completions" || current.Provider == "cursor" {
		return fmt.Errorf("folding is not verified for this provider API; use normal host compaction")
	}
	fingerprint, err := current.Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != checkpoint {
		return fmt.Errorf("reduction scope changed; keep original history")
	}
	for _, value := range messages {
		message, ok := value.(map[string]any)
		if !ok || ReplaySensitive(message) {
			return fmt.Errorf("range contains opaque state or a generation replay veto")
		}
		if message["role"] != "assistant" && message["role"] != "toolResult" {
			return fmt.Errorf("range contains protected instructions or user requests")
		}
		if provider, _ := message["provider"].(string); provider != "" && provider != current.Provider {
			return fmt.Errorf("range belongs to a different provider")
		}
		if model, _ := message["model"].(string); model != "" && model != current.Model {
			return fmt.Errorf("range belongs to a different model")
		}
		if api, _ := message["api"].(string); api != "" && api != current.API {
			return fmt.Errorf("range belongs to a different provider API")
		}
		blocks, _ := message["content"].([]any)
		for _, block := range blocks {
			if object, ok := block.(map[string]any); ok && object["type"] == "image" {
				return fmt.Errorf("media must remain in the active view")
			}
		}
	}
	return nil
}

// ReplaySensitive only inspects protocol envelopes. Tool text and argument values
// are untrusted data, not instructions or evidence of provider capabilities.
func ReplaySensitive(message map[string]any) bool {
	if message["provider"] == "cursor" || message["replayProhibited"] == true {
		return true
	}
	for _, key := range []string{"responseId", "response_id", "previousResponseId", "previous_response_id", "providerState", "compaction", "encrypted_content", "replayId", "deferred", "diagnostics", "textSignature", "thinkingSignature", "thoughtSignature", "signature"} {
		if value, ok := message[key]; ok && value != nil && value != "" {
			return true
		}
	}
	blocks, _ := message["content"].([]any)
	for _, value := range blocks {
		block, ok := value.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "text", "toolCall", "image":
		default:
			return true
		}
		for _, key := range []string{"textSignature", "thinkingSignature", "thoughtSignature", "signature", "encrypted_content", "replayId"} {
			if _, exists := block[key]; exists {
				return true
			}
		}
	}
	return false
}
