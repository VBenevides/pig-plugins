package automodels

import (
	"context"
	"fmt"
	"log"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

func (x *extension) accountRemaining(run context.Context, source accountSource, account sdk.OAuthAccount) (*float64, error) {
	_, remaining, err := x.accountQuota(run, source, account)
	return remaining, err
}

type accountSwitcher interface {
	accountSource
	ModelProvider() string
	Model() string
	IsIdle() (bool, error)
	SelectOAuthAccount(string) error
	Notify(string, string)
}

// modelSwitcher isolates host mutation and dialogs from quota policy.
type modelSwitcher interface {
	accountSwitcher
	Confirm(string, string) (bool, error)
	chooseModel(quota.Slot) (bool, error)
	markSelected(bool)
}

type nativeModelSwitcher struct {
	sdk.Context
	x *extension
}

func (c nativeModelSwitcher) chooseModel(slot quota.Slot) (bool, error) {
	return c.x.choose(c.Context, slot)
}
func (c nativeModelSwitcher) markSelected(primary bool) { c.x.selected(c.Context, primary) }

// rotateAccount never crosses provider boundaries or selects unknown quota.
func (x *extension) rotateAccount(ctx accountSwitcher, account sdk.OAuthAccount) (bool, error) {
	return x.rotateAccountWithPolicy(ctx, account, true)
}

// A failed provider response is settled even while its Session is not idle.
// Account selection affects only the next request; completed tools are retained.
func (x *extension) rotateAccountWithPolicy(ctx accountSwitcher, account sdk.OAuthAccount, requireIdle bool) (bool, error) {
	model := ctx.Model()
	primary, fallback := x.slots()
	accounts, err := ctx.OAuthAccounts()
	if err != nil {
		return false, fmt.Errorf("native OAuth account enumeration failed")
	}
	for _, candidate := range accounts {
		if candidate.Provider != account.Provider || candidate.ID == account.ID {
			continue
		}
		remaining, err := x.accountRemaining(x.life, ctx, candidate)
		if err != nil {
			ctx.Notify("auto-models: "+clean(err.Error()), "warning")
			log.Printf("auto-models: %v", err)
			continue
		}
		if remaining == nil || *remaining < 5 {
			continue
		}
		current, found, err := selectedAccount(ctx, account.Provider)
		if err != nil {
			return false, err
		}
		if !found || current.ID != account.ID || ctx.ModelProvider() != account.Provider {
			return false, nil
		}
		currPrimary, currFallback := x.slots()
		if ctx.Model() != model || currPrimary != primary || currFallback != fallback {
			return false, nil
		}
		if x.life.Err() != nil {
			return false, x.life.Err()
		}
		if requireIdle {
			idle, err := ctx.IsIdle()
			if err != nil || !idle {
				return false, err
			}
		}
		if err := ctx.SelectOAuthAccount(candidate.ID); err != nil {
			return false, fmt.Errorf("native OAuth account selection failed")
		}
		if err := x.clearPrimaryCooldown(account.Provider); err != nil {
			return true, err
		}
		ctx.Notify("Switched to another "+clean(account.Provider)+" account with available quota", "info")
		return true, nil
	}
	return false, nil
}

func (x *extension) lowQuota(ctx modelSwitcher, account sdk.OAuthAccount, remaining *float64) error {
	if !x.autoEnabled {
		return nil
	}
	idle, err := ctx.IsIdle()
	if err != nil || !idle {
		return err
	}
	if remaining == nil {
		return nil
	}
	if *remaining >= 5 {
		x.mu.Lock()
		x.lowQuotaAsked = ""
		x.mu.Unlock()
		return nil
	}
	if switched, err := x.rotateAccount(ctx, account); err != nil || switched {
		return err
	}
	primary, fallback := x.slots()
	if ctx.ModelProvider() != primary.Provider || ctx.Model() != primary.Model || primary == fallback {
		return nil
	}
	target, found, err := selectedAccount(ctx, fallback.Provider)
	if err != nil || !found {
		return err
	}
	remaining, err = x.accountRemaining(x.life, ctx, target)
	if err != nil || remaining == nil || *remaining < 5 {
		return err
	}
	key := account.ID + ":" + primary.Provider + "/" + primary.Model + ":" + fallback.Provider + "/" + fallback.Model
	x.mu.Lock()
	asked := x.lowQuotaAsked == key
	x.mu.Unlock()
	if asked {
		return nil
	}
	idle, err = ctx.IsIdle()
	if err != nil || !idle {
		return err
	}
	yes, err := ctx.Confirm("Low model quota", "Less than 5% quota remains. Switch to "+clean(fallback.Model)+"?")
	if err != nil {
		return err
	}
	x.mu.Lock()
	x.lowQuotaAsked = key
	x.mu.Unlock()
	if !yes {
		return nil
	}
	if ctx.ModelProvider() != primary.Provider || ctx.Model() != primary.Model {
		return nil
	}
	current, found, err := selectedAccount(ctx, account.Provider)
	if err != nil || !found || current.ID != account.ID {
		return err
	}
	currPrimary, currFallback := x.slots()
	if currPrimary != primary || currFallback != fallback || x.life.Err() != nil {
		return nil
	}
	currentTarget, found, err := selectedAccount(ctx, fallback.Provider)
	if err != nil || !found || currentTarget.ID != target.ID {
		return err
	}
	idle, err = ctx.IsIdle()
	if err != nil || !idle {
		return err
	}
	if ok, err := ctx.chooseModel(fallback); err != nil {
		return err
	} else if ok {
		ctx.markSelected(false)
	}
	return nil
}
