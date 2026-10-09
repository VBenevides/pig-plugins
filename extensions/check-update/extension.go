// Package checkupdate checks for distribution updates at startup.
package checkupdate

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
	"github.com/VBenevides/pig-plugins/internal/updates"
)

const Name = "check-update"

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	preference := func() string { return agentdir.File(os.Getenv, "check-update.json") }
	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		runCtx, cancel := sdkctx.Request(ctx)
		defer cancel()
		pigVersion := os.Getenv("PIG_PLUGINS_HOST_VERSION")
		if pigVersion == "" {
			versionCtx, cancelVersion := context.WithTimeout(runCtx, 2*time.Second)
			output, err := exec.CommandContext(versionCtx, "pig", "--version").Output()
			cancelVersion()
			if err != nil {
				ctx.Notify("Cannot identify PiG version: "+err.Error(), "warning")
				pigVersion = "unknown"
			} else {
				pigVersion = strings.TrimSpace(string(output))
			}
		}
		pigVersion = strings.TrimPrefix(pigVersion, "pig ")
		enabled, err := updates.Enabled(preference())
		if err != nil {
			ctx.Notify(err.Error(), "warning")
			return nil, nil
		}
		if !enabled || os.Getenv("PI_OFFLINE") == "1" {
			return nil, nil
		}
		client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		checkUpdates(runCtx, ctx.Notify, client, pigVersion, updates.CompatibilityURL, updates.PluginsURL)
		return nil, nil
	})
	e.RegisterCommand(Name, sdk.CommandOptions{
		Description: "Persist startup update checking (/check-update on|off)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			var items []sdk.AutocompleteItem
			for _, value := range []string{"on", "off"} {
				if strings.HasPrefix(value, prefix) {
					items = append(items, sdk.AutocompleteItem{Value: value, Label: value})
				}
			}
			return items, nil
		},
		Handler: func(ctx sdk.Context, args string) error {
			value := strings.ToLower(strings.TrimSpace(args))
			if value != "on" && value != "off" {
				ctx.Notify("usage: /check-update on|off", "warning")
				return nil
			}
			if err := updates.SaveEnabled(preference(), value == "on"); err != nil {
				return fmt.Errorf("save update preference: %w", err)
			}
			ctx.Notify("Startup update checks: "+value, "info")
			return nil
		},
	})
	return e
}
