package automodels

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

// primaryAvailable queries the selected native account, never another account
// or the Codex CLI. Unsupported providers and missing credentials are unknown.
func (x *extension) primaryAvailable(run context.Context, source accountSource, provider string) (*bool, error) {
	if provider != "anthropic" && provider != "openai-codex" {
		return nil, nil
	}
	account, found, err := selectedAccount(source, provider)
	if err != nil || !found {
		return nil, err
	}
	entry, err := accountAuth(source, account.ID)
	if err != nil {
		return nil, err
	}
	if !validOAuth(entry, time.Now()) {
		return nil, fmt.Errorf("primary quota: native OAuth credentials unavailable or expired")
	}
	var available *bool
	if provider == "anthropic" {
		usage, err := x.client.FetchClaude(run, entry)
		if err != nil {
			return nil, err
		}
		available = quota.ClaudeAvailable(usage)
	} else {
		usage, err := x.client.FetchCodex(run, entry)
		if err != nil {
			return nil, err
		}
		available = quota.CodexAvailable(usage)
	}
	current, found, err := selectedAccount(source, provider)
	if err != nil {
		return nil, err
	}
	if !found || current.ID != account.ID {
		return nil, fmt.Errorf("primary quota: selected account changed during check")
	}
	return available, nil
}

func (x *extension) clearPrimaryCooldown(provider string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	previous, existed := x.rates[provider]
	delete(x.rates, provider)
	if err := x.store.SaveRateLimits(x.rates); err != nil {
		if existed {
			x.rates[provider] = previous
		}
		return err
	}
	return x.store.ClearRateLimit(provider)
}

const primaryQuotaInterval = 10 * time.Minute

// Recovery runs only on the configured fallback, with automatic selection
// enabled and the agent idle. Checks are retried at most once every ten minutes.
func (x *extension) recoverPrimary(ctx sdk.Context) error {
	primary, fallback := x.slots()
	if !x.autoEnabled || primary == fallback || ctx.ModelProvider() != fallback.Provider || ctx.Model() != fallback.Model {
		return nil
	}
	idle, err := ctx.IsIdle()
	if err != nil || !idle {
		return err
	}
	x.mu.Lock()
	if time.Since(x.primaryQuotaAt) < primaryQuotaInterval {
		x.mu.Unlock()
		return nil
	}
	x.primaryQuotaAt = time.Now()
	x.mu.Unlock()
	account, found, err := selectedAccount(ctx, primary.Provider)
	if err != nil || !found {
		return err
	}
	remaining, err := x.accountRemaining(x.life, ctx, account)
	if err != nil || remaining == nil || *remaining < 5 {
		return err
	}
	currentAccount, found, err := selectedAccount(ctx, primary.Provider)
	if err != nil || !found || currentAccount.ID != account.ID {
		return err
	}
	// A manual model/configuration change or a new request during the network
	// check must not be overwritten by its result.
	currentPrimary, currentFallback := x.slots()
	if x.life.Err() != nil || currentPrimary != primary || currentFallback != fallback || ctx.ModelProvider() != fallback.Provider || ctx.Model() != fallback.Model {
		return nil
	}
	idle, err = ctx.IsIdle()
	if err != nil || !idle {
		return err
	}
	if err := x.clearPrimaryCooldown(primary.Provider); err != nil {
		return err
	}
	if ok, err := x.choose(ctx, primary); err != nil {
		return err
	} else if ok {
		x.selected(ctx, true)
		ctx.Notify("Primary quota available, switched back to "+clean(primary.Model), "info")
	}
	return nil
}
