// Package goalext provides /goal: one objective that the agent keeps working on
// until it is reached or everything left is blocked.
//
//	/goal set <objective>   start; /skill:name tokens inside the objective are expanded
//	/goal check             show the current goal and status
//	/goal resume            continue the current unfinished goal
//	/goal clear             forget the goal and stop continuing
package goalext

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/eventprompt"
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
	"github.com/VBenevides/pig-plugins/internal/goal"
)

// Name is the extension identity and the footer status key.
const Name = "goal"

// maxIdleRuns is how many consecutive runs without a tool call, and without
// goal_finish, end the goal. It stops a model that only chats from looping.
const maxIdleRuns = 3

type extension struct {
	mu    sync.Mutex
	state *goal.State
	// toolCalls and lastStop describe the runs since the last settle.
	toolCalls     int
	lastStop      string
	idleRuns      int
	resumePending bool
}

// Extension returns the extension.
func Extension() *sdk.Extension {
	x := &extension{}
	e := sdk.New(Name)
	e.OnEvent(sdk.EventSessionStart, x.restore)
	e.OnEvent(sdk.EventSessionTree, x.restore)
	e.OnEvent(sdk.EventBeforeAgentStart, x.appendPrompt)
	e.OnEvent(sdk.EventToolExecutionEnd, func(sdk.Context, map[string]any) (any, error) {
		x.mu.Lock()
		x.toolCalls++
		x.mu.Unlock()
		return nil, nil
	})
	e.OnEvent(sdk.EventAgentEnd, x.agentEnd)
	e.OnEvent(sdk.EventAgentSettled, x.settled)
	e.RegisterCommand("goal", sdk.CommandOptions{
		Description: "Work on an objective until it is done (/goal set <objective>, /goal resume, /goal check, /goal clear)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			var items []sdk.AutocompleteItem
			for _, verb := range []string{"set", "resume", "check", "clear"} {
				if strings.HasPrefix(verb, prefix) {
					items = append(items, sdk.AutocompleteItem{Value: verb, Label: verb})
				}
			}
			return items, nil
		},
		Handler: x.command,
	})
	e.RegisterTool(sdk.ToolDefinition{
		Name:        "goal_finish",
		Label:       "Goal finish",
		Description: "End the active /goal. Use outcome \"reached\" when the objective is complete and verified, or \"blocked\" when everything left needs something you cannot supply. Fails when no goal is active.",
		Parameters: sdk.Schema{
			"type": "object",
			"properties": map[string]any{
				"outcome": map[string]any{"type": "string", "enum": []string{"reached", "blocked"}},
				"summary": map[string]any{"type": "string", "description": "What was done, or what blocks the rest."},
			},
			"required":             []string{"outcome", "summary"},
			"additionalProperties": false,
		},
		ExecutionMode: "sequential",
		Execute:       x.finishTool,
	})
	return e
}

func (x *extension) restore(ctx sdk.Context, _ map[string]any) (any, error) {
	branch, err := ctx.SessionManager().GetBranch(nil)
	if err != nil {
		return nil, fmt.Errorf("restore goal branch: %w", err)
	}
	state, warnings := goal.Restore(branch)
	for _, warning := range warnings {
		ctx.Notify(warning, "warning")
	}
	x.mu.Lock()
	x.state, x.toolCalls, x.lastStop, x.idleRuns = state, 0, "", 0
	x.resumePending = false
	x.mu.Unlock()
	announce(ctx, state)
	return nil, nil
}

func announce(ctx footerstatus.Context, state *goal.State) {
	text := ""
	if state != nil && state.Status == goal.Working {
		text = "GOAL"
	}
	footerstatus.Set(ctx, Name, text)
}

func (x *extension) appendPrompt(_ sdk.Context, data map[string]any) (any, error) {
	x.mu.Lock()
	state := x.state
	x.resumePending = false
	x.mu.Unlock()
	if state == nil || state.Status != goal.Working {
		return nil, nil
	}
	base, err := eventprompt.Read(Name, data)
	if err != nil {
		return nil, err
	}
	return map[string]any{"systemPrompt": base + goal.SystemPrompt(state.Objective)}, nil
}

func (x *extension) agentEnd(_ sdk.Context, data map[string]any) (any, error) {
	messages, _ := data["messages"].([]any)
	stop := ""
	for _, raw := range slices.Backward(messages) {
		if message, _ := raw.(map[string]any); message["role"] == "assistant" {
			stop, _ = message["stopReason"].(string)
			break
		}
	}
	x.mu.Lock()
	x.lastStop = stop
	x.mu.Unlock()
	return nil, nil
}

