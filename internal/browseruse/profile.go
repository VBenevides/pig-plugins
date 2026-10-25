package browseruse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
)

// ProfileDir returns the dedicated profile for this browser, not a personal profile.
func ProfileDir(getenv func(string) string, mode Mode) string {
	return filepath.Join(agentdir.Dir(getenv), "browser-use", string(mode))
}

func prepareProfile(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("browser profile path must be absolute")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create persistent browser profile: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect persistent browser profile: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("persistent browser profile must be a directory, not a symlink")
	}
	if err := os.Chmod(path, 0700); err != nil {
		return fmt.Errorf("restrict persistent browser profile permissions: %w", err)
	}
	return nil
}
