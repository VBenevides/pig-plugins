// Package compatibility reads the distribution's supported dependency versions.
package compatibility

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Dependency identifies a dependency and its latest supported release.
type Dependency struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// PiG reads and validates the PiG entry in the JSON compatibility manifest.
func PiG(data []byte) (Dependency, error) {
	var manifest struct {
		Dependencies map[string]Dependency `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Dependency{}, fmt.Errorf("decode compatibility manifest: %w", err)
	}
	pig, ok := manifest.Dependencies["pig"]
	if !ok || strings.TrimSpace(pig.Path) == "" {
		return Dependency{}, fmt.Errorf("compatibility manifest requires dependencies.pig.path")
	}
	parts := strings.Split(pig.Version, ".")
	if len(parts) != 3 {
		return Dependency{}, fmt.Errorf("invalid compatible PiG version %q", pig.Version)
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return Dependency{}, fmt.Errorf("invalid compatible PiG version %q", pig.Version)
		}
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return Dependency{}, fmt.Errorf("invalid compatible PiG version %q: %w", pig.Version, err)
		}
	}
	return pig, nil
}
