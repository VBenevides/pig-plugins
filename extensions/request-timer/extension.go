// Package requesttimerext displays elapsed time while the agent handles a request.
package requesttimerext

import (
	"strings"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/requesttimer"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

const Name = "request-timer"

// Extension returns a timer with one lifecycle per user request, not per model call.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	timer := &requesttimer.Timer{}
	var mu sync.Mutex
	var pending string
	update := func(ctx sdk.Context) func(string) {
		return func(text string) {
			if text == "" {
				ctx.ClearWorkingMessage()
			} else {
				ctx.SetWorkingMessage(text)
			}
		}
	}
	e.OnEvent(sdk.EventAgentStart, func(ctx sdk.Context, _ map[string]any) (any, error) {
		if ctx.HasUI() {
			timer.Start(update(ctx))
		}
		return nil, nil
	})
	e.OnEvent(sdk.EventAgentEnd, func(ctx sdk.Context, _ map[string]any) (any, error) {
		elapsed, running := timer.Stop(update(ctx))
		if !running {
			return nil, nil
		}
		mu.Lock()
		pending = "Elapsed - " + strings.TrimPrefix(requesttimer.Format(elapsed), "Working - ")
		mu.Unlock()
		return nil, nil
	})
	// A passive message appended during agent_end can keep an active turn running. Wait for settlement.
	e.OnEvent(sdk.EventAgentSettled, func(ctx sdk.Context, _ map[string]any) (any, error) {
		mu.Lock()
		text := pending
		pending = ""
		mu.Unlock()
		if text == "" {
			return nil, nil
		}
		trigger := false
		return nil, ctx.SendMessage(Name, text, true, sdk.SendMessageOptions{TriggerTurn: &trigger})
	})
	for _, event := range []string{sdk.EventSessionShutdown, sdk.EventSessionStart} {
		handler := func(ctx sdk.Context, _ map[string]any) (any, error) {
			timer.Stop(update(ctx))
			mu.Lock()
			pending = ""
			mu.Unlock()
			return nil, nil
		}
		if event == sdk.EventSessionStart {
			handler = sdkctx.IgnoreStale(handler)
		}
		e.OnEvent(event, handler)
	}
	return e
}
