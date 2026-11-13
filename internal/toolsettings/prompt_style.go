package toolsettings

import (
	"fmt"
	"strings"
)

func normalizePromptStyle(style string) (string, error) {
	style = strings.ToLower(style)
	switch style {
	case "", "default", "legacy":
		return style, nil
	default:
		return "", fmt.Errorf("unknown prompt style %q; use default or legacy", style)
	}
}

func (c Config) Style() string {
	if c.PromptStyle == "" {
		return "default"
	}
	return c.PromptStyle
}
