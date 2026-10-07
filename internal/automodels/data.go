// Package automodels implements the pi-auto-models@0.1.14 data and quota layer.
// Unlike upstream, persistence failures are returned instead of silently ignored.
package automodels

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

const maxReadBytes = 1 << 20

// Slot fields can be omitted independently; Defaults resolves each field.
type Slot struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}
type Config struct {
	Primary  *Slot `json:"primary,omitempty"`
	Fallback *Slot `json:"fallback,omitempty"`
}

func Defaults(config Config) (Slot, Slot) {
	resolve := func(slot *Slot, defaults Slot) Slot {
		if slot != nil {
			if slot.Provider != "" {
				defaults.Provider = slot.Provider
			}
			if slot.Model != "" {
				defaults.Model = slot.Model
			}
			if slot.Thinking != "" {
				defaults.Thinking = slot.Thinking
			}
		}
		return defaults
	}
	return resolve(config.Primary, Slot{"anthropic", "claude-opus-4-6", "high"}),
		resolve(config.Fallback, Slot{"openai-codex", "gpt-5.5", "high"})
}

type AuthEntry struct {
	Type      string  `json:"type"`
	Access    string  `json:"access"`
	Refresh   string  `json:"refresh"`
	AccountID string  `json:"accountId,omitempty"`
	Expires   float64 `json:"expires"` // Unix milliseconds.
}

type CodexWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds float64 `json:"limit_window_seconds"`
	ResetAfterSeconds  float64 `json:"reset_after_seconds"`
	ResetAt            float64 `json:"reset_at"`
}
type CodexRateLimit struct {
	Allowed         bool         `json:"allowed"`
	LimitReached    bool         `json:"limit_reached"`
	PrimaryWindow   *CodexWindow `json:"primary_window,omitempty"`
	SecondaryWindow *CodexWindow `json:"secondary_window,omitempty"`
}
type CodexAdditionalRateLimit struct {
	LimitName      string          `json:"limit_name,omitempty"`
	MeteredFeature string          `json:"metered_feature,omitempty"`
	RateLimit      *CodexRateLimit `json:"rate_limit,omitempty"`
}
type CodexUsage struct {
	PlanType             string                     `json:"plan_type,omitempty"`
	RateLimit            *CodexRateLimit            `json:"rate_limit,omitempty"`
	AdditionalRateLimits []CodexAdditionalRateLimit `json:"additional_rate_limits,omitempty"`
}
type ClaudeModel struct {
	DisplayName *string `json:"display_name,omitempty"`
}
type ClaudeScope struct {
	Model *ClaudeModel `json:"model,omitempty"`
}
type ClaudeLimit struct {
	Kind     string       `json:"kind,omitempty"`
	Percent  *float64     `json:"percent,omitempty"`
	Severity string       `json:"severity,omitempty"`
	ResetsAt string       `json:"resets_at,omitempty"`
	Scope    *ClaudeScope `json:"scope,omitempty"`
}
type ClaudeUsage struct {
	Limits []ClaudeLimit `json:"limits,omitempty"`
}

type RateLimitInfo struct {
	Utilization       string  `json:"utilization,omitempty"`
	Status            string  `json:"status,omitempty"`
	Reset             string  `json:"reset,omitempty"`
	WeeklyUtilization string  `json:"weeklyUtilization,omitempty"`
	WeeklyStatus      string  `json:"weeklyStatus,omitempty"`
	WeeklyReset       string  `json:"weeklyReset,omitempty"`
	RequestsLimit     string  `json:"requestsLimit,omitempty"`
	RequestsRemaining string  `json:"requestsRemaining,omitempty"`
	RequestsReset     string  `json:"requestsReset,omitempty"`
	TokensLimit       string  `json:"tokensLimit,omitempty"`
	TokensRemaining   string  `json:"tokensRemaining,omitempty"`
	TokensReset       string  `json:"tokensReset,omitempty"`
	CapturedAt        float64 `json:"capturedAt,omitempty"` // Unix milliseconds.
}

// Store uses Dir, or the standard per-agent directory when Dir is empty.
// Read-modify-write cooldown operations require serialization by the caller.
type Store struct{ Dir string }

