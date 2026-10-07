// Package todoext ports pi-todo's session-backed tool and read-only viewer.
package todoext

import (
	"encoding/json"
	"fmt"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"slices"
	"sync"
)

const Name = "todo"

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	var mu sync.Mutex
	state := tasks.New()
	var restoreErr error
	restore := func(ctx sdk.Context, _ map[string]any) (any, error) {
		branch, err := ctx.SessionManager().GetBranch(nil)
		var restored tasks.State
		if err == nil {
			restored, err = tasks.Restore(branch)
		}
		mu.Lock()
		defer mu.Unlock()
		restoreErr = err
		if err == nil {
			state = restored
		}
		return nil, err
	}
	e.OnEvent(sdk.EventSessionStart, restore)
	e.OnEvent(sdk.EventSessionTree, restore)
	e.RegisterTool(sdk.ToolDefinition{Name: "todo", Label: "Todo", Description: "Manage a todo list. Actions: list, add (text), toggle (id), clear", Parameters: tasks.Schema(), ExecutionMode: "sequential", Execute: func(_ sdk.Context, raw map[string]any) (any, error) {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		var p tasks.Params
		if err = json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		if restoreErr != nil {
			return nil, fmt.Errorf("todo state unavailable: %w", restoreErr)
		}
		text, details := state.Apply(p)
		return sdk.ToolResult{Content: text, Details: details}, nil
	}, RenderCall: renderCall, RenderResult: renderResult})
	e.RegisterCommand("todos", sdk.CommandOptions{Description: "Show all todos on the current branch", Handler: func(ctx sdk.Context, _ string) error {
		if !ctx.HasUI() || ctx.Mode() != "tui" {
			ctx.Notify("/todos requires interactive mode", "error")
			return nil
		}
		mu.Lock()
		err := restoreErr
		items := slices.Clone(state.Todos)
		mu.Unlock()
		if err != nil {
			return fmt.Errorf("todo state unavailable: %w", err)
		}
		_, err = ctx.Custom(&viewer{items: items, theme: ctx.UITheme()}, nil)
		return err
	}})
	return e
}
