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
		mode, ok := guard.ParseAccessMode(args)
		if !ok {
			return fmt.Errorf("invalid test mode %q", args)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		if err := (guard.ModeStore{Path: guard.SharedModeFile(home)}).Save(mode); err != nil {
			return err
		}
		ctx.Notify("test writer saved "+string(mode), "info")
		return nil
	}})
	return e
}
