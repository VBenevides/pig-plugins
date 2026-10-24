// Package browserprobe exercises real host session changes in browser tests.
package browserprobe

import (
	"errors"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func Extension() *sdk.Extension {
	e := sdk.New("session-probe")
	e.Command("browser-test-new", "Create a test session", func(ctx sdk.Context, _ string) error {
		result, err := ctx.NewSession(nil)
		if err != nil {
			return err
		}
		if result.Cancelled {
			return errors.New("test session change was canceled")
		}
		return nil
	})
	e.Command("browser-test-reload", "Reload test extensions", func(ctx sdk.Context, _ string) error { return ctx.Reload() })
	return e
}
