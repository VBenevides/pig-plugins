package automodels

import (
	"crypto/sha256"
	"time"
)

const warningInterval = 5 * time.Minute
const warningHistoryLimit = 64

type warningState struct {
	at      time.Time
	repeats uint64
}

// admitWarning coalesces identical warnings from polling and candidate rotation.
// Store only bounded fingerprints, not credential or provider response payloads.
// A periodic reminder includes the suppressed count, so failures stay observable.
func (x *extension) admitWarning(message string, now time.Time) (bool, uint64) {
	key := sha256.Sum256([]byte(message))
	x.mu.Lock()
	defer x.mu.Unlock()
	state, found := x.warnings[key]
	if found && now.Sub(state.at) < warningInterval {
		if state.repeats < ^uint64(0) {
			state.repeats++
		}
		x.warnings[key] = state
		return false, 0
	}
	if x.warnings == nil {
		x.warnings = make(map[[32]byte]warningState)
	}
	if !found && len(x.warnings) >= warningHistoryLimit {
		var oldest [32]byte
		var oldestAt time.Time
		for key, value := range x.warnings {
			if oldestAt.IsZero() || value.at.Before(oldestAt) {
				oldest, oldestAt = key, value.at
			}
		}
		delete(x.warnings, oldest)
	}
	x.warnings[key] = warningState{at: now}
	return true, state.repeats
}
