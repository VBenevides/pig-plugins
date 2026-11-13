// Package toolsettings stores global discovery and invocation choices. It is not a filesystem sandbox.
package toolsettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

const FileName = "tool-settings.json"

// Skill retains discovery metadata so a disabled skill remains available in settings.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Hidden      bool   `json:"hidden,omitempty"`
}

type Config struct {
	DisabledTools  map[string]bool `json:"disabledTools"`
	EnabledTools   map[string]bool `json:"enabledTools,omitempty"`
	Defaults       map[string]bool `json:"defaults,omitempty"`
	DisabledSkills map[string]bool `json:"disabledSkills"`
	Skills         []Skill         `json:"skills,omitempty"`
	// Nil preserves normal defaults; an empty non-nil list permits no entries.
	ToolAllowlist  []string `json:"toolAllowlist"`
	SkillAllowlist []string `json:"skillAllowlist"`
	NativeFiles    bool     `json:"nativeFiles"`
}

func Path() string { return filepath.Join(agentdir.Dir(os.Getenv), FileName) }

func Load(path string) (Config, error) {
	c := Config{NativeFiles: true, DisabledTools: map[string]bool{}, DisabledSkills: map[string]bool{}, EnabledTools: map[string]bool{}, Defaults: map[string]bool{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read tool settings: %w", err)
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse tool settings: %w", err)
	}
	if c.DisabledTools == nil {
		c.DisabledTools = map[string]bool{}
	}
	if c.DisabledSkills == nil {
		c.DisabledSkills = map[string]bool{}
	}
	if c.EnabledTools == nil {
		c.EnabledTools = map[string]bool{}
	}
	if c.Defaults == nil {
		c.Defaults = map[string]bool{}
	}
	return c, nil
}

func Save(path string, c Config) error {
	return fsutil.WriteJSONFileAtomic(path, c, 0o600)
}

// Reset restores each tool's initial active state and restores skill discovery.
func (c *Config) Reset() {
	c.NativeFiles = true
	c.EnabledTools = map[string]bool{}
	c.DisabledTools = map[string]bool{}
	c.DisabledSkills = map[string]bool{}
	c.ToolAllowlist = nil
	c.SkillAllowlist = nil
}

// ToolEnabled and SkillEnabled apply restrictive presets to new entries as well as existing entries.
func (c Config) ToolEnabled(name string) bool {
	return !c.DisabledTools[name] &&
		(c.ToolAllowlist == nil || slices.Contains(c.ToolAllowlist, name)) &&
		(defaultTool(name) || c.EnabledTools[name] || slices.Contains(c.ToolAllowlist, name))
}

func (c Config) SkillEnabled(path string) bool {
	path = Canonical(path)
	return !c.DisabledSkills[path] && (c.SkillAllowlist == nil || slices.Contains(c.SkillAllowlist, path))
}

// Characters counts Unicode characters, not UTF-8 bytes. Tokens are an estimate, rounded up.
func Characters(text string) (int, int) { n := utf8.RuneCountInString(text); return n, (n + 3) / 4 }

func Canonical(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

// FilterSkills expands resource roots only when a skill is disabled. The host still parses metadata and
// decides which Markdown files are skills. Files within a skill directory are not independent skills.
func FilterSkills(paths []string, c Config) ([]string, error) {
	if c.SkillAllowlist != nil && len(c.SkillAllowlist) == 0 {
		return nil, nil
	}
	if len(c.DisabledSkills) == 0 && c.SkillAllowlist == nil {
		return paths, nil
	}
	var out []string
	seen := map[string]bool{}
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 32 {
			return fmt.Errorf("skill directory nesting exceeds 32 at %s", path)
		}
		canonical := Canonical(path)
		if seen[canonical] {
			return nil
		}
		seen[canonical] = true
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("inspect skill path %s: %w", path, err)
		}
		if !info.IsDir() {
			if strings.HasSuffix(path, ".md") && c.SkillEnabled(canonical) {
				out = append(out, path)
			}
			return nil
		}
		skillFile := filepath.Join(path, "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			if c.SkillEnabled(Canonical(skillFile)) {
				out = append(out, path)
			}
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("inspect skill %s: %w", skillFile, err)
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("list skills %s: %w", path, err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" {
				continue
			}
			if err := visit(filepath.Join(path, entry.Name()), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, path := range paths {
		if err := visit(path, 0); err != nil {
			return nil, err
		}
	}
	return out, nil
}