func (s Store) path(name string) string {
	dir := s.Dir
	if dir == "" {
		dir = agentdir.Dir(os.Getenv)
	}
	return filepath.Join(dir, name)
}

// readObject bounds local reads and never includes file contents in errors.
func readObject(path string, value any) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxReadBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return fmt.Errorf("read %s: %w", path, readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	if len(data) > maxReadBytes {
		return fmt.Errorf("read %s: file exceeds 1 MiB", path)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("decode %s: invalid JSON object", path)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode %s: invalid field types", path)
	}
	return nil
}

func (s Store) LoadConfig() (Config, error) {
	var config Config
	if err := readObject(s.path("auto-model.json"), &config); err != nil {
		return Config{}, err
	}
	return config, nil
}
func (s Store) SaveConfig(config Config) error {
	return fsutil.WriteJSONFileAtomic(s.path("auto-model.json"), config, 0o600)
}
func (s Store) LoadRateLimits() (map[string]RateLimitInfo, error) {
	limits := map[string]RateLimitInfo{}
	if err := readObject(s.path("auto-model-rate-limits.json"), &limits); err != nil {
		return nil, err
	}
	return limits, nil
}
func (s Store) SaveRateLimits(limits map[string]RateLimitInfo) error {
	if limits == nil {
		limits = map[string]RateLimitInfo{}
	}
	return fsutil.WriteJSONFileAtomic(s.path("auto-model-rate-limits.json"), limits, 0o600)
}
func (s Store) LoadAuth() (map[string]AuthEntry, error) {
	auth := map[string]AuthEntry{}
	if err := readObject(s.path("auth.json"), &auth); err != nil {
		return nil, err
	}
	return auth, nil
}

type providerQuota struct {
	Expires float64 `json:"rateLimitExpiresAt"`
}

func (s Store) loadCache() (map[string]providerQuota, error) {
	var raw map[string]json.RawMessage
	path := s.path("claude-quota-cache.json")
	if err := readObject(path, &raw); err != nil {
		return nil, err
	}
	cache := map[string]providerQuota{}
	if raw == nil {
		return cache, nil
	}
	if legacy, ok := raw["rateLimitExpiresAt"]; ok {
		var expires float64
		if json.Unmarshal(legacy, &expires) == nil && string(legacy) != "null" {
			cache["anthropic"] = providerQuota{Expires: expires}
			return cache, nil
		}
	}
	for provider, data := range raw {
		var entry *providerQuota
		if json.Unmarshal(data, &entry) != nil || entry == nil {
			return nil, fmt.Errorf("decode %s: invalid cooldown entry", path)
		}
		cache[provider] = *entry
	}
	return cache, nil
}
func (s Store) RateLimitLeft(provider string, now time.Time) (time.Duration, error) {
	cache, err := s.loadCache()
	if err != nil {
		return 0, err
	}
	return positiveDuration((cache[provider].Expires - float64(now.UnixMilli())) * float64(time.Millisecond)), nil
}
func (s Store) SetRateLimit(provider string, expires time.Time) error {
	cache, err := s.loadCache()
	if err != nil {
		return err
	}
	cache[provider] = providerQuota{Expires: float64(expires.UnixMilli())}
	return fsutil.WriteJSONFileAtomic(s.path("claude-quota-cache.json"), cache, 0o600)
}
func (s Store) ClearRateLimit(provider string) error {
	cache, err := s.loadCache()
	if err != nil {
		return err
	}
	delete(cache, provider)
	return fsutil.WriteJSONFileAtomic(s.path("claude-quota-cache.json"), cache, 0o600)
}

// Saturate duration conversion rather than wrapping a large provider timestamp.
func positiveDuration(nanoseconds float64) time.Duration {
	if math.IsNaN(nanoseconds) || nanoseconds <= 0 {
		return 0
	}
	if nanoseconds >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(nanoseconds)
}

// HasExplicitModelFlag follows upstream's end-of-options handling.
func HasExplicitModelFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--model" || len(arg) >= 8 && arg[:8] == "--model=" {
			return true
		}
	}
	return false
}
