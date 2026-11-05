package cursor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

// runner is the bridge surface the provider needs. Tests replace it.
type runner interface {
	RunTurn(ctx context.Context, key string, model cursorbridge.Selection, tools []cursorbridge.ToolDef, prompt string, emit func(cursorbridge.Event) error) (cursorbridge.Outcome, error)
	Models(ctx context.Context, key string) ([]cursorbridge.Model, error)
	Me(ctx context.Context, key string) (cursorbridge.User, error)
	Close() error
}

type processRunner struct{ *cursorbridge.Process }

func (p processRunner) Models(ctx context.Context, key string) ([]cursorbridge.Model, error) {
	return p.Client.Models(ctx, key)
}
func (p processRunner) Me(ctx context.Context, key string) (cursorbridge.User, error) {
	return p.Client.Me(ctx, key)
}

// manager keeps at most one bridge process per account. A process is shared by
// nothing else: its environment and workspace belong to one API key, and turns
// on it are serialized. Idle processes are closed.
type manager struct {
	start func(ctx context.Context, key string) (runner, error)
	idle  time.Duration

	mu      sync.Mutex
	entries map[string]*entry
	closed  bool
}

type entry struct {
	ready chan struct{}
	r     runner
	err   error
	users int
	timer *time.Timer
}

func newManager(start func(context.Context, string) (runner, error), idle time.Duration) *manager {
	return &manager{start: start, idle: idle, entries: map[string]*entry{}}
}

// Account identity for map keys is a hash, so keys never sit in memory as map keys.
func accountHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// acquire returns the account's runner and a release function. The caller must
// call release exactly once.
func (m *manager) acquire(ctx context.Context, key string) (runner, func(), error) {
	id := accountHash(key)
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, nil, errors.New("Cursor bridge manager is closed")
		}
		e := m.entries[id]
		creator := e == nil
		if creator {
			e = &entry{ready: make(chan struct{})}
			m.entries[id] = e
		}
		m.mu.Unlock()
		if creator {
			// Start outside the lock so another account is never blocked by this one.
			e.r, e.err = m.start(ctx, key)
			if e.err != nil {
				m.mu.Lock()
				delete(m.entries, id)
				m.mu.Unlock()
			}
			close(e.ready)
		} else {
			select {
			case <-e.ready:
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
		if e.err != nil {
			if creator {
				return nil, nil, e.err
			}
			continue // the starter failed; try to start again under this caller's context
		}
		m.mu.Lock()
		// The entry may have been evicted or closed between readiness and now.
		if m.closed || m.entries[id] != e {
			m.mu.Unlock()
			continue
		}
		e.users++
		if e.timer != nil {
			e.timer.Stop()
			e.timer = nil
		}
		m.mu.Unlock()
		var once sync.Once
		return e.r, func() { once.Do(func() { m.release(id, e) }) }, nil
	}
}

func (m *manager) release(id string, e *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.users--
	if e.users == 0 && !m.closed && m.entries[id] == e {
		e.timer = time.AfterFunc(m.idle, func() { m.evict(id, e) })
	}
}

func (m *manager) evict(id string, e *entry) {
	m.mu.Lock()
	if e.users != 0 || m.entries[id] != e {
		m.mu.Unlock()
		return
	}
	delete(m.entries, id)
	m.mu.Unlock()
	_ = e.r.Close() // Process.Close is idempotent; a failure here has no caller to report to.
}

// closeAll stops every process and refuses new ones. It reports every failure.
func (m *manager) closeAll() error {
	m.mu.Lock()
	m.closed = true
	entries := m.entries
	m.entries = map[string]*entry{}
	m.mu.Unlock()
	var failures []error
	for _, e := range entries {
		<-e.ready
		if e.timer != nil {
			e.timer.Stop()
		}
		if e.err == nil && e.r != nil {
			if err := e.r.Close(); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

// drop closes an account's idle process, for example after logout. A process
// with active turns is left alone; its turns finish under their own context.
func (m *manager) drop(key string) {
	id := accountHash(key)
	m.mu.Lock()
	e := m.entries[id]
	m.mu.Unlock()
	if e != nil {
		m.evict(id, e)
	}
}
