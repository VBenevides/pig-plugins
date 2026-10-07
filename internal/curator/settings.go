package curator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// Engines are the accepted search engines.
var Engines = []string{"legacy", "fts", "hybrid", "episodes", "state"}

// Environment variables the settings map onto. A value saved with /pi-curator wins over the same variable in the
// environment, because it records what the user last chose.
const (
	EnvPrefetchBudget = "PI_CURATOR_PREFETCH_BUDGET"
	EnvStartup        = "PI_CURATOR_STARTUP_DECISIONS"
	EnvEngine         = "PI_CURATOR_SEARCH_ENGINE"
	EnvPrefetchEngine = "PI_CURATOR_PREFETCH_ENGINE"
)

// Defaults of the PiG integration (the upstream plugin itself defaults to 500 and no startup decisions).
const (
	DefaultPrefetch = "512"
	DefaultStartup  = "2"
	DefaultEngine   = "legacy"
)

// Settings holds the saved values; a nil field is not saved.
type Settings struct {
	Prefetch *int
	Startup  *int
	Engine   string
}

var wholeNumber = regexp.MustCompile(`^[0-9]+$`)

func parseWhole(text string) (int, bool) {
	if text == "off" {
		return 0, true
	}
	if !wholeNumber.MatchString(text) {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	return n, err == nil
}

// ParseSetting parses a `/pi-curator <key> <value>` pair and names the accepted range on a bad value.
func ParseSetting(key, value string) (Settings, error) {
	switch key {
	case "prefetch":
		n, ok := parseWhole(value)
		if !ok || (n != 0 && (n < 64 || n > 8192)) {
			return Settings{}, errors.New("prefetch must be off, 0 or a whole number from 64 to 8192")
		}
		return Settings{Prefetch: &n}, nil
	case "startup":
		n, ok := parseWhole(value)
		if !ok || n > 10 {
			return Settings{}, errors.New("startup must be off, 0 or a whole number from 1 to 10")
		}
		return Settings{Startup: &n}, nil
	case "engine":
		if !slices.Contains(Engines, value) {
			return Settings{}, fmt.Errorf("engine must be %s", strings.Join(Engines, ", "))
		}
		return Settings{Engine: value}, nil
	}
	return Settings{}, fmt.Errorf("unknown setting %q; use prefetch, startup or engine", key)
}

// fileKeys maps a settings-file key to the /pi-curator key that validates it.
var fileKeys = []struct{ file, command string }{
	{"prefetchBudget", "prefetch"},
	{"startupDecisions", "startup"},
	{"searchEngine", "engine"},
}

// jsString renders a decoded JSON value the way JavaScript's String() does for the shapes a settings file holds.
func jsString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		n, err := v.Float64()
		if err != nil {
			return v.String()
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	case nil:
		return "null"
	}
	return fmt.Sprint(value)
}

func merge(into *Settings, change Settings) {
	if change.Prefetch != nil {
		into.Prefetch = change.Prefetch
	}
	if change.Startup != nil {
		into.Startup = change.Startup
	}
	if change.Engine != "" {
		into.Engine = change.Engine
	}
}

// LoadSettings reads and validates the file. A missing file is empty; an unreadable, malformed or invalid file is
// an error, so a typo is never half applied.
func LoadSettings(file string) (Settings, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("%s is unreadable: %w", file, err)
	}
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return Settings{}, fmt.Errorf("%s is not a valid settings object: %w", file, err)
	}
	var result Settings
	for _, key := range fileKeys {
		value, present := object[key.file]
		if !present {
			continue
		}
		change, err := ParseSetting(key.command, jsString(value))
		if err != nil {
			return Settings{}, fmt.Errorf("%s: %w", file, err)
		}
		merge(&result, change)
	}
	return result, nil
}

// SaveSettings merges change into the file, keeping every other key, and writes it atomically with mode 0600.
func SaveSettings(file string, change Settings) error {
	current := map[string]any{}
	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		if current, err = fsutil.DecodeObject(data); err != nil {
			return fmt.Errorf("%s must be a JSON object: %w", file, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("read %s: %w", file, err)
	}
	if change.Prefetch != nil {
		current["prefetchBudget"] = *change.Prefetch
	}
	if change.Startup != nil {
		current["startupDecisions"] = *change.Startup
	}
	if change.Engine != "" {
		current["searchEngine"] = change.Engine
	}
	return fsutil.WriteJSONFileAtomic(file, current, 0o600)
}

// Config resolves the effective values from the settings file, the environment and the defaults.
type Config struct {
	Getenv    func(string) string
	LookupEnv func(string) (string, bool)
}

// File is the settings file in the agent directory.
func (c Config) File() string { return agentdir.File(c.Getenv, "pi-curator.json") }

// Values are the effective settings as text, the way the plugin reads its environment variables.
type Values struct {
	Prefetch string
	Startup  string
	Engine   string
	sources  [3]string
}

// Effective returns the values with saved settings applied over the environment over the defaults. When the
// settings file is damaged it returns the environment values together with the error: a damaged file is reported,
// never trusted, and never stops the session.
func (c Config) Effective() (Values, error) {
	saved, err := LoadSettings(c.File())
	var out Values
	pick := func(i int, savedText, variable, fallback string) string {
		if savedText != "" {
			out.sources[i] = "saved"
			return savedText
		}
		value := c.Getenv(variable)
		present := value != ""
		if c.LookupEnv != nil {
			value, present = c.LookupEnv(variable)
		}
		if present {
			out.sources[i] = "environment " + variable
			return value
		}
		out.sources[i] = "default"
		return fallback
	}
	var prefetch, startup string
	if saved.Prefetch != nil {
		prefetch = strconv.Itoa(*saved.Prefetch)
	}
	if saved.Startup != nil {
		startup = strconv.Itoa(*saved.Startup)
	}
	out.Prefetch = pick(0, prefetch, EnvPrefetchBudget, DefaultPrefetch)
	out.Startup = pick(1, startup, EnvStartup, DefaultStartup)
	out.Engine = pick(2, saved.Engine, EnvEngine, DefaultEngine)
	return out, err
}

// Describe lists each setting with its effective value and where it came from.
func (c Config) Describe() []string {
	values, _ := c.Effective() // a damaged file is reported by the caller, and shows here as nothing saved
	return []string{
		fmt.Sprintf("prefetch: %s (%s)", values.Prefetch, values.sources[0]),
		fmt.Sprintf("startup: %s (%s)", values.Startup, values.sources[1]),
		fmt.Sprintf("engine: %s (%s)", values.Engine, values.sources[2]),
	}
}
