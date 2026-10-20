// Package smartapprovelancet gates mutations and reviewed local reads before
// tool execution. A shared per-user access mode selects off, lax or strict;
// profile-local LANCET scoring is independent. Unknown-scope model advice is
// display-only and can never approve a call.
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
	settings := guard.Settings{Mode: guard.Interactive}
	service := guard.NewLocal(agentdir.Dir(getenv), getenv, lancet.InstallOptions{})
	gate := guard.NewGate(settings, service)
	gate.UseAllowlist(guard.NewAllowlist(guard.AllowlistFile(getenv)))
	home, homeErr := os.UserHomeDir()
	modePath := ""
	if homeErr == nil {
		modePath = guard.SharedModeFile(home)
	}
	controller := &guard.Controller{Gate: gate, Lancet: service, Settings: settingsFile, Modes: &guard.ModeStore{Path: modePath}}

	announce := func(ctx sdk.Context) { footerstatus.Set(ctx, Name, controller.Chip()) }

	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		note, err := controller.RefreshShared(nil)
		if err != nil {
			note = "Cannot load guard mode: " + err.Error()
		}
		if note != "" {
			fmt.Fprintf(os.Stderr, "%s%s\n", guard.Prefix, note)
			ctx.Notify(guard.Prefix+note, "warning")
		}
		announce(ctx)
		return nil, nil
	})
	e.OnEvent(sdk.EventBeforeAgentStart, func(ctx sdk.Context, _ map[string]any) (any, error) {
		if _, err := controller.RefreshShared(func() { announce(ctx) }); err != nil {
			announce(ctx)
			ctx.Notify(guard.Prefix+"Guard settings unavailable: "+err.Error(), "warning")
		}
		return nil, nil
	})
	e.OnSessionShutdown(func(_ sdk.Context, _ map[string]any) (any, error) {
		if err := service.Release(); err != nil {
			fmt.Fprintf(os.Stderr, "%scannot release LANCET at shutdown: %v\n", guard.Prefix, err)
		}
		return nil, nil
	})

	e.RegisterCommand(guard.CommandName, sdk.CommandOptions{
		Description: "Set shared guard mode (mode off|lax|strict), inspect status and manage local LANCET scoring (lancet ...)",
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
