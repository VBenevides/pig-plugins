package ask

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed upstream.json
var upstreamJSON []byte

// Captured from rpiv-ask-user-question@2.12.0. Regenerate from the pinned upstream, never from Go output.
var upstream = func() struct {
	Schema   map[string]any `json:"schema"`
	Guidance Guidance       `json:"guidance"`
} {
	var value struct {
		Schema   map[string]any `json:"schema"`
		Guidance Guidance       `json:"guidance"`
	}
	if err := json.Unmarshal(upstreamJSON, &value); err != nil {
		panic(fmt.Sprintf("invalid embedded questionnaire contract: %v", err))
	}
	return value
}()

type Guidance struct {
	Description string   `json:"description"`
	Snippet     string   `json:"promptSnippet"`
	Guidelines  []string `json:"promptGuidelines"`
}
type Config struct {
	Guidance    Guidance
	CollapseKey string
	Warning     string
}

func Schema() map[string]any { return upstream.Schema }
func LoadConfig(getenv func(string) string) Config {
	home := getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	base := strings.TrimSpace(getenv("XDG_CONFIG_HOME"))
	if base == "~" {
		base = home
	} else if strings.HasPrefix(base, "~/") {
		base = filepath.Join(home, base[2:])
	}
	legacy := filepath.Join(home, ".config", "rpiv-ask-user-question", "config.json")
	if !filepath.IsAbs(base) {
		base = filepath.Join(home, ".config")
	}
	path := filepath.Join(base, "rpiv-ask-user-question", "config.json")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) && path != legacy {
		path = legacy
		file, err = os.Open(path)
	}
	out := Config{Guidance: upstream.Guidance, CollapseKey: "ctrl+]"}
	if errors.Is(err, os.ErrNotExist) {
		return out
	}
	if err != nil {
		out.Warning = fmt.Sprintf("questionnaire config %s: %v", path, err)
		return out
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	var raw map[string]any
	if err == nil {
		if len(data) > 1<<20 {
			err = errors.New("file exceeds 1 MiB")
		} else {
			err = json.Unmarshal(data, &raw)
			if err == nil && raw == nil {
				err = errors.New("expected an object")
			}
		}
	}
	if err != nil {
		out.Warning = fmt.Sprintf("questionnaire config %s: %v; defaults used", path, err)
		return out
	}
	if fields, ok := raw["guidance"].(map[string]any); ok {
		if value, ok := fields["description"].(string); ok && value != "" {
			out.Guidance.Description = value
		}
		if value, ok := fields["promptSnippet"].(string); ok && value != "" {
			out.Guidance.Snippet = value
		}
		if values, ok := fields["promptGuidelines"].([]any); ok && len(values) > 0 {
			valid := true
			guidelines := make([]string, len(values))
			for i, v := range values {
				value, ok := v.(string)
				if !ok || value == "" {
					valid = false
					break
				}
				guidelines[i] = value
			}
			if valid {
				out.Guidance.Guidelines = guidelines
			}
		}
	}
	if value, ok := raw["collapseKey"].(string); ok {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "off" || validCollapse(value) {
			out.CollapseKey = value
		}
	}
	return out
}
func validCollapse(value string) bool {
	if value == "" || strings.HasPrefix(value, "+") || strings.HasSuffix(value, "+") || strings.Contains(value, "++") {
		return false
	}
	parts := strings.Split(value, "+")
	seen := map[string]bool{}
	for _, part := range parts[:len(parts)-1] {
		if seen[part] || !slices.Contains([]string{"ctrl", "shift", "alt", "super"}, part) {
			return false
		}
		seen[part] = true
	}
	base := parts[len(parts)-1]
	if len(base) == 1 {
		return strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789_-!@#$%^&*()|~`'\":;,./<>?[]{}=\\", rune(base[0]))
	}
	return slices.Contains(strings.Fields("escape esc enter return tab space backspace delete insert clear home end pageup pagedown up down left right f1 f2 f3 f4 f5 f6 f7 f8 f9 f10 f11 f12"), base)
}
