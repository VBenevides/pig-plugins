package automodels

import (
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func TestGenerationReplayVetoPreventsFallbackContinuation(t *testing.T) {
	for _, message := range []map[string]any{
		{"role": "assistant", "provider": "synthetic", "replayProhibited": true, "stopReason": "error", "errorMessage": "HTTP 429 rate limit"},
	} {
		x := &extension{autoEnabled: true, retryPending: true}
		if _, err := x.messageEnd(sdk.Context{}, map[string]any{"message": message}); err != nil {
			t.Fatal(err)
		}
		if x.retryPending || !x.retryUsed {
			t.Fatal("forbidden recovery remained armed")
		}
	}
}
