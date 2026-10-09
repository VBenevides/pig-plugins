package automodels

import (
	"time"

	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

// startupCooldown revalidates cached cooldowns against the selected account's
// live quota. Unknown or failed quota checks must not imply recovery.
func (x *extension) startupCooldown(source accountSource, primary quota.Slot, now time.Time) (time.Duration, error) {
	left, err := x.cooldown(primary.Provider, now)
	if err != nil {
		return 0, err
	}
	if passive := quota.PassiveCooldown(x.rate(primary.Provider), now); passive > left {
		left = passive
	}
	if left <= 0 {
		return 0, nil
	}
	account, found, err := selectedAccount(source, primary.Provider)
	if err != nil || !found {
		return left, err
	}
	remaining, err := x.accountRemaining(x.life, source, account)
	if err != nil || remaining == nil || *remaining <= 0 {
		return left, err
	}
	current, found, err := selectedAccount(source, primary.Provider)
	if err != nil || !found || current.ID != account.ID {
		return left, err
	}
	currPrimary, _ := x.slots()
	if currPrimary != primary || x.life.Err() != nil {
		return left, x.life.Err()
	}
	if err := x.clearPrimaryCooldown(primary.Provider); err != nil {
		return left, err
	}
	return 0, nil
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
