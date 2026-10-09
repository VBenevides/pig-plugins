package automodels

// ClaudeRemaining returns the smaller remaining session/weekly quota.
// Unknown windows are not treated as evidence of available quota.
func ClaudeRemaining(usage *ClaudeUsage) *float64 {
	if usage == nil {
		return nil
	}
	var remaining *float64
	for _, limit := range usage.Limits {
		if (limit.Kind != "session" && limit.Kind != "weekly_all") || limit.Percent == nil || !finite(*limit.Percent) || *limit.Percent < 0 {
			continue
		}
		value := 100 - *limit.Percent
		if remaining == nil || value < *remaining {
			remaining = &value
		}
	}
	return remaining
}

func CodexRemaining(usage *CodexUsage) *float64 {
	if usage == nil || usage.RateLimit == nil {
		return nil
	}
	if usage.RateLimit.LimitReached {
		value := 0.0
		return &value
	}
	var remaining *float64
	for _, window := range []*CodexWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow} {
		if window == nil {
			continue
		}
		if !finite(window.UsedPercent) || window.UsedPercent < 0 {
			return nil
		}
		value := 100 - window.UsedPercent
		if remaining == nil || value < *remaining {
			remaining = &value
		}
	}
	if remaining != nil && !usage.RateLimit.Allowed {
		value := 0.0
		return &value
	}
	return remaining
}
