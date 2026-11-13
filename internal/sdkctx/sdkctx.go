// Package sdkctx adapts PiG's request context to the standard library.
package sdkctx

import (
	"context"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

// Request returns a context that ends when the request behind ctx is cancelled, and a function that releases it.
// The caller must call the function when the work is done.
func Request(ctx sdk.Context) (context.Context, context.CancelFunc) {
	runCtx, cancel := context.WithCancel(context.Background())
	if done := ctx.Done(); done != nil {
		go func() {
			select {
			case <-done:
				cancel()
			case <-runCtx.Done():
			}
		}()
	}
	return runCtx, cancel
}

// staleMarker appears in the panic message the SDK raises for a ctx used after session replacement or reload.
const staleMarker = "ctx is stale"

// IgnoreStale wraps a session_start handler. When the session is replaced while the handler runs (for example by
// startup auto-resume), the SDK panics on the next ctx use and prints a stack trace to the terminal. The new session
// runs its own session_start, so the abandoned one ends quietly. Other panics are raised again.
func IgnoreStale(handler func(sdk.Context, map[string]any) (any, error)) func(sdk.Context, map[string]any) (any, error) {
	return func(ctx sdk.Context, data map[string]any) (result any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if !isStale(recovered) {
					panic(recovered)
				}
				result, err = nil, nil
			}
		}()
		return handler(ctx, data)
	}
}

func isStale(recovered any) bool {
	switch value := recovered.(type) {
	case error:
		return strings.Contains(value.Error(), staleMarker)
	case string:
		return strings.Contains(value, staleMarker)
	}
	return false
}

// OnSessionStart registers a session_start handler that ends quietly when its context goes stale.
func OnSessionStart(e *sdk.Extension, handler func(sdk.Context, map[string]any) (any, error)) {
	e.OnSessionStart(IgnoreStale(handler))
}
