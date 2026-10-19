package betterfooter

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
	for _, message := range []string{"Codex usage: OAuth token expired", "Codex usage: HTTP 429", "Codex usage: HTTP 500", "Claude usage: HTTP 401"} {
		output.Reset()
		host := &reportHost{hasUI: true}
		x := &extension{}
		cause := errors.New(message)
		err := fmt.Errorf("quota refresh: %w", quota.UsageFailure(cause))
		for range 10 {
			x.report(host, err)
		}
		if len(host.messages) != 0 {
			t.Fatalf("quota popup: %v", host.messages)
		}
		if !strings.Contains(output.String(), message) || !errors.Is(err, cause) {
			t.Fatalf("lost diagnostic or cause: %s", output.String())
		}
	}
}
