package automodels

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

func TestQuotaFailuresDoNotNotifyButRemainInDebugDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, message := range []string{"native OAuth credentials unavailable or expired", "Codex usage: OAuth token expired", "Codex usage: HTTP 429", "Codex usage: HTTP 500", "Claude usage: HTTP 401"} {
		output.Reset()
		source := &fakeSwitcher{}
		x := &extension{}
		cause := errors.New(message)
		err := fmt.Errorf("quota refresh: %w", quota.UsageFailure(cause))
		for range 10 {
			x.report(source, err)
		}
		if len(source.notices) != 0 {
			t.Fatalf("quota popup: %v", source.notices)
		}
		if !strings.Contains(output.String(), message) || !errors.Is(err, cause) {
			t.Fatalf("lost diagnostic or cause: %s", output.String())
		}
	}
}
