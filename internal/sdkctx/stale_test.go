package sdkctx

import (
	"errors"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func TestIgnoreStaleSwallowsOnlyStaleContextPanics(t *testing.T) {
	stale := IgnoreStale(func(sdk.Context, map[string]any) (any, error) {
		panic(errors.New("registerTool: This extension ctx is stale after session replacement or reload."))
	})
	if result, err := stale(sdk.Context{}, nil); result != nil || err != nil {
		t.Fatalf("stale panic not ignored: %v %v", result, err)
	}
	other := IgnoreStale(func(sdk.Context, map[string]any) (any, error) { panic("boom") })
	func() {
		defer func() {
			if recover() != "boom" {
				t.Fatal("unrelated panic was hidden")
			}
		}()
		_, _ = other(sdk.Context{}, nil)
	}()
	want := errors.New("plain")
	passthrough := IgnoreStale(func(sdk.Context, map[string]any) (any, error) { return 7, want })
	if result, err := passthrough(sdk.Context{}, nil); result != 7 || !errors.Is(err, want) {
		t.Fatalf("result changed: %v %v", result, err)
	}
}
