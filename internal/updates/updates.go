// Package updates checks the two distribution version sources without installing anything.
package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/VBenevides/pig-plugins/internal/compatibility"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

const CompatibilityURL = "https://raw.githubusercontent.com/VBenevides/pig-plugins/main/COMPATIBILITY.json"
const PluginsURL = "https://raw.githubusercontent.com/VBenevides/pig-plugins/main/VERSION"

var versionPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:\+[0-9A-Za-z.-]+)?$`)

func Newer(latest, current string) (bool, error) {
	parse := func(value string) ([3]uint64, error) {
		var parts [3]uint64
		match := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
		if match == nil {
			return parts, fmt.Errorf("invalid release version %q", value)
		}
		for i := range parts {
			number, err := strconv.ParseUint(match[i+1], 10, 64)
			if err != nil {
				return parts, fmt.Errorf("invalid version component: %w", err)
			}
			parts[i] = number
		}
		return parts, nil
	}
	next, err := parse(latest)
	if err != nil {
		return false, err
	}
	prev, err := parse(current)
	if err != nil {
		return false, err
	}
	for i := range next {
		if next[i] != prev[i] {
			return next[i] > prev[i], nil
		}
	}
	return false, nil
}

func fetchVersionSource(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil {
		return nil, err
	}
	if len(body) > 4096 {
		return nil, fmt.Errorf("version response exceeds 4096 bytes")
	}
	return body, nil
}

// LatestCompatiblePiG reads the supported release, not the newest upstream release.
func LatestCompatiblePiG(ctx context.Context, client *http.Client, url string) (string, error) {
	body, err := fetchVersionSource(ctx, client, url)
	if err != nil {
		return "", err
	}
	pig, err := compatibility.PiG(body)
	if err != nil {
		return "", err
	}
	return pig.Version, nil
}

func Latest(ctx context.Context, client *http.Client, url string, jsonResponse bool) (string, error) {
	body, err := fetchVersionSource(ctx, client, url)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(body))
	if jsonResponse {
		var result struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("decode version response: %w", err)
		}
		value = result.Version
	}
	if _, err := Newer(value, "0.0.0"); err != nil {
		return "", err
	}
	return strings.TrimPrefix(value, "v"), nil
}

func Enabled(path string) (bool, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read update preference: %w", err)
	}
	var settings struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return false, fmt.Errorf("decode update preference: %w", err)
	}
	if settings.Enabled == nil {
		return false, fmt.Errorf("update preference has no enabled value")
	}
	return *settings.Enabled, nil
}

func SaveEnabled(path string, enabled bool) error {
	return fsutil.WriteJSONFileAtomic(path, struct {
		Enabled bool `json:"enabled"`
	}{enabled}, 0600)
}
