// Package sessionprobe supplies real host lifecycle transitions for ADHD integration tests.
package sessionprobe

import (
	"errors"
	"fmt"
	"sync"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func Extension() *sdk.Extension {
	e := sdk.New("session-probe")
	e.OnEvent(sdk.EventBeforeAgentStart, func(_ sdk.Context, data map[string]any) (any, error) {
		base, ok := data["systemPrompt"].(string)
		if !ok {
			return nil, errors.New("test contribution received no base prompt")
		}
		return map[string]any{"systemPrompt": base + "\n\nUNCHANGED_EXTENSION_SENTINEL"}, nil
	})
	var mu sync.Mutex
	compactMode := "removed"
	e.Command("adhd-test-branch", "Select a saved ADHD branch", func(ctx sdk.Context, args string) error {
		entries, err := ctx.SessionManager().GetEntries()
		if err != nil {
			return err
		}
		target := ""
		for _, entry := range entries {
			if args == "off" && entry["type"] == "custom" && entry["customType"] == "adhd-output.state" {
				data, _ := entry["data"].(map[string]any)
				if data["enabled"] == false {
					target, _ = entry["id"].(string)
					break
				}
			}
			if args == "on" && entry["type"] == "custom_message" && entry["customType"] == "adhd-output.rules.v1" {
				target, _ = entry["id"].(string)
			}
		}
		if target == "" {
			return errors.New("test branch point not found")
		}
		result, err := ctx.NavigateTree(target, map[string]any{"summarize": false})
		if err != nil {
			return err
		}
		if result.Cancelled {
			return errors.New("test branch navigation cancelled")
		}
		return nil
	})
	e.Command("adhd-test-new", "Create a real new session", func(ctx sdk.Context, _ string) error {
		result, err := ctx.NewSession(nil)
		if err != nil {
			return err
		}
		if result.Cancelled {
			return errors.New("test new session cancelled")
		}
		return nil
	})
	e.Command("adhd-test-reload", "Reload the actual extension instances", func(ctx sdk.Context, _ string) error { return ctx.Reload() })
	e.Command("adhd-test-fork", "Fork before the first user message", func(ctx sdk.Context, _ string) error {
		branch, err := ctx.SessionManager().GetBranch(nil)
		if err != nil {
			return err
		}
		for _, entry := range branch {
			message, _ := entry["message"].(map[string]any)
			if message["role"] != "user" {
				continue
			}
			id, ok := entry["id"].(string)
			if !ok {
				return errors.New("test fork point has no ID")
			}
			result, err := ctx.Fork(id, nil)
			if err != nil {
				return err
			}
			if result.Cancelled {
				return errors.New("test fork cancelled")
			}
			return nil
		}
		return errors.New("test fork has no user message")
	})
	e.OnEvent(sdk.EventSessionBeforeCompact, func(ctx sdk.Context, _ map[string]any) (any, error) {
		mu.Lock()
		mode := compactMode
		mu.Unlock()
		if mode == "cancel" {
			return map[string]any{"cancel": true}, nil
		}
		if mode == "fail" {
			// Let the host call the scripted failing provider. Callback errors are
			// reported by PiG but do not abort its default compaction.
			return nil, nil
		}
		branch, err := ctx.SessionManager().GetBranch(nil)
		if err != nil {
			return nil, err
		}
		keep := ""
		for _, entry := range branch {
			if mode == "retained" && entry["type"] == "custom_message" && entry["customType"] == "adhd-output.rules.v1" {
				keep, _ = entry["id"].(string)
			}
			message, _ := entry["message"].(map[string]any)
			if mode == "removed" && message["role"] == "user" {
				keep, _ = entry["id"].(string)
			}
		}
		if keep == "" {
			return nil, errors.New("test compaction has no keep entry")
		}
		return map[string]any{"compaction": map[string]any{"summary": "ADHD_TEST_COMPACT_SUMMARY", "firstKeptEntryId": keep, "tokensBefore": 1200}}, nil
	})
	e.Command("adhd-test-compact", "Persist a controlled real compaction", func(ctx sdk.Context, args string) error {
		mu.Lock()
		compactMode = args
		mu.Unlock()
		done := make(chan error, 1)
		ctx.CompactWithOptions(sdk.CompactOptions{OnComplete: func(map[string]any) { done <- nil }, OnError: func(err error) { done <- err }})
		select {
		case err := <-done:
			if args == "cancel" || args == "fail" {
				if err == nil {
					return fmt.Errorf("%s compaction unexpectedly succeeded", args)
				}
				return nil
			}
			return err
		case <-time.After(30 * time.Second):
			return errors.New("test compaction did not finish")
		}
	})
	return e
}
