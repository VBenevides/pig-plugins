package guard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// Mode is the approval mode.
type Mode string

const (
	// Interactive asks through a dialog before a dangerous command or a protected path.
	Interactive Mode = "interactive"
	// Strict (auto mode) blocks them without asking.
	Strict Mode = "auto"
)

// ParseMode returns the mode named by s.
func ParseMode(s string) (Mode, bool) {
	switch Mode(s) {
	case "strict": // Accept settings written before auto was renamed.
		return Strict, true
	case Interactive, Strict:
		return Mode(s), true
	}
	return "", false
}

// SettingsFileName is the settings file in the agent directory. Its keys are the ones smart-approve-lancet uses.
const SettingsFileName = "smart-approve-lancet.json"

// SettingsFile returns the settings file path.
func SettingsFile(getenv func(string) string) string {
	return agentdir.File(getenv, SettingsFileName)
}

// Settings are the values read from the settings file.
type Settings struct {
	Mode     Mode
	Lancet   bool
	Disabled bool
	// Problem is set when the file could not be used. The mode is then Strict and Lancet is off: a damaged setting
	// must never loosen the guard.
	Problem string
}

// LoadSettings reads the settings file. A missing file means interactive with LANCET off.
func LoadSettings(file string) Settings {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Settings{Mode: Interactive}
	}
	if err != nil {
		return Settings{Mode: Strict, Problem: fmt.Sprintf("%s is unreadable: %v", file, err)}
	}
	settings, err := parseSettings(data)
	if err != nil {
		return Settings{Mode: Strict, Problem: fmt.Sprintf("%s is invalid (%v); using auto", file, err)}
	}
	return settings
}

func parseSettings(data []byte) (Settings, error) {
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return Settings{}, err
	}
	settings := Settings{Mode: Interactive}
	if raw, present := object["enabled"]; present {
		enabled, ok := raw.(bool)
		if !ok {
			return Settings{}, errors.New("enabled must be a boolean")
		}
		settings.Disabled = !enabled
	}
	if raw, present := object["mode"]; present {
		name, _ := raw.(string)
		mode, ok := ParseMode(name)
		if !ok {
			return Settings{}, errors.New("mode must be interactive or auto")
		}
		settings.Mode = mode
	}
	if raw, present := object["lancet"]; present {
		lancet, ok := raw.(map[string]any)
		if !ok {
			return Settings{}, errors.New(`lancet must be an object like {"enabled": true}`)
		}
		if enabled, present := lancet["enabled"]; present {
			value, ok := enabled.(bool)
			if !ok {
				return Settings{}, errors.New(`lancet must be an object like {"enabled": true}`)
			}
			settings.Lancet = value
		}
	}
	return settings, nil
}

// Change names the settings to write; nil fields stay as they are.
type Change struct {
	Mode    *Mode
	Lancet  *bool
	Enabled *bool
}

// SaveSettings writes only the changed keys and keeps every other key in the file. It refuses to overwrite an
// existing file that is not a JSON object. The write is atomic and the file mode is 0600.
func SaveSettings(file string, change Change) error {
	current := map[string]any{}
	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		if current, err = fsutil.DecodeObject(data); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("read %s: %w", file, err)
	}
	if change.Enabled != nil {
		current["enabled"] = *change.Enabled
	}
	if change.Mode != nil {
		current["mode"] = string(*change.Mode)
	}
	if change.Lancet != nil {
		lancet, ok := current["lancet"].(map[string]any)
		if !ok {
			lancet = map[string]any{}
		}
		lancet["enabled"] = *change.Lancet
		current["lancet"] = lancet
	}
	return fsutil.WriteJSONFileAtomic(file, current, 0o600)
}
