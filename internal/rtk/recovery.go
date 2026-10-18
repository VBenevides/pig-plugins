// Package rtk provides native, presentation-only command output reduction.
// It never executes commands or changes native tool status/structured values.
package rtk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	MaxInputBytes    = 2 * 1024 * 1024
	MaxVisibleBytes  = 50 * 1024
	maxSnapshots     = 32
	maxSnapshotBytes = 16 * 1024 * 1024
)

type snapshot struct {
	path string
	size int
}

// Recovery owns a private temporary directory for this process/session. Oldest
// snapshots are evicted under fixed count/byte limits. Close removes only files
// this instance created, never arbitrary contents of a supplied directory.
type Recovery struct {
	mu          sync.Mutex
	parent, dir string
	files       []snapshot
	bytes       int
	closed      bool
}

func NewRecovery() *Recovery { return &Recovery{} }

// Save publishes a complete captured result. It cannot recover bytes already
// truncated by the host; callers must preserve the host's truncation notices.
func (r *Recovery) Save(ctx context.Context, raw string) (path string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.closed {
		return "", errors.New("output recovery is closed")
	}
	if len(raw) > MaxInputBytes {
		return "", fmt.Errorf("output recovery exceeds %d-byte capture limit", MaxInputBytes)
	}
	if r.dir == "" {
		r.dir, err = os.MkdirTemp(r.parent, "pig-rtk-")
		if err != nil {
			return "", fmt.Errorf("create output recovery directory: %w", err)
		}
	}
	file, err := os.CreateTemp(r.dir, "output-*.txt")
	if err != nil {
		return "", fmt.Errorf("create output recovery file: %w", err)
	}
	path = file.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(path))
			path = ""
		}
	}()
	_, writeErr := file.WriteString(raw)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	err = errors.Join(writeErr, file.Close(), ctx.Err())
	if err != nil {
		return path, fmt.Errorf("persist captured output: %w", err)
	}
	for len(r.files) >= maxSnapshots || r.bytes+len(raw) > maxSnapshotBytes {
		oldest := r.files[0]
		if removeErr := os.Remove(oldest.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return path, fmt.Errorf("evict output recovery %s: %w", filepath.Base(oldest.path), removeErr)
		}
		r.files = r.files[1:]
		r.bytes -= oldest.size
	}
	r.files = append(r.files, snapshot{path, len(raw)})
	r.bytes += len(raw)
	return path, nil
}

func (r *Recovery) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	var failures []error
	remaining := r.files[:0]
	for _, file := range r.files {
		if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove output recovery %s: %w", filepath.Base(file.path), err))
			remaining = append(remaining, file)
		}
	}
	r.files = remaining
	r.bytes = 0
	for _, file := range remaining {
		r.bytes += file.size
	}
	if r.dir != "" && len(remaining) == 0 {
		if err := os.Remove(r.dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove output recovery directory: %w", err))
		} else {
			r.dir = ""
		}
	}
	return errors.Join(failures...)
}

// Compaction describes presentation changes only, not host capture truncation.
type Compaction struct {
	InputBytes    int    `json:"inputBytes"`
	OutputBytes   int    `json:"outputBytes"`
	RawOutputPath string `json:"rawOutputPath"`
}

// Present never discards source text unless recovery was successfully saved.
// On failure it returns the original capture, with an error for the caller to
// surface alongside the original execution status. It never rewrites history.
func (r *Recovery) Present(ctx context.Context, raw, reduced string) (string, *Compaction, error) {
	if len(reduced) >= len(raw) {
		return raw, nil, nil
	}
	if len(reduced) > MaxVisibleBytes {
		return raw, nil, errors.New("reduced output exceeds visible budget; retaining host output")
	}
	path, err := r.Save(ctx, raw)
	if err != nil {
		return raw, nil, err
	}
	text := fmt.Sprintf("[RTK: captured output compressed; original %d bytes. Raw capture: %s (bounded session retention; may already be host-truncated)]\n%s", len(raw), path, reduced)
	if len(text) > MaxVisibleBytes {
		return raw, nil, errors.New("recovery notice and reduced output exceed visible budget; retaining host output")
	}
	if len(text) >= len(raw) {
		return raw, nil, nil
	}
	return text, &Compaction{len(raw), len(text), path}, nil
}
