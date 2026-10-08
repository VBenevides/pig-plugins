package betterfooter

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// SettingsFile is the name of the settings file in the agent directory.
const SettingsFile = "better-footer.json"

// Settings are the two toggles of /better-footer.
type Settings struct {
	KeepRecentModel           bool `json:"keepRecentModel"`
	SkipExhaustedScopedModels bool `json:"skipExhaustedScopedModels"`
}

// DefaultSettings turns both features on.
func DefaultSettings() Settings {
	return Settings{KeepRecentModel: true, SkipExhaustedScopedModels: true}
}

// SettingsPath is better-footer.json in the agent directory.
func SettingsPath(getenv func(string) string) string {
	return filepath.Join(agentdir.Dir(getenv), SettingsFile)
}

// LoadSettings reads the settings file. A missing file yields the defaults and no error. A file that cannot be
// read or is not a JSON object yields the defaults together with an error, so the failure is never silent; a
// field that is not a boolean falls back to its default.
func LoadSettings(path string) (Settings, error) {
	settings := DefaultSettings()
	var raw json.RawMessage
	found, err := readJSONCapped(path, &raw)
	if err != nil {
		return settings, fmt.Errorf("could not read %s; using better-footer defaults: %w", path, err)
	}
	if !found {
		return settings, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return settings, fmt.Errorf("could not read %s; using better-footer defaults: not a JSON object", path)
	}
	for name, target := range map[string]*bool{
		"keepRecentModel":           &settings.KeepRecentModel,
		"skipExhaustedScopedModels": &settings.SkipExhaustedScopedModels,
	} {
		var value any
		if json.Unmarshal(fields[name], &value) == nil {
			if b, ok := value.(bool); ok {
				*target = b
			}
		}
	}
	return settings, nil
}

// SaveSettings writes the settings atomically with 0600 permissions: the file is replaced whole or not at all.
func SaveSettings(path string, settings Settings) error {
	if path == "" {
		return errors.New("no settings path")
	}
	return fsutil.WriteJSONFileAtomic(path, settings, 0o600)
}

// Toggle flips one setting by key, "keepRecentModel" or "skipExhaustedScopedModels", and reports whether the key
// is known.
func (s Settings) Toggle(key string) (Settings, bool) {
	switch key {
	case "keepRecentModel":
		s.KeepRecentModel = !s.KeepRecentModel
	case "skipExhaustedScopedModels":
		s.SkipExhaustedScopedModels = !s.SkipExhaustedScopedModels
	default:
		return s, false
	}
	return s, true
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// MenuItems are the two lines of the settings menu, in order: the recent-model line, then the skip line.
func (s Settings) MenuItems() (recent, skip string) {
	return "Keep model and thinking level: " + onOff(s.KeepRecentModel), "Skip exhausted scoped models: " + onOff(s.SkipExhaustedScopedModels)
}
