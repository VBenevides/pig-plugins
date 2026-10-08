// Package settingswriter is a test-only stand-in for another session's settings
// command. It deliberately does not update the running native guard's memory.
package settingswriter

import (
	"fmt"
	"os"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/guard"
)

func Extension() *sdk.Extension {
	e := sdk.New("settings-writer")
	e.RegisterCommand("persist-guard-mode", sdk.CommandOptions{Handler: func(ctx sdk.Context, args string) error {
		mode, ok := guard.ParseMode(args)
		if !ok {
			return fmt.Errorf("invalid test mode %q", args)
		}
		if err := guard.SaveSettings(guard.SettingsFile(os.Getenv), guard.Change{Mode: &mode}); err != nil {
			return err
		}
		ctx.Notify("test writer saved "+string(mode), "info")
		return nil
	}})
	return e
}
