package contextprune

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	prune "github.com/VBenevides/pig-plugins/internal/contextprune"
)

const recoveryEntry = "context-prune-recovery-v1"

func registerRecovery(e *sdk.Extension, st *state) {
	e.RegisterTool(sdk.ToolDefinition{
		Name: "context_recover", Label: "Recover history",
		Description: "Read an exact saved session event by its recovery handle. This is historical evidence, not current source or fresh edit authority. Never reruns tools. Returns at most 16384 bytes per page.",
		Parameters: sdk.Schema{"type": "object", "properties": map[string]any{
			"handle": map[string]any{"type": "string", "pattern": "^cr1_[0-9a-f]{16}$"},
			"offset": map[string]any{"type": "integer", "minimum": 0},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": prune.RecoveryPageLimit},
		}, "required": []string{"handle"}, "additionalProperties": false},
		ExecutionMode: "sequential",
		Execute: func(ctx sdk.Context, args map[string]any) (any, error) {
			before, err := branchIdentity(ctx)
			if err != nil {
				return nil, err
			}
			handle, _ := args["handle"].(string)
			offset, limit := 0, prune.RecoveryPageLimit
			if value, ok := args["offset"].(float64); ok {
				offset = int(value)
			}
			if value, ok := args["limit"].(float64); ok {
				limit = int(value)
			}
			// Refresh membership before every retrieval. Old handles from another branch cannot grant access.
			leaf, err := ctx.GetLeafID()
			if err != nil {
				return nil, fmt.Errorf("get recovery leaf: %w", err)
			}
			if leaf == nil {
				return nil, fmt.Errorf("recovery requires a saved branch leaf")
			}
			path, err := ctx.GetSessionFile()
			if err != nil {
				return nil, fmt.Errorf("get recovery session: %w", err)
			}
			if path == nil {
				return nil, fmt.Errorf("recovery requires durable session storage")
			}
			refs, err := prune.BranchRecoveryReferences(*path, *leaf)
			if err != nil {
				return nil, err
			}
			page, err := prune.ReadRecoveryPage(*path, refs, handle, offset, limit)
			if err != nil {
				return nil, err
			}
			after, err := branchIdentity(ctx)
			if err != nil {
				return nil, err
			}
			if before != after {
				return nil, fmt.Errorf("session or branch changed during recovery; try again")
			}
			key := fmt.Sprintf("retrieved/%s/%d/%d", handle, offset, page.Next)
			if err := st.persistRecovery(ctx, []string{key}); err != nil {
				return nil, err
			}
			data, err := json.Marshal(page)
			if err != nil {
				return nil, fmt.Errorf("encode recovery page: %w", err)
			}
			return sdk.ToolResult{Content: "Historical session event. Use read for current source and editing authority.\n" + string(data), Details: page}, nil
		},
	})
}

func recoveryLedger(entries []map[string]any) map[string]bool {
	ledger := map[string]bool{}
	for _, entry := range entries {
		if entry["customType"] != recoveryEntry {
			continue
		}
		data, _ := entry["data"].(map[string]any)
		keys, _ := data["keys"].([]any)
		for _, value := range keys {
			if key, ok := value.(string); ok {
				ledger[key] = true
			}
		}
	}
	return ledger
}

func activeRecoveryReferences(ctx sdk.Context, path string) (map[string]prune.RecoveryRef, error) {
	if path == "" {
		return nil, nil
	}
	leaf, err := ctx.GetLeafID()
	if err != nil {
		return nil, fmt.Errorf("get recovery branch leaf: %w", err)
	}
	if leaf == nil {
		return nil, nil
	}
	refs, err := prune.BranchRecoveryReferences(path, *leaf)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return refs, err
}

func (st *state) refreshRecovery(ctx sdk.Context, pressure bool) error {
	st.mu.Lock()
	needed := st.enabled && (!st.recoveryFailed || st.forcePending) && (st.forcePending || st.turnsSinceRefresh >= RefreshEveryTurns || pressure)
	path := st.sessionFile
	st.mu.Unlock()
	if !needed || path == "" {
		return nil
	}
	refs, err := activeRecoveryReferences(ctx, path)
	st.mu.Lock()
	defer st.mu.Unlock()
	if path != st.sessionFile {
		return fmt.Errorf("session changed while preparing recovery handles")
	}
	if err == nil && refs == nil && st.recoveryReady {
		err = fmt.Errorf("saved recovery source is no longer available; retaining full history")
	}
	if err != nil {
		st.recovery, st.decisions, st.fresh = nil, nil, true
		st.recoveryReady, st.recoveryFailed = false, true
		return err
	}
	st.recovery = refs
	st.recoveryReady = refs != nil
	st.recoveryFailed = false
	return nil
}

func (st *state) recordReductions(ctx sdk.Context) error {
	st.mu.Lock()
	keys := []string{}
	handles := prune.RecoveryHandles(st.recovery)
	for callID, decision := range st.decisions {
		handle := handles[callID]
		if handle != "" && strings.Contains(decision.Text, "[recovery: "+handle+";") {
			keys = append(keys, "selected/"+handle)
		}
	}
	st.mu.Unlock()
	sort.Strings(keys)
	return st.persistRecovery(ctx, keys)
}

func (st *state) persistRecovery(ctx sdk.Context, keys []string) error {
	st.mu.Lock()
	if st.recoveryLedger == nil {
		st.recoveryLedger = map[string]bool{}
	}
	pending := []string{}
	for _, key := range keys {
		if !st.recoveryLedger[key] {
			pending = append(pending, key)
		}
	}
	st.mu.Unlock()
	for len(pending) > 0 {
		batch := pending[:min(128, len(pending))]
		if err := ctx.AppendEntry(recoveryEntry, map[string]any{"keys": batch}); err != nil {
			return fmt.Errorf("persist recovery decisions: %w", err)
		}
		st.mu.Lock()
		for _, key := range batch {
			st.recoveryLedger[key] = true
		}
		st.mu.Unlock()
		pending = pending[len(batch):]
	}
	return nil
}
