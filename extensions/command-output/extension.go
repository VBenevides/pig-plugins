// Package commandoutput adds opt-in presentation-only Go command reducers.
package commandoutput

import (
	"fmt"
	"os"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/rtk"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

func Extension() *sdk.Extension {
	e := sdk.New("command-output")
	var mu sync.Mutex
	recovery := rtk.NewRecovery()
	// Keep failed cleanup observable and retain the instance so a later lifecycle
	// event retries its bounded cleanup instead of abandoning owned artifacts.
	reset := func(ctx sdk.Context, _ map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if err := recovery.Close(); err != nil {
			ctx.Notify(fmt.Sprintf("RTK recovery cleanup: %v", err), "warn")
			return nil, nil
		}
		recovery = rtk.NewRecovery()
		return nil, nil
	}
	e.OnSessionStart(reset)
	e.OnSessionShutdown(reset)
	e.OnEvent(sdk.EventSessionTree, reset)
	e.OnToolResult(func(ctx sdk.Context, event map[string]any) (any, error) {
		if event["toolName"] != "bash" || os.Getenv("PI_RTK_BYPASS") == "1" {
			return nil, nil
		}
		settings, err := ctx.GetSettings()
		if err != nil {
			ctx.Notify(fmt.Sprintf("Command output reduction disabled: read settings: %v", err), "warn")
			return nil, nil
		}
		if settings["reduceCommandOutput"] != true {
			return nil, nil
		}
		request, cancel := sdkctx.Request(ctx)
		defer cancel()
		mu.Lock()
		defer mu.Unlock()
		override := rtk.ProcessResult(request, recovery, event, true)
		if override == nil {
			return nil, nil
		}
		return override, nil
	})
	return e
}