func (x *extension) command(ctx sdk.Context, args string) error {
	verb, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(verb) {
	case "set":
		return x.set(ctx, rest)
	case "resume":
		return x.resume(ctx)
	case "check":
		x.mu.Lock()
		state := x.state
		x.mu.Unlock()
		idle, err := ctx.IsIdle()
		if err != nil {
			return fmt.Errorf("goal check: %w", err)
		}
		ctx.Notify(goal.Describe(state, idle), "info")
	case "clear":
		return x.clear(ctx)
	default:
		ctx.Notify("usage: /goal set <objective> | /goal resume | /goal check | /goal clear", "warning")
	}
	return nil
}

func (x *extension) set(ctx sdk.Context, objective string) error {
	if objective == "" {
		ctx.Notify("usage: /goal set <objective>", "warning")
		return nil
	}
	if len(objective) > goal.MaxObjectiveBytes {
		ctx.Notify(fmt.Sprintf("goal objective is longer than %d bytes", goal.MaxObjectiveBytes), "warning")
		return nil
	}
	blocks, err := expandSkills(ctx, objective)
	if err != nil {
		return fmt.Errorf("goal set: %w", err)
	}
	state := &goal.State{Objective: objective, Status: goal.Working}
	if err := ctx.AppendEntry(goal.EntryType, state); err != nil {
		return fmt.Errorf("goal set: save goal: %w", err)
	}
	x.mu.Lock()
	x.state, x.toolCalls, x.lastStop, x.idleRuns = state, 0, "", 0
	x.mu.Unlock()
	announce(ctx, state)
	if err := ctx.SendUserMessage(goal.InitialMessage(blocks, objective), "followUp"); err != nil {
		x.finish(ctx, goal.Stopped, "could not start: "+err.Error())
		return fmt.Errorf("goal set: start: %w", err)
	}
	ctx.Notify("Goal set. It keeps working until it is reached or blocked; /goal check shows progress.", "info")
	return nil
}

// resumeContext keeps the continuation boundary testable without a running host.
type resumeContext interface {
	footerstatus.Context
	Notify(string, string)
	IsIdle() (bool, error)
	HasPendingMessages() (bool, error)
	AppendEntry(string, any) error
	SendUserMessage(any, string) error
}

func (x *extension) resume(ctx resumeContext) error {
	x.mu.Lock()
	state := x.state
	if state == nil || state.Outcome == goal.Reached {
		x.mu.Unlock()
		ctx.Notify("No unfinished goal to resume. Use /goal set <objective>.", "info")
		return nil
	}
	if x.resumePending {
		x.mu.Unlock()
		ctx.Notify("Goal continuation is already queued.", "info")
		return nil
	}
	x.resumePending = true
	x.mu.Unlock()
	queued := false
	defer func() {
		if !queued {
			x.mu.Lock()
			x.resumePending = false
			x.mu.Unlock()
		}
	}()
	idle, err := ctx.IsIdle()
	if err != nil {
		return fmt.Errorf("goal resume: check idle: %w", err)
	}
	pending, err := ctx.HasPendingMessages()
	if err != nil {
		return fmt.Errorf("goal resume: check queue: %w", err)
	}
	if !idle || pending {
		ctx.Notify("Goal resume requires an idle agent with no queued messages; wait for the current run to finish.", "info")
		return nil
	}
	resumed := *state
	resumed.Status, resumed.Outcome = goal.Working, ""
	if err := ctx.AppendEntry(goal.EntryType, resumed); err != nil {
		return fmt.Errorf("goal resume: save goal: %w", err)
	}
	x.mu.Lock()
	x.state, x.toolCalls, x.lastStop, x.idleRuns = &resumed, 0, "", 0
	x.mu.Unlock()
	announce(ctx, &resumed)
	if err := ctx.SendUserMessage(goal.ContinueMessage, "followUp"); err != nil {
		x.finish(ctx, goal.Stopped, "could not resume: "+err.Error())
		return fmt.Errorf("goal resume: continue: %w", err)
	}
	queued = true
	ctx.Notify("Goal resumed.", "info")
	return nil
}

// expandSkills renders the skills named in objective. An unknown name is left as plain text.
func expandSkills(ctx sdk.Context, objective string) ([]string, error) {
	names := goal.SkillNames(objective)
	if len(names) == 0 {
		return nil, nil
	}
	commands, err := ctx.GetCommands()
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	var blocks []string
	for _, name := range names {
		for _, command := range commands {
			if command.Source != "skill" || strings.TrimPrefix(command.Name, "skill:") != name {
				continue
			}
			block, err := goal.SkillBlock(name, command.SourceInfo.Path)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
			break
		}
	}
	return blocks, nil
}

