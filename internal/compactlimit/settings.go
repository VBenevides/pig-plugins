package compactlimit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// FileName is the per-user settings file in PiG's agent directory.
const FileName = "compact-settings.json"

// File returns the settings file path.
func File(getenv func(string) string) string { return agentdir.File(getenv, FileName) }

// Load reads the limits. A missing file means no limit. A damaged file returns an error and no limits: a limit
// that cannot be read is reported, never guessed, and PiG's own threshold keeps working.
func Load(file string) (Limits, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Limits{}, nil
	}
	if err != nil {
		return Limits{}, fmt.Errorf("read %s: %w", file, err)
	}
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return Limits{}, fmt.Errorf("%s: %w", file, err)
	}
	return limitsFromObject(file, object)
}

func limitsFromObject(file string, object map[string]any) (Limits, error) {
	var limits Limits
	raw, present := object["limit"]
	if !present {
		return limits, nil
	}
	section, ok := raw.(map[string]any)
	if !ok {
		return Limits{}, fmt.Errorf(`%s: "limit" must be an object`, file)
	}
	if value, present := section["relative"]; present {
		number, ok := value.(json.Number)
		if !ok {
			return Limits{}, fmt.Errorf(`%s: limit.relative must be a number`, file)
		}
		percent, err := ParsePercent(number.String())
		if err != nil {
			return Limits{}, fmt.Errorf("%s: limit.relative: %w", file, err)
		}
		limits.Relative = &percent
	}
	if value, present := section["absolute"]; present {
		number, ok := value.(json.Number)
		if !ok {
			return Limits{}, fmt.Errorf(`%s: limit.absolute must be a number`, file)
		}
		tokens, err := ParseTokens(number.String())
		if err != nil {
			return Limits{}, fmt.Errorf("%s: limit.absolute: %w", file, err)
		}
		limits.Absolute = &tokens
	}
	return limits, nil
}

// Save writes the limits, keeping every other key of an existing file. It refuses to replace a file that is not
// a JSON object. The write is atomic and the mode is 0600.
func Save(file string, limits Limits) error {
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
	section := map[string]any{}
	if limits.Relative != nil {
		section["relative"] = *limits.Relative
	}
	if limits.Absolute != nil {
		section["absolute"] = *limits.Absolute
	}
	if len(section) == 0 {
		delete(object, "limit")
	} else {
		object["limit"] = section
	}
	return fsutil.WriteJSONFileAtomic(file, object, 0o600)
}

// ReserveTokens returns PiG's compaction.reserveTokens: the project's .pig/settings.json over the global
// settings.json over the default. An unreadable or invalid value is ignored and the default is used, because the
// reserve only moves the cap and PiG applies its own validation.
func ReserveTokens(getenv func(string) string, cwd string) int {
	reserve := DefaultReserveTokens
	files := []string{agentdir.File(getenv, "settings.json")}
	if cwd != "" {
		files = append(files, filepath.Join(cwd, ".pig", "settings.json"))
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var settings struct {
			Compaction struct {
				ReserveTokens *int `json:"reserveTokens"`
			} `json:"compaction"`
		}
		if json.Unmarshal(data, &settings) != nil || settings.Compaction.ReserveTokens == nil || *settings.Compaction.ReserveTokens < 0 {
			continue
		}
		reserve = *settings.Compaction.ReserveTokens
	}
	return reserve
}
