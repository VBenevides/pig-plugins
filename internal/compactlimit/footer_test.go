package compactlimit

import (
	"testing"
	"time"
)

func TestFooterLimitShowsOnlyAUserLimitBelowTheCapAndRefreshes(t *testing.T) {
	agent := t.TempDir()
	getenv := func(key string) string {
		if key == "PIG_CODING_AGENT_DIR" {
			return agent
		}
		return ""
	}
	now := time.Unix(1000, 0)
	footer := &Footer{Getenv: getenv, TTL: time.Second, Now: func() time.Time { return now }}
	file := File(getenv)
	const window = 1_000_000

	if got := footer.Limit(window, ""); got != 0 {
		t.Fatalf("no settings: %d", got)
	}
	if err := Save(file, Limits{Absolute: ptrInt(250_000)}); err != nil {
		t.Fatal(err)
	}
	if got := footer.Limit(window, ""); got != 0 {
		t.Fatalf("a change inside the TTL must not hit the disk: %d", got)
	}
	now = now.Add(2 * time.Second)
	if got := footer.Limit(window, ""); got != 250_000 {
		t.Fatalf("absolute: %d", got)
	}
	if err := Save(file, Limits{Absolute: ptrInt(250_000), Relative: ptrFloat(10)}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if got := footer.Limit(window, ""); got != 100_000 {
		t.Fatalf("min(relative, absolute): %d", got)
	}
	if got := footer.Limit(200_000, ""); got != 20_000 {
		t.Fatalf("a model switch must recompute from the new window: %d", got)
	}
	if got := footer.Limit(0, ""); got != 0 {
		t.Fatalf("unknown window: %d", got)
	}
	if err := Save(file, Limits{Absolute: ptrInt(2_000_000)}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if got := footer.Limit(window, ""); got != 0 {
		t.Fatalf("a limit above PiG's own cap is clamped and must not be shown: %d", got)
	}
	if err := Save(file, Limits{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if got := footer.Limit(window, ""); got != 0 {
		t.Fatalf("cleared: %d", got)
	}
}
