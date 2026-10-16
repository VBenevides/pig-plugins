package automodels

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

type reportingSwitcher struct {
	fakeSwitcher
	hasUI bool
}

func (s *reportingSwitcher) HasUI() bool { return s.hasUI }

func TestReportTransientFailureUsesOwnedUI(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, ui := range []bool{true, false} {
		output.Reset()
		x := &extension{}
		ctx := &reportingSwitcher{hasUI: ui}
		x.report(ctx, context.DeadlineExceeded)
		if ui && (output.Len() != 0 || len(ctx.notices) != 1) {
			t.Fatalf("raw output on live terminal: %q notifications=%v", output.String(), ctx.notices)
		}
		if !ui && (len(ctx.notices) != 0 || !strings.Contains(output.String(), "context deadline exceeded")) {
			t.Fatalf("headless error lost: %q notifications=%v", output.String(), ctx.notices)
		}
	}
}
