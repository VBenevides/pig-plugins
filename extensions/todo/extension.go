// Package todoext ports pi-todotools' session-backed phased todo tool.
package todoext

import (
	"encoding/json"
	"fmt"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"sync"
)

const Name = "todo"

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	var mu sync.Mutex
	phases := []tasks.Phase{}
	var restoreErr error
	restore := func(ctx sdk.Context, _ map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		branch, err := ctx.SessionManager().GetBranch(nil)
		restoreErr = err
		if err != nil {
			return nil, fmt.Errorf("restore todo branch: %w", err)
		}
		var warnings []string
		phases, warnings = tasks.Restore(branch)
		for _, warning := range warnings {
			ctx.Notify(warning, "warn")
		}
		return nil, syncWidget(ctx, phases)
	}
	e.OnEvent(sdk.EventSessionStart, restore)
	e.OnEvent(sdk.EventSessionTree, restore)
	e.OnEvent(sdk.EventBeforeAgentStart, appendGuidance)
	e.RegisterTool(sdk.ToolDefinition{Name: "todo", Label: "Todo", Description: "Manage phased tasks by exact content, never IDs. Ops: init (list or items), start (task), done/drop (task or phase), rm (task or phase; omit to clear), append (phase, items), view. The earliest open task auto-promotes when no task is in progress.", PromptSnippet: "Track phased tasks with one op-based todo tool; reference tasks by exact content.", PromptGuidelines: []string{"Use one todo operation at a time; batch it with real work.", "Reference tasks and phases by exact content/name; use view when uncertain.", "Mark verified work done immediately and drop obsolete work."}, Parameters: tasks.PhasedSchema(), ExecutionMode: "sequential", Execute: func(ctx sdk.Context, raw map[string]any) (any, error) {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		var op tasks.Operation
		if err = json.Unmarshal(data, &op); err != nil {
			return nil, fmt.Errorf("decode todo parameters: %w", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if restoreErr != nil {
			return nil, fmt.Errorf("todo state unavailable: %w", restoreErr)
		}
		file, err := ctx.GetSessionFile()
		if err != nil {
			return nil, fmt.Errorf("get todo session file: %w", err)
		}
		details, errors, err := tasks.CommitOperation(phases, op, func(entry tasks.StateEntry) error { return ctx.AppendEntry(tasks.StateEntryType, entry) })
		if err != nil {
			return nil, err
		}
		if file == nil {
			details.Storage = "memory"
		}
		if op.Op != "view" && len(errors) == 0 {
			phases = tasks.ClonePhases(details.Phases)
			// The mutation is already durable. A display failure must not make the
			// agent retry a successful append or other non-idempotent operation.
			if err := syncWidget(ctx, phases); err != nil {
				ctx.Notify(err.Error(), "warn")
			}
		}
		return sdk.ToolResult{Content: tasks.FormatSummary(details.Phases, errors, op.Op == "view"), Details: details, IsError: len(errors) > 0}, nil
	}, RenderCall: renderCall, RenderResult: renderResult})
	e.RegisterCommand("todos", sdk.CommandOptions{Description: "Show phased todos on the current branch", Handler: func(ctx sdk.Context, _ string) error {
		if !ctx.HasUI() || ctx.Mode() != "tui" {
			ctx.Notify("/todos requires interactive mode", "error")
			return nil
		}
		mu.Lock()
		err := restoreErr
		snapshot := tasks.ClonePhases(phases)
		mu.Unlock()
		if err != nil {
			return fmt.Errorf("todo state unavailable: %w", err)
		}
		_, err = ctx.Custom(&viewer{phases: snapshot, theme: ctx.UITheme()}, nil)
		return err
	}})
	return e
}
