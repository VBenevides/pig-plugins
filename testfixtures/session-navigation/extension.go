// Package sessionnavigation supplies real session transitions for integration tests.
package sessionnavigation

import (
	"fmt"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func Extension() *sdk.Extension {
	e := sdk.New("session-navigation")
	e.RegisterCommand("todo-test-branch", sdk.CommandOptions{Handler: func(ctx sdk.Context, _ string) error {
		branch, err := ctx.SessionManager().GetBranch(nil)
		if err != nil {
			return err
		}
		for _, entry := range branch {
			message, ok := entry["message"].(map[string]any)
			if !ok || message["role"] != "toolResult" || message["toolName"] != "todo" {
				continue
			}
			id, ok := entry["id"].(string)
			if !ok {
				return fmt.Errorf("todo result lacks session entry ID")
			}
			result, err := ctx.NavigateTree(id, map[string]any{"summarize": false})
			if err != nil {
				return err
			}
			if result.Cancelled {
				return fmt.Errorf("test tree navigation cancelled")
			}
			return nil
		}
		return fmt.Errorf("todo branch point not found")
	}})
	e.RegisterCommand("todo-test-new", sdk.CommandOptions{Handler: func(ctx sdk.Context, _ string) error {
		result, err := ctx.NewSession(nil)
		if err != nil {
			return err
		}
		if result.Cancelled {
			return fmt.Errorf("test new session cancelled")
		}
		return nil
	}})
	return e
}
