// Package websearchext exposes grounded native provider search to PiG.
package websearchext

import (
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
	"github.com/VBenevides/pig-plugins/internal/websearch"
	"os"
	"slices"
	"strings"
	"sync"
)

const Name = "web-search"

type extension struct {
	mu              sync.Mutex
	preferred, last map[string]bool
	suppressed      bool
}

func Extension() *sdk.Extension {
	x := &extension{}
	e := sdk.New(Name)
	registerSearchServers(e)
	for _, name := range []string{"web_search", "url_context"} {
		urlOnly := name == "url_context"
		exposure := sdk.ToolExposureDirect
		if !urlOnly {
			exposure = sdk.ToolExposureDeferred
		}
		urls := map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 20}
		required := []string{"query"}
		if urlOnly {
			urls["minItems"] = 1
			required = append(required, "urls")
		}
		e.RegisterTool(sdk.ToolDefinition{Name: name, Exposure: exposure, Label: map[bool]string{true: "URL Context", false: "Web Search"}[urlOnly], Description: map[bool]string{true: "Analyze up to 20 public URLs using Gemini URL Context, including web pages, documents, images and YouTube videos.", false: "Search with Parallel MCP, falling back to the current model's native web search, then Exa MCP. Optionally analyze URLs."}[urlOnly], Parameters: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "urls": urls}, "required": required}, Execute: func(ctx sdk.Context, p map[string]any) (any, error) { return execute(ctx, p, urlOnly) }})
	}
	for _, event := range []string{sdk.EventSessionStart, sdk.EventSessionTree, sdk.EventModelSelect} {
		e.OnEvent(event, func(ctx sdk.Context, _ map[string]any) (any, error) {
			if event == sdk.EventSessionStart {
				active, err := ctx.GetActiveTools()
				if err != nil {
					return nil, err
				}
				if !slices.Contains(active, "tool_search") && !slices.Contains(active, "codemode") {
					ctx.SetActiveTools(append(active, "web_search"))
				}
			}
			return nil, x.sync(ctx)
		})
	}
	return e
}
func set(values []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		out[v] = true
	}
	return out
}
func (x *extension) sync(ctx sdk.Context) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	active, err := ctx.GetActiveTools()
	if err != nil {
		return err
	}
	current := set(active)
	if x.preferred == nil {
		x.preferred = set(active)
	} else {
		for tool := range current {
			if !x.last[tool] {
				x.preferred[tool] = true
			}
		}
		for tool := range x.last {
			if !current[tool] && !(tool == "url_context" && x.suppressed) {
				delete(x.preferred, tool)
			}
		}
	}
	model, err := currentModel(ctx)
	if err != nil {
		return err
	}
	desired := map[string]bool{}
	for tool := range x.preferred {
		desired[tool] = true
	}
	x.suppressed = false
	if websearch.Kind(model) != "google" {
		delete(desired, "url_context")
		x.suppressed = x.preferred["url_context"]
	}
	if !equal(current, desired) {
		tools := make([]string, 0, len(desired))
		for tool := range desired {
			tools = append(tools, tool)
		}
		slices.Sort(tools)
		ctx.SetActiveTools(tools)
	}
	x.last = desired
	return nil
}
func equal(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
func currentModel(ctx sdk.Context) (websearch.Model, error) {
	info, err := ctx.GetModelInfo()
	if err != nil {
		return websearch.Model{}, err
	}
	if info == nil {
		return websearch.Model{}, nil
	}
	raw := ctx.ModelRegistry().Find(info.Provider, info.ID)
	if raw == nil {
		return websearch.Model{}, fmt.Errorf("current model %s/%s not found in registry", info.Provider, info.ID)
	}
	return decodeModel(raw)
}
func decodeModel(raw map[string]any) (websearch.Model, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return websearch.Model{}, err
	}
	var m websearch.Model
	err = json.Unmarshal(data, &m)
	return m, err
}
func fail(err error, details map[string]any) sdk.ToolResult {
	if details == nil {
		details = map[string]any{"error": true}
	} else if details["error"] == nil {
		details["error"] = true
	}
	return sdk.ToolResult{Content: "Error: " + err.Error(), Details: details, IsError: true}
}
func execute(ctx sdk.Context, p map[string]any, urlOnly bool) (any, error) {
	query, ok := p["query"].(string)
	if !ok {
		return fail(errors.New("query must be a string"), nil), nil
	}
	var urls []string
	if raw, present := p["urls"]; present {
		values, ok := raw.([]any)
		if !ok {
			return fail(errors.New("urls must be an array"), nil), nil
		}
		for _, v := range values {
			value, ok := v.(string)
			if !ok {
				return fail(errors.New("each URL must be a string"), nil), nil
			}
			urls = append(urls, value)
		}
	}
	if len(urls) > 20 || urlOnly && len(urls) == 0 {
		return fail(errors.New("URL count is outside the supported range"), nil), nil
	}
	if !urlOnly {
		return searchWithFallback(ctx, query, urls), nil
	}
	return nativeSearch(ctx, query, urls, true), nil
}

