package websearch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Model struct {
	ID          string             `json:"id"`
	Provider    string             `json:"provider"`
	API         string             `json:"api"`
	BaseURL     string             `json:"baseUrl"`
	Headers     map[string]string  `json:"headers"`
	MaxTokens   int                `json:"maxTokens"`
	Reasoning   bool               `json:"reasoning"`
	ThinkingMap map[string]*string `json:"thinkingLevelMap"`
}
type Auth struct {
	OK      bool              `json:"ok"`
	Error   string            `json:"error"`
	Key     string            `json:"apiKey"`
	Headers map[string]string `json:"headers"`
	BaseURL string            `json:"baseUrl"`
}

func Kind(m Model) string {
	switch {
	case m.Provider == "google-generative-ai" || m.API == "google-generative-ai":
		return "google"
	case m.Provider == "xai" && m.API == "openai-responses":
		return "xai"
	case m.API == "openai-responses" || m.API == "azure-openai-responses" || m.API == "openai-codex-responses":
		return "openai"
	case m.API == "anthropic-messages":
		return "anthropic"
	}
	return "unsupported"
}

type Request struct {
	Model               Model
	Auth                Auth
	Query               string
	URLs                []string
	URLOnly             bool
	Thinking, SessionID string
}

var youtube = regexp.MustCompile(`^(?:https?://)?(?:www\.)?(?:youtube\.com/(?:watch\?v=|embed/)|youtu\.be/)[a-zA-Z0-9_-]{11}`)

