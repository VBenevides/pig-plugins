package guard

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// AccessMode controls which operations the guard checks. It is independent of
// the legacy interactive/auto approval setting and the profile's LANCET switch.
type AccessMode string

const (
	ModeOff    AccessMode = "off"
	ModeLax    AccessMode = "lax"
	ModeStrict AccessMode = "strict"
)

func ParseAccessMode(value string) (AccessMode, bool) {
	mode := AccessMode(value)
	return mode, mode == ModeOff || mode == ModeLax || mode == ModeStrict
}

// SharedModeFile is per user home, never relative to a project or agent profile.
// Passing home explicitly keeps tests from touching the user's real preference.
func SharedModeFile(home string) string {
	return filepath.Join(home, ".pig", "smart-approve-lancet", "mode.json")
}

type ModeState struct {
	Mode     AccessMode `json:"mode"`
	Revision string     `json:"revision"`
}

// ModeStore contains only the shared mode. Writes replace a complete explicit
// choice, never a stale settings snapshot. Concurrent choices are last-rename
// wins; no read/modify/write transaction or persistent lock is needed.
type ModeStore struct{ Path string }

func (s ModeStore) Load() (ModeState, error) {
	file, err := os.Open(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return ModeState{Mode: ModeLax, Revision: "missing"}, nil
	}
	if err != nil {
		return ModeState{}, fmt.Errorf("open shared guard mode %s: %w", s.Path, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return ModeState{}, fmt.Errorf("read shared guard mode %s: %w", s.Path, err)
	}
	if len(data) > 4096 {
		return ModeState{}, fmt.Errorf("shared guard mode %s exceeds 4096 bytes", s.Path)
	}
	var state ModeState
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("decode shared guard mode %s: %w", s.Path, err)
	}
	if _, ok := ParseAccessMode(string(state.Mode)); !ok || len(state.Revision) != 32 {
		return state, fmt.Errorf("invalid mode or revision in %s", s.Path)
	}
	return state, nil
}

// Save publishes a private, synced choice. On filesystems supporting directory
// fsync, success means both the content and rename have been synced. An error
// after rename explicitly reports that the choice may already be visible.
func (s ModeStore) Save(mode AccessMode) (err error) {
	if _, ok := ParseAccessMode(string(mode)); !ok {
		return fmt.Errorf("invalid guard mode %q", mode)
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create shared guard directory: %w", err)
	}
	state := ModeState{Mode: mode}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("create guard mode revision: %w", err)
	}
	state.Revision = fmt.Sprintf("%x", nonce)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode shared guard mode: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".mode-*")
	if err != nil {
		return fmt.Errorf("create shared guard mode: %w", err)
	}
	defer func() {
		closeErr := tmp.Close()
		if errors.Is(closeErr, os.ErrClosed) {
			closeErr = nil
		}
		removeErr := os.Remove(tmp.Name())
		if errors.Is(removeErr, fs.ErrNotExist) {
			removeErr = nil
		}
		err = errors.Join(err, closeErr, removeErr)
	}()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write shared guard mode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync shared guard mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close shared guard mode: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		return fmt.Errorf("publish shared guard mode: %w", err)
	}
	parent, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("guard mode published, durability uncertain: %w", err)
	}
	if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
		return fmt.Errorf("guard mode published, durability uncertain: %w", err)
	}
	return nil
}

// Effective leaves legacy profile preferences untouched. They never silently
// disable every other profile; a user must choose a shared mode explicitly.
func (s ModeStore) Effective(profile string) (ModeState, string, error) {
	state, err := s.Load()
	if err != nil || state.Revision != "missing" {
		return state, "", err
	}
	data, err := os.ReadFile(profile)
	if errors.Is(err, fs.ErrNotExist) {
		return state, "", nil
	}
	if err != nil {
		return state, "", fmt.Errorf("read legacy guard settings: %w", err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return state, "", fmt.Errorf("decode legacy guard settings: %w", err)
	}
	if _, mode := settings["mode"]; mode {
		state.Mode = ModeStrict
	}
	if _, enabled := settings["enabled"]; enabled {
		state.Mode = ModeStrict
	}
	if state.Mode == ModeStrict {
		return state, "Legacy profile mode not imported; using strict. Select /smart-approve-lancet mode off|lax|strict to save a shared choice.", nil
	}
	return state, "", nil
}