func nativeSearch(ctx sdk.Context, query string, urls []string, urlOnly bool) sdk.ToolResult {
	m, err := currentModel(ctx)
	var details map[string]any
	if err != nil {
		return fail(err, details)
	}
	kind := websearch.Kind(m)
	if m.ID == "" || kind == "unsupported" || urlOnly && kind != "google" {
		code := "unsupported_model"
		if m.ID == "" {
			code = "missing_config"
		}
		if urlOnly && kind != "unsupported" {
			code = "unsupported_provider"
		}
		if details == nil {
			details = map[string]any{}
		}
		details["error"] = code
		details["grounded"] = false
		details["providerKind"] = kind
		details["currentModel"] = fmt.Sprintf("%s (%s/%s)", m.ID, m.Provider, m.API)
		return fail(errors.New("current model does not support provider-native search"), details)
	}
	rawModel := ctx.ModelRegistry().Find(m.Provider, m.ID)
	authMap, err := ctx.ModelRegistry().GetApiKeyAndHeaders(rawModel)
	if err != nil {
		return fail(err, nil)
	}
	authBytes, err := json.Marshal(authMap)
	if err != nil {
		return fail(err, nil)
	}
	var auth websearch.Auth
	if err = json.Unmarshal(authBytes, &auth); err != nil {
		return fail(err, nil)
	}
	if auth.OK && auth.Key == "" && !hasAuthHeader(auth.Headers) {
		env := map[string]string{"anthropic": "ANTHROPIC_API_KEY", "openai": "OPENAI_API_KEY", "google": "GEMINI_API_KEY", "google-generative-ai": "GEMINI_API_KEY", "xai": "XAI_API_KEY", "opencode": "OPENCODE_API_KEY", "opencode-go": "OPENCODE_API_KEY"}
		auth.Key = os.Getenv(env[m.Provider])
	}
	sessionID, err := ctx.GetSessionID()
	if err != nil {
		return fail(err, nil)
	}
	thinking, err := ctx.GetThinkingLevel()
	if err != nil {
		return fail(err, nil)
	}
	runCtx, cancel := sdkctx.Request(ctx)
	defer cancel()
	var updateErr error
	result, err := websearch.Search(runCtx, websearch.Request{Model: m, Auth: auth, Query: query, URLs: urls, URLOnly: urlOnly, Thinking: thinking, SessionID: sessionID}, func(text string) {
		if updateErr == nil {
			updateErr = ctx.OnUpdate(sdk.ToolResult{Content: text, Details: map[string]any{"streaming": true}})
			if updateErr != nil {
				cancel()
			}
		}
	})
	if updateErr != nil {
		result := fail(fmt.Errorf("stream tool update: %w", updateErr), nil)
		result.Details.(map[string]any)["stopFallback"] = true
		return result
	}
	if err != nil {
		return fail(err, nil)
	}
	text, out := result.Format(m.ID, urlOnly)
	return sdk.ToolResult{Content: text, Details: out}
}
func hasAuthHeader(headers map[string]string) bool {
	for k, v := range headers {
		if v != "" {
			switch strings.ToLower(k) {
			case "authorization", "x-api-key", "x-goog-api-key":
				return true
			}
		}
	}
	return false
}
