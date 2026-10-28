package contextprune

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// FileName is the per-user settings file in PiG's agent directory.
const FileName = "context-prune.json"

// File returns the settings file path.
func File(getenv func(string) string) string { return agentdir.File(getenv, FileName) }

// Enabled reads the switch. A missing file means off, because pruning is opt-in. A damaged file returns an error
// and false, so a bad file never turns pruning on.
func Enabled(file string) (bool, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", file, err)
	}
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return false, fmt.Errorf("%s: %w", file, err)
	}
	value, present := object["enabled"]
	if !present {
		return false, nil
	}
	enabled, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf(`%s: "enabled" must be true or false`, file)
	}
	return enabled, nil
}

// SetEnabled writes the switch and keeps every other key of an existing file. It refuses to replace a file that
// is not a JSON object. The write is atomic and the mode is 0600.
func SetEnabled(file string, enabled bool) error {
	object := map[string]any{}
	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		if object, err = fsutil.DecodeObject(data); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("read %s: %w", file, err)
	}
	object["enabled"] = enabled
	return fsutil.WriteJSONFileAtomic(file, object, 0o600)
}
