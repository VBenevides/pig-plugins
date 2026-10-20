package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// profileScoring ignores retired access settings when a shared mode is in use.
// A broken LANCET setting remains an error; it must not disable mandatory scoring.
func profileScoring(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read profile guard settings: %w", err)
	}
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return false, fmt.Errorf("decode profile guard settings: %w", err)
	}
	delete(object, "mode")
	delete(object, "enabled")
	data, err = json.Marshal(object)
	if err != nil {
		return false, fmt.Errorf("encode profile guard settings: %w", err)
	}
	settings, err := parseSettings(data)
	if err != nil {
		return false, err
	}
	return settings.Lancet, nil
}

func (c *Controller) sharedSnapshot() (ModeState, bool, string, error) {
	state, note, err := c.Modes.Effective(c.Settings)
	if err != nil {
		return state, false, note, err
	}
	scoring, err := profileScoring(c.Settings)
	return state, scoring, note, err
}

// RefreshShared reloads shared state for a status request or safe UI refresh.
func (c *Controller) RefreshShared(announce func()) (string, error) {
	state, scoring, note, err := c.sharedSnapshot()
	if err != nil {
		return note, err
	}
	c.Gate.mu.Lock()
	changed := c.Gate.access != state.Mode || c.Gate.lancetOn != scoring
	c.Gate.access, c.Gate.lancetOn = state.Mode, scoring
	c.Gate.mu.Unlock()
	if changed && announce != nil {
		announce()
	}
	return note, nil
}

func (c *Controller) checkShared(ctx context.Context, call Call, announce func()) Decision {
	state, scoring, _, err := c.sharedSnapshot()
	if err != nil {
		return block("cannot load current shared/profile settings: %v", err)
	}
	if err := ctx.Err(); err != nil {
		return block("admission cancelled: %v", err)
	}
	c.Gate.mu.Lock()
	changed := c.Gate.access != state.Mode || c.Gate.lancetOn != scoring
	c.Gate.access, c.Gate.lancetOn = state.Mode, scoring
	c.Gate.mu.Unlock()
	if changed && announce != nil {
		announce()
	}
	decision := c.Gate.CheckAccess(ctx, call, state.Mode)
	if decision.Block {
		return decision
	}
	// A dialog or a native score may take time. Never execute a call approved
	// under an obsolete policy, including an off->strict->off ABA transition.
	next, nextScoring, _, err := c.sharedSnapshot()
	if err != nil {
		return block("cannot revalidate shared/profile settings: %v", err)
	}
	if err := ctx.Err(); err != nil {
		return block("admission cancelled: %v", err)
	}
	if state != next || scoring != nextScoring {
		return block("guard mode or scoring changed during admission; retry the operation")
	}
	return decision
}
