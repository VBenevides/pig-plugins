package cursordirect

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerationReplayVetoOnDirectFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frames []byte
		status int
	}{
		{"HTTP 503", nil, 503},
		{"Connect terminal", terminal(`{"error":{"code":"unavailable"}}`), 200},
		{"partial then failure", join(frame(update(1, str(1, "partial"))), terminal(`{"error":{"code":"unavailable"}}`)), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if tc.status != 200 {
					w.WriteHeader(tc.status)
					return
				}
				writeFrames(w, tc.frames)
			})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			message, _ := result(t, client, ctx, prompt())
			if requests.Load() != 1 || message["replayProhibited"] != true || message["stopReason"] != "error" {
				t.Fatalf("requests=%d message=%#v", requests.Load(), message)
			}
		})
	}
}
