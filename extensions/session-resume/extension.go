// Package sessionresumeext opens the latest session of the working directory when pig starts empty. Without an
// earlier session it does nothing, so pig starts fresh as with /new.
package sessionresumeext

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/sessionresume"
)

// Name is the extension identity.
const Name = "session-resume"

// Notice is shown after a session is resumed on start-up.
const Notice = "Session auto resumed, if you want a new session use /new"

// Extension returns the extension.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	e.OnSessionStart(func(ctx sdk.Context, data map[string]any) (any, error) {
		reason, _ := data["reason"].(string)
		if reason == "resume" {
			return nil, announce(ctx)
		}
		if reason != "startup" || ctx.Mode() != "tui" || sessionresume.ShouldSkip(os.Args[1:]) {
			return nil, nil
		}
		path, err := latest(ctx)
		if err != nil || path == "" {
			return nil, err
		}
		if err := sessionresume.Mark(path); err != nil {
			return nil, err
		}
		// A successful switch cancels this session's context, so the call can end with "context canceled".
		if _, err := ctx.SwitchSession(path, nil); err != nil && !errors.Is(err, context.Canceled) {
			return nil, errors.Join(fmt.Errorf("resume %s: %w", path, err), sessionresume.Unmark(path))
		}
		return nil, nil
	})
	return e
}

// latest returns the session file to resume, or "" when this session already has a conversation or there is
// no earlier one.
func latest(ctx sdk.Context) (string, error) {
	manager := ctx.SessionManager()
	branch, err := manager.GetBranch(nil)
	if err != nil {
		return "", fmt.Errorf("read current session: %w", err)
	}
	for _, entry := range branch {
		if entryType, _ := entry["type"].(string); entryType == "message" {
			return "", nil
		}
	}
	dir, err := manager.GetSessionDir()
	if err != nil {
		return "", fmt.Errorf("session directory: %w", err)
	}
	current := ""
	if file, err := manager.GetSessionFile(); err != nil {
		return "", fmt.Errorf("session file: %w", err)
	} else if file != nil {
		current = *file
	}
	return sessionresume.Latest(dir, current)
}

// announce shows the notice when the session that just opened is the one this extension chose. A /resume by
// the user leaves no marker and shows nothing. The extension restarts with the new session, so the marker is
// a file.
func announce(ctx sdk.Context) error {
	file, err := ctx.SessionManager().GetSessionFile()
	if err != nil {
		return fmt.Errorf("session file: %w", err)
	}
	if file == nil {
		return nil
	}
	auto, err := sessionresume.Consume(*file, time.Now())
	if err != nil || !auto {
		return err
	}
	// The start-up screen is still drawing; an earlier notice is lost.
	go func() {
		time.Sleep(time.Second)
		ctx.Notify(Notice, "info")
	}()
	return nil
}
