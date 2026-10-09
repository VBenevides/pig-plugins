// Package smartapprovelancet is the PiG extension "smart-approve-lancet": the safety layer that gates bash, write
// and edit through tool_call. It registers no tool. The policy lives in internal/guard.
//
//   - hard-blocked bash behaviors (rm -rf /, fork bomb, curl|sh, ...) are always blocked;
//   - with LANCET on (/smart-approve-lancet lancet on), every other bash command is scored by the local model: risky
//     is blocked, review needs confirmation, not_flagged continues, and an unavailable model blocks;
//   - other dangerous bash behaviors and protected write/edit paths need confirmation, and are blocked when no UI
//     exists or the mode is auto. Turning the guard off bypasses all these checks.
//
// Unknown-scope confirmation dialogs use the current session LLM for advisory risk and target descriptions.
package smartapprovelancet

import (
	"fmt"
	"os"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
	"github.com/VBenevides/pig-plugins/internal/guard"
	"github.com/VBenevides/pig-plugins/internal/lancet"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

// Name is the extension identity and the footer status key.
const Name = "smart-approve-lancet"

// Extension returns the extension. Settings and the model are read from the agent directory of the environment.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	getenv := os.Getenv
	settingsFile := guard.SettingsFile(getenv)
	settings := guard.LoadSettings(settingsFile)
	service := guard.NewLocal(agentdir.Dir(getenv), getenv, lancet.InstallOptions{})
	gate := guard.NewGate(settings, service)
	gate.UseAllowlist(guard.NewAllowlist(guard.AllowlistFile(getenv)))
	controller := &guard.Controller{Gate: gate, Lancet: service, Settings: settingsFile}

	announce := func(ctx sdk.Context) { footerstatus.Set(ctx, Name, controller.Chip()) }

	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		if settings.Problem != "" {
			fmt.Fprintf(os.Stderr, "%s%s\n", guard.Prefix, settings.Problem)
			ctx.Notify(guard.Prefix+settings.Problem, "warning")
		}
		announce(ctx)
		return nil, nil
	})
	e.OnSessionShutdown(func(_ sdk.Context, _ map[string]any) (any, error) {
		if err := service.Release(); err != nil {
			fmt.Fprintf(os.Stderr, "%scannot release LANCET at shutdown: %v\n", guard.Prefix, err)
		}
		return nil, nil
	})

	e.RegisterCommand(guard.CommandName, sdk.CommandOptions{
		Description: "Turn the guard on/off, set its approval mode (interactive|auto), and manage local LANCET scoring (lancet ...)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			var items []sdk.AutocompleteItem
			for _, c := range guard.Completions(prefix) {
				items = append(items, sdk.AutocompleteItem{Value: c.Value, Label: c.Label})
			}
			return items, nil
		},
		Handler: func(ctx sdk.Context, args string) error {
			runCtx, cancel := sdkctx.Request(ctx)
			defer cancel()
			controller.Handle(runCtx, args, ctx.Notify, func() { announce(ctx) })
			return nil
		},
	})

	e.OnEvent(sdk.EventToolCall, func(ctx sdk.Context, data map[string]any) (any, error) {
		tool, _ := data["toolName"].(string)
		if !guard.Gated(tool) {
			return nil, nil
		}
		input, _ := data["input"].(map[string]any)
		runCtx, cancel := sdkctx.Request(ctx)
		defer cancel()
		decision := controller.Check(runCtx, guard.Call{
			Tool:    tool,
			Input:   input,
			Cwd:     ctx.Cwd(),
			HasUI:   ctx.HasUI(),
			Confirm: ctx.Confirm,
			Select:  ctx.Select,
			ExplainUnknown: func(command, cwd string) (guard.ScopeExplanation, error) {
				return explainUnknown(ctx, command, cwd)
			},
		}, func() { announce(ctx) })
		if decision.Block {
			return map[string]any{"block": true, "reason": decision.Reason}, nil
		}
		return nil, nil
	})
	return e
}
