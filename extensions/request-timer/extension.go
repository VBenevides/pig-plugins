// Package requesttimerext displays elapsed time while the agent handles a request.
package requesttimerext

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/requesttimer"
)

const Name = "request-timer"

// Extension returns a timer with one lifecycle per user request, not per model call.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	timer := &requesttimer.Timer{}
	update := func(ctx sdk.Context) func(string) {
		return func(text string) { ctx.SetStatus(Name, text) }
	}
	e.OnEvent(sdk.EventAgentStart, func(ctx sdk.Context, _ map[string]any) (any, error) {
		if ctx.HasUI() {
			timer.Start(update(ctx))
		}
		return nil, nil
	})
	for _, event := range []string{sdk.EventAgentEnd, sdk.EventSessionShutdown, sdk.EventSessionStart} {
		e.OnEvent(event, func(ctx sdk.Context, _ map[string]any) (any, error) {
			timer.Stop(update(ctx))
			return nil, nil
		})
	}
	return e
}
