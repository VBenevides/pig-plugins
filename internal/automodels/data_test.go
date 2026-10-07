package automodels

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultsPartialSlots(t *testing.T) {
	config := Config{Primary: &Slot{Model: "custom"}, Fallback: &Slot{Provider: "other", Thinking: "off"}}
	primary, fallback := Defaults(config)
	if primary != (Slot{"anthropic", "custom", "high"}) || fallback != (Slot{"other", "gpt-5.5", "off"}) {
		t.Fatalf("%+v %+v", primary, fallback)
	}
	if config.Primary.Provider != "" || config.Fallback.Model != "" {
		t.Fatal("Defaults mutated persisted config")
	}
}
func TestStoreMissingRoundTripAndAgentDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIG_CODING_AGENT_DIR", dir)
	store := Store{}
	if config, err := store.LoadConfig(); err != nil || config != (Config{}) {
		t.Fatalf("missing config: %+v %v", config, err)
	}
	if auth, err := store.LoadAuth(); err != nil || len(auth) != 0 {
		t.Fatalf("missing auth: %+v %v", auth, err)
	}
	if limits, err := store.LoadRateLimits(); err != nil || len(limits) != 0 {
		t.Fatalf("missing limits: %+v %v", limits, err)
	}
	if left, err := store.RateLimitLeft("anthropic", time.Now()); err != nil || left != 0 {
		t.Fatalf("missing cooldown: %s %v", left, err)
	}
	config := Config{Primary: &Slot{Provider: "anthropic", Model: "custom", Thinking: "low"}}
	if err := store.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadConfig(); err != nil || !reflect.DeepEqual(got, config) {
		t.Fatalf("config roundtrip: %+v %v", got, err)
	}
	limits := map[string]RateLimitInfo{"anthropic": {Utilization: "0.75", Status: "allowed", Reset: "1700000200", WeeklyUtilization: "0.1", WeeklyStatus: "warning", WeeklyReset: "1700500000", RequestsLimit: "100", RequestsRemaining: "50", RequestsReset: "2s", TokensLimit: "1000", TokensRemaining: "500", TokensReset: "1m", CapturedAt: 1700000000000}}
	if err := store.SaveRateLimits(limits); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadRateLimits(); err != nil || !reflect.DeepEqual(got, limits) {
		t.Fatalf("limits roundtrip: %+v %v", got, err)
	}
	for _, name := range []string{"auto-model.json", "auto-model-rate-limits.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions: %v %v", name, info, err)
		}
	}
	if err := store.SaveRateLimits(nil); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadRateLimits(); err != nil || len(got) != 0 {
		t.Fatalf("empty limits: %+v %v", got, err)
	}
}
func TestLegacyCacheMigrationAndProviderIsolation(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	path := store.path("claude-quota-cache.json")
	if err := os.WriteFile(path, []byte(`{"rateLimitExpiresAt":1700000060000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	if left, err := store.RateLimitLeft("anthropic", now); err != nil || left != time.Minute {
		t.Fatalf("legacy: %s %v", left, err)
	}
	if left, err := store.RateLimitLeft("openai-codex", now); err != nil || left != 0 {
		t.Fatalf("other provider: %s %v", left, err)
	}
	if err := store.SetRateLimit("openai-codex", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(`"anthropic"`)) || !bytes.Contains(data, []byte(`"openai-codex"`)) {
		t.Fatalf("migration: %s %v", data, err)
	}
	if err := store.ClearRateLimit("anthropic"); err != nil {
		t.Fatal(err)
	}
	if left, err := store.RateLimitLeft("openai-codex", now); err != nil || left != 2*time.Minute {
		t.Fatalf("neighbor preserved: %s %v", left, err)
	}
	if left, err := store.RateLimitLeft("openai-codex", now.Add(2*time.Minute)); err != nil || left != 0 {
		t.Fatalf("expiry boundary: %s %v", left, err)
	}
	if err := store.ClearRateLimit("absent"); err != nil {
		t.Fatal(err)
	}
}
func TestStoreBoundedMalformedAndIOFailures(t *testing.T) {
	for _, name := range []string{"auto-model.json", "auth.json", "auto-model-rate-limits.json", "claude-quota-cache.json"} {
		t.Run(name, func(t *testing.T) {
			store := Store{Dir: t.TempDir()}
			load := func() error {
				switch name {
				case "auto-model.json":
					_, err := store.LoadConfig()
					return err
				case "auth.json":
					_, err := store.LoadAuth()
					return err
				case "auto-model-rate-limits.json":
					_, err := store.LoadRateLimits()
					return err
				default:
					_, err := store.RateLimitLeft("anthropic", time.Now())
					return err
				}
			}
			path := store.path(name)
			for _, content := range []string{`null`, `[]`, `{"bad":`, `{}` + `{}`, strings.Repeat(" ", maxReadBytes+1)} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := load(); err == nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("bad file did not produce contextual error: %v", err)
				}
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := load(); err == nil {
				t.Fatal("directory read must fail")
			}
		})
	}
	store := Store{Dir: t.TempDir()}
	if err := os.WriteFile(store.path("claude-quota-cache.json"), []byte(`{"anthropic":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRateLimit("openai-codex", time.Now()); err == nil {
		t.Fatal("invalid cache must not be overwritten")
	}
	if err := store.ClearRateLimit("anthropic"); err == nil {
		t.Fatal("invalid cache must not be silently cleared")
	}
}
func TestAtomicPersistenceFailurePreservesExistingFile(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	limits := map[string]RateLimitInfo{"anthropic": {Utilization: "0.5"}}
	if err := store.SaveRateLimits(limits); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path("auto-model-rate-limits.json"))
	if err != nil {
		t.Fatal(err)
	}
	limits["anthropic"] = RateLimitInfo{CapturedAt: math.NaN()}
	if err := store.SaveRateLimits(limits); err == nil {
		t.Fatal("invalid JSON number must fail")
	}
	after, err := os.ReadFile(store.path("auto-model-rate-limits.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed encode replaced prior file: %v", err)
	}
	if err := os.Mkdir(store.path("auto-model.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(Config{}); err == nil {
		t.Fatal("failed atomic rename must be observable")
	}
	entries, err := os.ReadDir(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".auto-model.json.tmp-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
	blocked := filepath.Join(t.TempDir(), "not-directory")
	if err := os.WriteFile(blocked, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Store{Dir: blocked}).SaveConfig(Config{}); err == nil {
		t.Fatal("parent directory creation failure must be observable")
	}
}
func TestAuthLoadsOnlyPerAgentFile(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	auth := `{"anthropic":{"type":"oauth","access":"access-secret","refresh":"refresh-secret","expires":9999999999999},"openai-codex":{"type":"oauth","access":"codex","expires":9999999999999,"accountId":"account"},"openai":{"type":"api_key","key":"never-send"}}`
	if err := os.WriteFile(store.path("auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadAuth()
	if err != nil || got["anthropic"].Refresh != "refresh-secret" || got["openai-codex"].AccountID != "account" || got["openai"].Type != "api_key" {
		t.Fatalf("auth decoding failed: %v", err)
	}
	if err := os.WriteFile(store.path("auth.json"), []byte(`{"anthropic":{"expires":"access-secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadAuth(); err == nil || strings.Contains(err.Error(), "access-secret") {
		t.Fatalf("auth errors must not expose values: %v", err)
	}
}
