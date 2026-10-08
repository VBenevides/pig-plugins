// Package footerstatus mirrors extension status badges for the fused footer.
// The SDK has no status-map getter. Separate extension subprocesses cannot share
// this in-memory mirror, so their badges remain visible only in the host footer.
package footerstatus

import (
	"maps"
	"sync"
)

// Context is the SDK status setter, also usable by isolated tests.
type Context interface {
	SetStatus(key, text string)
}

// Store retains status text unchanged, including theme ANSI sequences.
// Its zero value is ready to use. Snapshot returns an independent map; the
// footer renderer sorts its keys for deterministic display order.
type Store struct {
	setMu sync.Mutex
	mu    sync.RWMutex
	texts map[string]string
}

// Set records a badge and forwards it to the existing host status setter.
// Empty text deletes a badge. Writers are serialized through the host call;
// snapshots remain available while that call is in progress.
func (s *Store) Set(ctx Context, key, text string) {
	s.setMu.Lock()
	defer s.setMu.Unlock()
	s.mu.Lock()
	if text == "" {
		delete(s.texts, key)
	} else {
		if s.texts == nil {
			s.texts = make(map[string]string)
		}
		s.texts[key] = text
	}
	s.mu.Unlock()
	ctx.SetStatus(key, text)
}

// Snapshot returns the current badges without exposing mutable store state.
func (s *Store) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Clone(s.texts)
}

var shared Store

// Set updates the process-wide store shared by fused extensions and the host.
func Set(ctx Context, key, text string) { shared.Set(ctx, key, text) }

// Snapshot returns the process-wide badges for the fused footer renderer.
func Snapshot() map[string]string { return shared.Snapshot() }