func (x *extension) clear(ctx sdk.Context) error {
	x.mu.Lock()
	had := x.state != nil
	x.mu.Unlock()
	if !had {
		ctx.Notify("No goal to clear.", "info")
		return nil
	}
	if err := ctx.AppendEntry(goal.EntryType, goal.State{Cleared: true}); err != nil {
		return fmt.Errorf("goal clear: save: %w", err)
	}
	x.mu.Lock()
	x.state = nil
	x.mu.Unlock()
	announce(ctx, nil)
	ctx.Notify("Goal cleared. A run in progress is not interrupted; press Esc to stop it.", "info")
	return nil
}

// finish ends a working goal. Persistence failure is reported but the in-memory
// state still ends, so the loop cannot continue against the user's intent.
func (x *extension) finish(ctx resumeContext, outcome goal.Outcome, summary string) {
	x.mu.Lock()
	if x.state == nil || x.state.Status != goal.Working {
		x.mu.Unlock()
		return
	}
	ended := *x.state
	ended.Status, ended.Outcome, ended.Summary = goal.Finished, outcome, strings.TrimSpace(summary)
	x.state = &ended
	x.mu.Unlock()
	announce(ctx, &ended)
	if err := ctx.AppendEntry(goal.EntryType, ended); err != nil {
		ctx.Notify("goal: could not save the finished state: "+err.Error(), "warning")
	}
	level := "info"
	if outcome != goal.Reached {
		level = "warning"
	}
	ctx.Notify(goal.Describe(&ended, true), level)
}

func (x *extension) finishTool(ctx sdk.Context, params map[string]any) (any, error) {
	outcome, _ := params["outcome"].(string)
	summary, _ := params["summary"].(string)
	summary = strings.TrimSpace(summary)
	if outcome != string(goal.Reached) && outcome != string(goal.Blocked) {
		return nil, errors.New(`goal_finish: outcome must be "reached" or "blocked"`)
	}
	if summary == "" || len(summary) > goal.MaxSummaryBytes {
		return nil, fmt.Errorf("goal_finish: summary must be 1 to %d bytes", goal.MaxSummaryBytes)
	}
	x.mu.Lock()
	active := x.state != nil && x.state.Status == goal.Working
	x.mu.Unlock()
	if !active {
		return nil, errors.New("goal_finish: no goal is active")
	}
	x.finish(ctx, goal.Outcome(outcome), summary)
	return sdk.ToolResult{Content: "Goal finished (" + outcome + "). Reply with a short final note."}, nil
}

// settled runs after a run, with its retries and queued messages, is fully over.
func (x *extension) settled(ctx sdk.Context, _ map[string]any) (any, error) {
	x.mu.Lock()
	state, stop, tools := x.state, x.lastStop, x.toolCalls
	x.mu.Unlock()
	if state == nil || state.Status != goal.Working {
		return nil, nil
	}
	idle, err := ctx.IsIdle()
	if err != nil {
		return nil, fmt.Errorf("goal: check idle: %w", err)
	}
	pending, err := ctx.HasPendingMessages()
	if err != nil {
		return nil, fmt.Errorf("goal: check queue: %w", err)
	}
	if !idle || pending {
		return nil, nil // another run is coming and will settle again
	}
	x.mu.Lock()
	x.toolCalls, x.lastStop = 0, ""
	if tools == 0 {
		x.idleRuns++
	} else {
		x.idleRuns = 0
	}
	idleRuns := x.idleRuns
	x.mu.Unlock()
	switch {
	case stop == "aborted":
		x.finish(ctx, goal.Stopped, "interrupted by you; use /goal resume to continue")
	case stop == "error":
		x.finish(ctx, goal.Stopped, "the model request failed; use /goal resume to continue")
	case idleRuns >= maxIdleRuns:
		x.finish(ctx, goal.Stopped, fmt.Sprintf("no progress: %d runs in a row made no tool call and did not finish the goal", maxIdleRuns))
	default:
		if err := ctx.SendUserMessage(goal.ContinueMessage, "followUp"); err != nil {
			x.finish(ctx, goal.Stopped, "could not continue: "+err.Error())
			return nil, fmt.Errorf("goal: continue: %w", err)
		}
	}
	return nil, nil
}
