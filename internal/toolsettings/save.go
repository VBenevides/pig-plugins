package toolsettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SaveIfUnchanged prevents another settings dialog from silently replacing choices saved since this dialog
// opened. The lock is exclusive across processes. An interrupted writer leaves a lock for manual review.
func SaveIfUnchanged(path string, previous, next Config) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	lock := path + ".lock"
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("lock tool settings (another writer or interrupted save; inspect %s): %w", lock, err)
	}
	defer func() { err = errors.Join(err, f.Close(), os.Remove(lock)) }()
	current, err := Load(path)
	if err != nil {
		return err
	}
	before, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	now, err := json.Marshal(current)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, now) {
		return fmt.Errorf("tool settings changed in another session; reopen /tool-settings before saving")
	}
	return Save(path, next)
}
