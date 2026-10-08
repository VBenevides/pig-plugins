package betterfooter

import (
	"context"
	"errors"
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
	_, native := bf.NativeQuotaAccount(key)
	if !native || bf.QuotaSource(key) != bf.CodexProvider {
		return nil, "", false, nil
	}
	x.mu.Lock()
	source, gen := x.ctx, x.generation
	x.mu.Unlock()
	windows, err := fetchNativeCodex(ctx, source, x.client, key, time.Now())
	x.mu.Lock()
	current := gen == x.generation
	x.mu.Unlock()
	if !current && err == nil {
		err = errors.New("native quota session changed")
	}
	return windows, "", true, err
}

func fetchNativeCodex(ctx context.Context, source accountSource, client quota.Client, key string, now time.Time) ([]bf.RateWindow, error) {
	id, native := bf.NativeQuotaAccount(key)
	if !native || id == "" || bf.QuotaSource(key) != bf.CodexProvider {
		return nil, errors.New("native Codex quota account unavailable")
	}
	check := func() error {
		selected, err := nativeQuotaKey(source, bf.CodexProvider, bf.CodexProvider)
		if err != nil {
			return err
		}
		if selected != key {
			return errors.New("native OAuth selected account changed")
		}
		return nil
	}
	if err := check(); err != nil {
		return nil, err
	}
	auth, err := source.OAuthAccountAuth(id)
	if err != nil {
		return nil, errors.New("native account credential lookup failed")
	}
	entry := quota.AuthEntry{Type: text(auth, "type"), Access: text(auth, "access"), AccountID: text(auth, "accountId"), Expires: number(auth, "expires")}
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
