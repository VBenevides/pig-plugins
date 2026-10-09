package betterfooter

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
	bf "github.com/VBenevides/pig-plugins/internal/betterfooter"
)

// Native IDs select credentials locally; accountId in the auth map selects the
// ChatGPT tenant. Neither is a provider-global quota cache key.
type accountList interface {
	OAuthAccounts() ([]sdk.OAuthAccount, error)
}

type accountSource interface {
	accountList
	OAuthAccountAuth(string) (map[string]any, error)
}

type accountSnapshot []sdk.OAuthAccount

func (s accountSnapshot) OAuthAccounts() ([]sdk.OAuthAccount, error) { return s, nil }

func nativeQuotaKey(source accountList, provider, base string) (string, error) {
	if base == "" {
		return "", nil
	}
	accounts, err := source.OAuthAccounts()
	if err != nil {
		return "", errors.New("native OAuth account enumeration failed")
	}
	found := false
	for _, account := range accounts {
		if account.Provider != provider {
			continue
		}
		found = true
		if account.Active && account.ID != "" {
			return bf.NativeQuotaKey(base, account.ID), nil
		}
	}
	if found {
		return "", errors.New("native OAuth selected account unavailable")
	}
	return base, nil
}

func (x *extension) fetchNative(ctx context.Context, key string) ([]bf.RateWindow, string, bool, error) {
	id, native := bf.NativeQuotaAccount(key)
	provider := bf.QuotaSource(key)
	if !native || id == "" || (provider != bf.CodexProvider && provider != bf.ClaudeProvider) {
		return nil, "", false, nil
	}
	x.mu.Lock()
	source, gen := x.ctx, x.generation
	x.mu.Unlock()
	var windows []bf.RateWindow
	var err error
	if provider == bf.ClaudeProvider {
		windows, err = fetchNativeClaude(ctx, source, x.client, key, time.Now())
	} else {
		windows, err = fetchNativeCodex(ctx, source, x.client, key, time.Now())
	}
	x.mu.Lock()
	current := gen == x.generation
	x.mu.Unlock()
	if !current && err == nil {
		err = errors.New("native quota session changed")
	}
	return windows, "", true, err
}

// nativeCredential reads the selected native account's credential and returns a check that fails once the
// selection has moved to another account, so a slow read cannot be published under the wrong key.
func nativeCredential(source accountSource, key, provider string) (quota.AuthEntry, func() error, error) {
	id, native := bf.NativeQuotaAccount(key)
	if !native || id == "" || bf.QuotaSource(key) != provider {
		return quota.AuthEntry{}, nil, fmt.Errorf("native %s quota account unavailable", provider)
	}
	check := func() error {
		selected, err := nativeQuotaKey(source, provider, provider)
		if err != nil {
			return err
		}
		if selected != key {
			return errors.New("native OAuth selected account changed")
		}
		return nil
	}
	if err := check(); err != nil {
		return quota.AuthEntry{}, nil, err
	}
	auth, err := source.OAuthAccountAuth(id)
	if err != nil {
		return quota.AuthEntry{}, nil, errors.New("native account credential lookup failed")
	}
	return quota.AuthEntry{Type: text(auth, "type"), Access: text(auth, "access"), AccountID: text(auth, "accountId"), Expires: number(auth, "expires")}, check, nil
}

// fetchNativeClaude maps the selected Claude account's usage limits to footer windows.
func fetchNativeClaude(ctx context.Context, source accountSource, client quota.Client, key string, now time.Time) ([]bf.RateWindow, error) {
	entry, check, err := nativeCredential(source, key, bf.ClaudeProvider)
	if err != nil {
		return nil, err
	}
	usage, err := client.FetchClaude(ctx, entry)
	if err != nil {
		return nil, err // Client sanitizes HTTP/transport errors and never returns bodies.
	}
	if err := check(); err != nil {
		return nil, err
	}
	if usage == nil {
		return nil, errors.New("native Claude quota windows unavailable")
	}
	var windows []bf.RateWindow
	for _, limit := range usage.Limits {
		if limit.Percent == nil || math.IsNaN(*limit.Percent) || math.IsInf(*limit.Percent, 0) {
			continue
		}
		window := bf.RateWindow{Percent: math.Min(100, math.Max(0, 100-*limit.Percent)), CapturedAt: now}
		switch limit.Kind {
		case "session":
			window.Scope, window.WindowMins = "claude:session", 300
		case "weekly_all":
			window.Scope, window.WindowMins = "claude:weekly", 7*24*60
		default:
			continue // Model-scoped limits bind only some models; they are shown by /usage.
		}
		if reset, err := time.Parse(time.RFC3339, limit.ResetsAt); err == nil {
			window.HasReset, window.ResetSec = true, math.Max(0, reset.Sub(now).Seconds())
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 {
		return nil, errors.New("native Claude quota windows unavailable")
	}
	bf.SortRateWindows(windows)
	return windows, nil
}

func fetchNativeCodex(ctx context.Context, source accountSource, client quota.Client, key string, now time.Time) ([]bf.RateWindow, error) {
	entry, check, err := nativeCredential(source, key, bf.CodexProvider)
	if err != nil {
		return nil, err
	}
	usage, err := client.FetchCodex(ctx, entry)
	if err != nil {
		return nil, err // Client sanitizes HTTP/transport errors and never returns bodies.
	}
	if err := check(); err != nil {
		return nil, err
	}
	if usage == nil || usage.RateLimit == nil {
		return nil, errors.New("native Codex quota windows unavailable")
	}
	var windows []bf.RateWindow
	add := func(name string, window *quota.CodexWindow) {
		if window == nil || math.IsNaN(window.UsedPercent) || math.IsInf(window.UsedPercent, 0) || window.UsedPercent < 0 || window.UsedPercent > 100 {
			return
		}
		reset := window.ResetAfterSeconds
		if window.ResetAt > 0 {
			reset = window.ResetAt - float64(now.UnixMilli())/1000
		}
		if math.IsNaN(reset) || math.IsInf(reset, 0) || math.IsNaN(window.LimitWindowSeconds) || math.IsInf(window.LimitWindowSeconds, 0) {
			return
		}
		windows = append(windows, bf.RateWindow{Scope: "codex:" + name, Percent: 100 - window.UsedPercent, HasReset: window.ResetAt > 0 || reset > 0, ResetSec: math.Max(0, reset), CapturedAt: now, WindowMins: math.Max(0, window.LimitWindowSeconds/60), Advisory: usage.RateLimit.Allowed && !usage.RateLimit.LimitReached})
	}
	add("primary", usage.RateLimit.PrimaryWindow)
	add("secondary", usage.RateLimit.SecondaryWindow)
	if len(windows) == 0 {
		return nil, errors.New("native Codex quota windows unavailable")
	}
	bf.SortRateWindows(windows)
	return windows, nil
}

// Called with x.mu held. A switch discards old publications, even if the user
// later returns to that account while a previous read/request is still running.
func (x *extension) setQuotaKeyLocked(key string) {
	if x.state.QuotaKey == key {
		return
	}
	for _, changed := range []string{x.state.QuotaKey, key} {
		if _, native := bf.NativeQuotaAccount(changed); native {
			x.store.Invalidate(changed)
		}
	}
	x.quotaGeneration++
	x.state.QuotaKey = key
	x.state.Quota, _ = x.store.Get(key)
	x.lastPoll = time.Time{}
}
