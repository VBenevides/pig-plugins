package betterfooter

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestReportTransientFailureUsesOwnedUI(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, ui := range []bool{true, false} {
		output.Reset()
		ctx := &reportHost{hasUI: ui}
		x := &extension{}
		x.report(ctx, context.DeadlineExceeded)
		if ui && (output.Len() != 0 || len(ctx.messages) != 1) {
			t.Fatalf("raw output on live terminal: %q notifications=%v", output.String(), ctx.messages)
		}
		if !ui && (len(ctx.messages) != 0 || !strings.Contains(output.String(), "context deadline exceeded")) {
			t.Fatalf("headless error lost: %q notifications=%v", output.String(), ctx.messages)
		}
	}
}
