package picurator

import (
	"context"
	"errors"
	"slices"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/curator"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

var memoryTools = []string{"memory_search", "memory_read"}

func (x *extension) registerTools(e *sdk.Extension) {
	e.RegisterTool(sdk.ToolDefinition{
		Name:        "memory_search",
		Label:       "Memory search",
		Description: searchDescription,
		Exposure:    sdk.ToolExposureDeferred,
		Parameters: sdk.Schema{
			"type": "object",
			"properties": map[string]any{
				"query":  map[string]any{"type": "string"},
				"budget": map[string]any{"type": "number"},
			},
			"required": []string{"query"},
		},
		Execute: x.tool(curator.BuildSearch),
	})
	e.RegisterTool(sdk.ToolDefinition{
		Name:        "memory_read",
		Label:       "Memory evidence",
		Description: readDescription,
		Exposure:    sdk.ToolExposureDeferred,
		Parameters: sdk.Schema{
			"type": "object",
			"properties": map[string]any{
				"ids":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"cursor": map[string]any{"type": "number"},
				"budget": map[string]any{"type": "number"},
			},
			"required": []string{"ids"},
		},
		Execute: x.tool(curator.BuildRead),
	})
}

// exposeTools keeps the memory tools reachable. They are deferred, so the model finds them through tool_search or
// codemode; when neither is active nothing could ever declare them, so they are activated directly.
func (x *extension) exposeTools(ctx sdk.Context) {
	active, err := ctx.GetActiveTools()
	if err != nil {
		x.warn("cannot read the active tools: " + err.Error())
		return
	}
	if slices.Contains(active, "tool_search") || slices.Contains(active, "codemode") {
		return
	}
	ctx.SetActiveTools(slices.Concat(active, memoryTools))
}

type buildFunc func(params map[string]any, engine string) (curator.ToolRequest, error)

// tool returns the executor of a memory tool. It only runs in the consented repository of the session.
func (x *extension) tool(build buildFunc) sdk.ToolFunc {
	return func(ctx sdk.Context, params map[string]any) (any, error) {
		if !x.enabled(ctx) {
			return nil, errors.New("Repository memory is disabled or settings are invalid; use /pi-curator on to enable it")
		}
		sessionID, err := ctx.GetSessionID()
		if err != nil {
			return nil, err
		}
		x.countCall(sessionID, false, true)
		defer x.announce(ctx)
		state := x.current(sessionID, ctx.Cwd())
		if state == nil {
			return nil, errors.New("Repository memory is unavailable: initialize with consent first")
		}
		runCtx, cancel := sdkctx.Request(ctx)
		defer cancel()
		stop := context.AfterFunc(state.bg, cancel)
		defer stop()
		if runCtx.Err() != nil {
			return nil, errors.New("Memory request aborted")
		}
		values, err := x.config.Effective()
		if err != nil {
			return nil, err
		}
		request, err := build(params, values.Engine)
		if err != nil {
			return nil, err
		}
		options := x.boundOptions(state.root)
		// Indexed experiments explicitly refresh only the disposable sidecar, never the authoritative journal.
		if request.Index {
			if _, err := curator.MemoryCommand(runCtx, options, []string{"index"}); err != nil {
				return nil, err
			}
		}
		text, err := curator.MemoryCommand(runCtx, options, request.Args)
		if err != nil {
			return nil, err
		}
		return sdk.ToolResult{Content: text, Details: map[string]any{"untrusted": true}}, nil
	}
}
