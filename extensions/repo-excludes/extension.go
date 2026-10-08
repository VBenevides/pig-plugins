// Package repoexcludes keeps agent artifacts out of Git's untracked-file list.
package repoexcludes

import (
	"fmt"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/repoexcludes"
)

// Extension updates only Git's private exclude file, once per session start.
func Extension() *sdk.Extension {
	e := sdk.New("repo-excludes")
	e.OnEvent(sdk.EventSessionStart, onSessionStart)
	return e
}

func onSessionStart(ctx sdk.Context, _ map[string]any) (any, error) {
	trusted, err := ctx.IsProjectTrusted()
	if err != nil {
		return report(ctx, fmt.Errorf("check project trust: %w", err))
	}
	if !trusted {
		return nil, nil
	}
	_, err = repoexcludes.Ensure(ctx.Cwd())
	if err != nil {
		return report(ctx, err)
	}
	return nil, nil
}

func report(ctx sdk.Context, err error) (any, error) {
	err = fmt.Errorf("repo-excludes: %w", err)
	ctx.Notify(err.Error(), "error")
	return nil, err
}
