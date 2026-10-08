package betterfooter

import (
	"encoding/json"
	"errors"
)

// ParseProjectVersion extracts the normalized version of a package.json document; a manifest without a
// semantic-version "version" yields "" and no error.
func ParseProjectVersion(manifest []byte) (string, error) {
	var parsed struct {
		Version any `json:"version"`
	}
	if json.Unmarshal(manifest, &parsed) != nil {
		return "", errors.New("package.json: invalid JSON")
	}
	version, _ := parsed.Version.(string)
	return NormalizeVersion(version), nil
}
