package compactlimit

import (
	"sync"
	"time"
)

// Footer resolves the effective user limit for display. Reading the settings on every redraw would touch the disk
// many times a second, so the result is reused for a short time; a /compact-settings change shows within ttl.
type Footer struct {
	Getenv func(string) string
	TTL    time.Duration
	Now    func() time.Time

	mu       sync.Mutex
	at       time.Time
	window   int
	cwd      string
	cached   int
	haveBase bool
}

// Limit returns the effective limit in tokens when a user-set limit is what lowers it below PiG's own threshold,
// and 0 otherwise (nothing set, set but clamped, unknown window, or an unreadable file). Failures give 0: the
// footer then shows its normal counter, and the command reports the problem.
func (f *Footer) Limit(window int, cwd string) int {
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.haveBase && window == f.window && cwd == f.cwd && now().Sub(f.at) < f.TTL {
		return f.cached
	}
	f.haveBase, f.at, f.window, f.cwd, f.cached = true, now(), window, cwd, 0
	limits, err := Load(File(f.Getenv))
	if err != nil || !limits.IsSet() {
		return 0
	}
	if result := Effective(limits, window, ReserveTokens(f.Getenv, cwd)); !result.Native && result.UserSet {
		f.cached = result.Limit
	}
	return f.cached
}