func Build(req Request) (string, http.Header, map[string]any, error) {
	m, a := req.Model, req.Auth
	kind := Kind(m)
	if !a.OK {
		return "", nil, nil, fmt.Errorf("resolve provider auth: %s", a.Error)
	}
	if kind == "unsupported" || req.URLOnly && kind != "google" {
		return "", nil, nil, fmt.Errorf("unsupported provider %s (%s)", m.Provider, m.API)
	}
	if len(req.URLs) > 20 || req.URLOnly && len(req.URLs) == 0 {
		return "", nil, nil, errors.New("URL count must be between 1 and 20 for url_context, or 0 and 20 for web_search")
	}
	headers := http.Header{"Content-Type": {"application/json"}, "Accept": {"text/event-stream"}}
	base := strings.TrimRight(m.BaseURL, "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", nil, nil, errors.New("invalid provider base URL")
	}
	if m.Provider == "opencode" || m.Provider == "opencode-go" || u.Hostname() == "opencode.ai" {
		if req.SessionID != "" {
			headers.Set("x-opencode-session", req.SessionID)
			headers.Set("x-opencode-client", "pi")
		}
	}
	for k, v := range m.Headers {
		headers.Set(k, v)
	}
	for k, v := range a.Headers {
		headers.Set(k, v)
	}
	prompt := req.Query
	if len(req.URLs) > 0 {
		prompt += "\n\nAlso analyze these URLs:\n" + strings.Join(req.URLs, "\n")
	}
	var body map[string]any
	switch kind {
	case "google":
		if a.Key != "" {
			headers.Set("x-goog-api-key", a.Key)
		}
		parts := []any{}
		tools := []any{map[string]any{"google_search": map[string]any{}}}
		if len(req.URLs) > 0 {
			tools = append(tools, map[string]any{"url_context": map[string]any{}})
		}
		if req.URLOnly {
			tools = []any{map[string]any{"url_context": map[string]any{}}}
			prompt = req.Query
			var others []string
			for _, raw := range req.URLs {
				if youtube.MatchString(raw) {
					parts = append(parts, map[string]any{"file_data": map[string]any{"file_uri": raw, "mime_type": "video/mp4"}})
				} else {
					others = append(others, raw)
				}
			}
			if len(others) > 0 {
				prompt += "\n\nURLs:\n" + strings.Join(others, "\n")
			}
		}
		parts = append(parts, map[string]any{"text": prompt})
		body = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "tools": tools}
		base += "/models/" + url.PathEscape(m.ID) + ":streamGenerateContent?alt=sse"
	case "anthropic":
		oauth := strings.Contains(a.Key, "sk-ant-oat")
		headers.Set("anthropic-version", "2023-06-01")
		if a.Key != "" {
			if oauth {
				if headers.Get("Authorization") == "" {
					headers.Set("Authorization", "Bearer "+a.Key)
				}
				beta := headers.Get("anthropic-beta")
				if beta != "" {
					beta += ","
				}
				headers.Set("anthropic-beta", beta+"claude-code-20250219,oauth-2025-04-20")
				if headers.Get("user-agent") == "" {
					headers.Set("user-agent", "claude-cli/2.1.75")
				}
				if headers.Get("x-app") == "" {
					headers.Set("x-app", "cli")
				}
			} else if headers.Get("x-api-key") == "" {
				headers.Set("x-api-key", a.Key)
			}
		}
		maxTokens := m.MaxTokens / 3
		if maxTokens == 0 {
			maxTokens = 4096
		}
		body = map[string]any{"model": m.ID, "max_tokens": min(8192, max(1024, maxTokens)), "messages": []any{map[string]any{"role": "user", "content": prompt}}, "tools": []any{map[string]any{"type": "web_search_20250305", "name": "web_search", "max_uses": 10}}, "stream": true}
		if oauth {
			body["system"] = []any{map[string]any{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."}}
		}
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		base += "/messages"
	case "openai", "xai":
		if a.Key != "" && headers.Get("Authorization") == "" {
			headers.Set("Authorization", "Bearer "+a.Key)
		}
		codex := m.API == "openai-codex-responses"
		var input any = prompt
		include := []string{"web_search_call.action.sources", "web_search_call.results"}
		if codex {
			input = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": prompt}}}}
		} else if kind == "xai" {
			input = []any{map[string]any{"role": "user", "content": prompt}}
		}
		if codex || kind == "xai" {
			include = include[:1]
		}
		body = map[string]any{"model": m.ID, "input": input, "tools": []any{map[string]any{"type": "web_search"}}, "include": include, "stream": true, "store": false}
		if kind != "xai" && m.Reasoning && req.Thinking != "" && req.Thinking != "off" && req.Thinking != "none" {
			if effort, enabled := thinkingEffort(m, req.Thinking); enabled {
				body["reasoning"] = map[string]any{"effort": effort}
			}
		}
		if m.Provider == "github-copilot" {
			if a.BaseURL != "" {
				base = strings.TrimRight(a.BaseURL, "/")
			} else {
				for _, part := range strings.Split(a.Key, ";") {
					if strings.HasPrefix(part, "proxy-ep=") {
						host := strings.ToLower(strings.TrimPrefix(part, "proxy-ep="))
						if validCopilotHost(host) {
							base = "https://api." + strings.TrimPrefix(host, "proxy.")
						}
					}
				}
			}
		}
		if codex {
			if !strings.HasPrefix(strings.ToLower(headers.Get("Authorization")), "bearer ") {
				return "", nil, nil, errors.New("no OAuth credential configured for openai-codex")
			}
			if headers.Get("chatgpt-account-id") == "" {
				parts := strings.Split(a.Key, ".")
				if len(parts) != 3 {
					return "", nil, nil, errors.New("failed to extract ChatGPT account ID")
				}
				raw, err := base64.RawURLEncoding.DecodeString(parts[1])
				if err != nil {
					return "", nil, nil, errors.New("failed to extract ChatGPT account ID")
				}
				var claims map[string]any
				if json.Unmarshal(raw, &claims) != nil {
					return "", nil, nil, errors.New("failed to extract ChatGPT account ID")
				}
				id := str(obj(claims["https://api.openai.com/auth"]), "chatgpt_account_id")
				if id == "" {
					return "", nil, nil, errors.New("missing ChatGPT account ID")
				}
				headers.Set("chatgpt-account-id", id)
			}
			if headers.Get("originator") == "" {
				headers.Set("originator", "codex_cli_rs")
			}
			body["instructions"] = "Answer the user's request using web search when needed."
			body["text"] = map[string]any{"verbosity": "low"}
			body["tool_choice"] = "required"
			body["parallel_tool_calls"] = true
			if !strings.HasSuffix(base, "/codex/responses") {
				if !strings.HasSuffix(base, "/codex") {
					base += "/codex"
				}
				base += "/responses"
			}
		} else {
			base += "/responses"
		}
	}
	return base, headers, body, nil
}

var hostLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

func validCopilotHost(host string) bool {
	labels := strings.Split(host, ".")
	if len(host) > 253 || len(labels) < 4 || labels[0] != "proxy" || labels[len(labels)-2] != "githubcopilot" || labels[len(labels)-1] != "com" {
		return false
	}
	for _, label := range labels {
		if len(label) > 63 || !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// Search uses one bounded request. Cancellation interrupts transport and SSE reads.
func Search(ctx context.Context, req Request, update func(string)) (Result, error) {
	endpoint, headers, body, err := Build(req)
	if err != nil {
		return Result{}, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	request.Header = headers
	client := http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("provider search request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return Result{}, fmt.Errorf("provider search HTTP %d: %s", response.StatusCode, redact(string(raw), req.Auth))
	}
	r := stream{Result: Result{Kind: Kind(req.Model)}}
	completed := false
	lastUpdate := time.Time{}
	err = readSSE(response.Body, func(event map[string]any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := boundedEvent(event); err != nil {
			return err
		}
		done, e := r.consume(event)
		if len(r.Results) > 256 || len(r.Citations) > 256 || len(r.Calls) > 256 || len(r.Queries) > 256 || len(r.Text) > 8<<20 {
			return errors.New("provider search exceeds result or text limits")
		}
		completed = completed || done
		if update != nil && time.Since(lastUpdate) >= 50*time.Millisecond && r.Text != "" {
			update(r.Text)
			lastUpdate = time.Now()
		}
		return e
	})
	if err != nil {
		return Result{}, fmt.Errorf("provider search stream: %s", redact(err.Error(), req.Auth))
	}
	if !completed {
		return Result{}, errors.New("provider search stream ended without a terminal response")
	}
	if r.Kind == "google" {
		resolveGoogle(ctx, &r.Result)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	r.finalize()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return r.Result, nil
}
func redact(text string, auth Auth) string {
	if auth.Key != "" {
		text = strings.ReplaceAll(text, auth.Key, "[redacted]")
	}
	for _, value := range auth.Headers {
		if value != "" {
			text = strings.ReplaceAll(text, value, "[redacted]")
		}
	}
	return text
}
func readSSE(reader io.Reader, consume func(map[string]any) error) error {
	limited := io.LimitReader(reader, (32<<20)+1)
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data strings.Builder
	total := 0
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		raw := strings.TrimSpace(data.String())
		data.Reset()
		if raw == "[DONE]" {
			return nil
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return fmt.Errorf("invalid SSE JSON: %w", err)
		}
		return consume(event)
	}
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line) + 1
		if total > 32<<20 {
			return errors.New("SSE response exceeds 32 MiB")
		}
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			if data.Len() > 1<<20 {
				return errors.New("SSE event exceeds 1 MiB")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}
