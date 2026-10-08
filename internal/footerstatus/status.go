// Package footerstatus mirrors extension status badges for the fused footer.
// The SDK has no status-map getter. Separate extension subprocesses cannot share
// this in-memory mirror, so their badges remain visible only in the host footer.
package footerstatus

import (
	"maps"
	"slices"
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
	// listeners are called after each Set, in no particular order; they must not block.
	listeners map[int]func()
	nextID    int
}

// Subscribe registers fn to run after every badge change and returns its unsubscribe function.
// fn runs while Set holds the writer lock: it must be quick and must not call Set.
func (s *Store) Subscribe(fn func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listeners == nil {
		s.listeners = make(map[int]func())
	}
	id := s.nextID
	s.nextID++
	s.listeners[id] = fn
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
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
	s.mu.RLock()
	notify := slices.Collect(maps.Values(s.listeners))
	s.mu.RUnlock()
	for _, fn := range notify {
		fn()
	}
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

// Subscribe registers fn on the process-wide store; see Store.Subscribe.
func Subscribe(fn func()) func() { return shared.Subscribe(fn) }
