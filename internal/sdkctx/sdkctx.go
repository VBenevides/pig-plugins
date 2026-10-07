// Package sdkctx adapts PiG's request context to the standard library.
package sdkctx

import (
	"context"

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
