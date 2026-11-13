// Package pathcheck reports likely surrounding-whitespace typos without changing tool input.
package pathcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func Extension() *sdk.Extension {
	e := sdk.New("path-check")
	e.OnEvent(sdk.EventToolCall, func(ctx sdk.Context, data map[string]any) (any, error) {
		name, _ := data["toolName"].(string)
		if name != "read" && name != "edit" {
			return nil, nil
		}
		input, _ := data["input"].(map[string]any)
		path, _ := input["path"].(string)
		reason := pathHint(path, ctx.Cwd(), os.Stat)
		if reason == "" {
			return nil, nil
		}
		ctx.Notify(reason, "warning")
		return map[string]any{"block": true, "reason": reason}, nil
	})
	return e
}

// Only ordinary filesystem paths are inspected. The host owns special path syntax
// and platform-specific normalization. Other stat errors are left to the tool.
func pathHint(path, cwd string, stat func(string) (os.FileInfo, error)) string {
	trimmed := strings.Trim(path, " \t\r\n")
	if trimmed == path || trimmed == "" {
		return ""
	}
	for _, value := range []string{path, trimmed} {
		if strings.HasPrefix(value, "~") || strings.HasPrefix(value, "@") || strings.Contains(value, "://") {
			return ""
		}
	}
	resolve := func(value string) string {
		if filepath.IsAbs(value) {
			return value
		}
		return filepath.Join(cwd, value)
	}
	if _, err := stat(resolve(path)); !os.IsNotExist(err) {
		return ""
	}
	if _, err := stat(resolve(trimmed)); err != nil {
		return ""
	}
	return fmt.Sprintf("Path not found: %q. Possible match after removing surrounding whitespace: %q. Retry with that exact path. The path was not changed automatically.", path, trimmed)
}
