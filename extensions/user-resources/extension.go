// Package userresources loads skills, prompts and themes from the agent directory at runtime.
//
// A fused pig-plugins binary ignores ambient resource discovery, so resources installed under the agent
// directory (for example ~/.pig/agent/skills) are contributed here instead. The optional file
// <agent dir>/pig-plugins.json overrides the defaults without rebuilding:
//
//	{"skillPaths": ["skills", "~/more-skills"], "promptPaths": [], "themePaths": []}
//
// A key that is present replaces its default; relative paths resolve against the agent directory.
package userresources

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
)

const (
	Name       = "user-resources"
	ConfigFile = "pig-plugins.json"
)

type config struct {
	SkillPaths  *[]string `json:"skillPaths"`
	PromptPaths *[]string `json:"promptPaths"`
	ThemePaths  *[]string `json:"themePaths"`
}

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	e.OnEvent(sdk.EventResourcesDiscover, func(ctx sdk.Context, _ map[string]any) (any, error) {
		paths, err := resolve(os.Getenv)
		if err != nil {
			ctx.Notify(err.Error(), "error")
			return nil, err
		}
		return paths, nil
	})
	return e
}

// resolve returns the resource paths to contribute. Missing default directories are skipped; a malformed
// config file is an error rather than a silent fallback.
func resolve(getenv func(string) string) (map[string][]string, error) {
	dir := agentdir.Dir(getenv)
	cfg, err := load(filepath.Join(dir, ConfigFile))
	if err != nil {
		return nil, err
	}
	home := getenv("HOME")
	result := map[string][]string{}
	for key, spec := range map[string]struct {
		configured *[]string
		fallback   string
	}{
		"skillPaths":  {cfg.SkillPaths, "skills"},
		"promptPaths": {cfg.PromptPaths, "prompts"},
		"themePaths":  {cfg.ThemePaths, "themes"},
	} {
		if spec.configured == nil {
			if isDir(filepath.Join(dir, spec.fallback)) {
				result[key] = []string{filepath.Join(dir, spec.fallback)}
			}
			continue
		}
		for _, p := range *spec.configured {
			result[key] = append(result[key], expand(p, dir, home))
		}
	}
	return result, nil
}

func load(path string) (config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return config{}, nil
	}
	if err != nil {
		return config{}, fmt.Errorf("user-resources: read %s: %w", path, err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("user-resources: parse %s: %w", path, err)
	}
	return cfg, nil
}

func expand(p, agentDir, home string) string {
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case filepath.IsAbs(p):
		return p
	default:
		return filepath.Join(agentDir, p)
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
