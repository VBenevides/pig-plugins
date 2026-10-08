package betterfooter

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// RateWindow is one quota window of the active account.
type RateWindow struct {
	// Scope identifies the window within its provider: "tokens", "zai:3", "codex:primary", ...
	Scope string
	// Percent is the share still available, 0..100.
	Percent float64
	// HasReset reports that ResetSec is meaningful (a reset of 0 means the window has just reset).
	HasReset bool
	// ResetSec is the seconds left at CapturedAt.
	ResetSec   float64
	CapturedAt time.Time
	// WindowMins is the known window size in minutes, or 0 when unknown; it orders 5h before weekly before total.
	WindowMins float64
	// Advisory windows are displayed only; exhausting one does not make the provider's models unusable.
	Advisory bool
}

// Remaining is the seconds until the reset, counted from now.
func (w RateWindow) Remaining(now time.Time) float64 {
	return w.ResetSec - now.Sub(w.CapturedAt).Seconds()
}

// Active reports whether the window still counts: untimed, or timed and not yet reset.
func (w RateWindow) Active(now time.Time) bool { return !w.HasReset || w.Remaining(now) > 0 }

func (w RateWindow) sortKey() float64 {
	switch {
	case w.WindowMins > 0:
		return w.WindowMins
	case w.HasReset:
		return w.ResetSec / 60
	}
	return math.Inf(1)
}

// CompareRateWindows orders windows by size: 5h before weekly before windows without a known reset.
func CompareRateWindows(a, b RateWindow) int {
	ka, kb := a.sortKey(), b.sortKey()
	if ka == kb {
		return 0
	}
	return cmp.Compare(ka, kb)
}

// SortRateWindows sorts in place, keeping the order of equal windows.
func SortRateWindows(windows []RateWindow) { slices.SortStableFunc(windows, CompareRateWindows) }

// UpsertRateWindow replaces the window with the same scope, or appends it, and returns the sorted result.
func UpsertRateWindow(windows []RateWindow, window RateWindow) []RateWindow {
	out := slices.Clone(windows)
	if i := slices.IndexFunc(out, func(w RateWindow) bool { return w.Scope == window.Scope }); i >= 0 {
		out[i] = window
	} else {
		out = append(out, window)
	}
	SortRateWindows(out)
	return out
}
