package automodels

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
