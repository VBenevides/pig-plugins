package automodels

import (
	"testing"
	"time"

	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

func TestRecoveryClearsPersistedCooldownAndSnapshot(t *testing.T) {
	store := quota.Store{Dir: t.TempDir()}
	if err := store.SetRateLimit("openai-codex", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	x := &extension{store: store, rates: map[string]quota.RateLimitInfo{
		"openai-codex": {TokensRemaining: "0", TokensReset: "1h"},
		"anthropic":    {Status: "allowed"},
	}}
	if err := x.clearPrimaryCooldown("openai-codex"); err != nil {
		t.Fatal(err)
	}
	left, err := store.RateLimitLeft("openai-codex", time.Now())
	if err != nil || left != 0 {
		t.Fatal(left, err)
	}
	rates, err := store.LoadRateLimits()
	if err != nil || len(rates) != 1 || rates["anthropic"].Status != "allowed" {
		t.Fatal(rates, err)
	}
	if err := x.clearPrimaryCooldown("openai-codex"); err != nil {
		t.Fatal("repeated recovery must be idempotent", err)
	}
}
