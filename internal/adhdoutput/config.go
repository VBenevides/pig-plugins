// Package adhdoutput implements the session-aware ADHD presentation toggle.
package adhdoutput

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// SaveDefaultEnabled persists defaultEnabled so new sessions start with the chosen mode. It keeps every other
// field of an existing file and never replaces an invalid file, so a damaged configuration stays visible.
func SaveDefaultEnabled(path string, enabled bool) error {
	object := map[string]any{}
	data, err := readBounded(path, 64<<10)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read ADHD configuration: %w", err)
	default:
		if object, err = fsutil.DecodeObject(data); err != nil {
			return fmt.Errorf("decode ADHD configuration %s: %w", path, err)
		}
	}
	object["defaultEnabled"] = enabled
	return fsutil.WriteJSONFileAtomic(path, object, 0o600)
}

// Config is extension-owned configuration. Normal use needs no configuration file.
type Config struct {
	DefaultEnabled bool   `json:"defaultEnabled"`
	ShowStatus     bool   `json:"showStatus"`
	RulesFile      string `json:"rulesFile,omitempty"`
	Rules          string `json:"-"`
}

// ConfigPath honors the host's configuration root without modifying user files.
func ConfigPath() string {
	root := os.Getenv("PIG_HOME")
	if root == "" {
		root = filepath.Join(os.Getenv("HOME"), ".pig")
	}
	return filepath.Join(root, "state", "adhd-output", "config.json")
}

// LoadConfig only reads existing files. Relative rulesFile paths use the config directory.
func LoadConfig(path, defaultRules string) (Config, error) {
	config := Config{ShowStatus: true, Rules: defaultRules}
	data, err := readBounded(path, 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, fmt.Errorf("read ADHD configuration: %w", err)
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return config, errors.New("ADHD configuration must be a JSON object")
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("decode ADHD configuration: %w", err)
	}
	if config.RulesFile != "" {
		rulesPath := config.RulesFile
		if !filepath.IsAbs(rulesPath) {
			rulesPath = filepath.Join(filepath.Dir(path), rulesPath)
		}
		data, err := readBounded(rulesPath, 128<<10)
		if err != nil {
			return config, fmt.Errorf("read ADHD rules: %w", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return config, errors.New("ADHD rules file is empty")
		}
		config.Rules = string(data)
	}
	return config, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes: %s", limit, path)
	}
	return data, nil
}
