package lancet

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RetentionPeriod is how long the model may stay unused before cleanup deletes it.
const RetentionPeriod = 7 * 24 * time.Hour

const (
	touchInterval = time.Hour
	markerName    = "last-used"
)

var (
	touchMu   sync.Mutex
	lastTouch = map[string]time.Time{}
)

// storeRoot is the directory that holds the model, the runtime and the last-used marker.
func storeRoot(agentDir string) string { return filepath.Join(agentDir, "smart-approve-lancet") }

func markerPath(root string) string { return filepath.Join(root, markerName) }

func writeMarker(root string, now time.Time) error {
	target := markerPath(root)
	temporary := fmt.Sprintf("%s.%d.tmp", target, os.Getpid())
	if err := os.WriteFile(temporary, []byte(now.UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		return errors.Join(err, os.Remove(temporary))
	}
	return nil
}

// readLastUsed parses the marker. It reports false when the marker is missing, invalid or in the future.
func readLastUsed(root string, now time.Time) (time.Time, bool) {
	data, err := os.ReadFile(markerPath(root))
	if err != nil {
		return time.Time{}, false
	}
	used, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil || used.After(now) {
		return time.Time{}, false
	}
	return used, true
}

// TouchLastUsed records that the model was used. It writes at most once an hour per process, only when the store
// directory exists, and never fails the caller: a disk error is passed to log.
func TouchLastUsed(agentDir string, now time.Time, log func(string)) {
	root := storeRoot(agentDir)
	touchMu.Lock()
	defer touchMu.Unlock()
	if previous, ok := lastTouch[root]; ok && !now.Before(previous) && now.Sub(previous) < touchInterval {
		return
	}
	if _, err := os.Stat(root); err != nil {
		return
	}
	if err := writeMarker(root, now); err != nil {
		if log != nil {
			log(fmt.Sprintf("lancet last-used update failed: %v", err))
		}
		return
	}
	lastTouch[root] = now
}

// ResetTouchThrottle forgets the write throttle. Tests use it.
func ResetTouchThrottle() {
	touchMu.Lock()
	defer touchMu.Unlock()
	clear(lastTouch)
}

// DeletionDate is when cleanup will delete an unused model, or false when the marker is not valid.
func DeletionDate(agentDir string, now time.Time) (time.Time, bool) {
	used, ok := readLastUsed(storeRoot(agentDir), now)
	return used.Add(RetentionPeriod), ok
}

// CleanupOptions say when it is safe to delete the model.
type CleanupOptions struct {
	AgentDir string
	// Enabled is true when LANCET scoring is on in this profile.
	Enabled bool
	// ConfigUnreadable is true when the settings could not be read, so Enabled is not known.
	ConfigUnreadable bool
	Now              time.Time
	Log              func(string)
}

// CleanupUnused deletes the pinned model directory when no use was recorded for more than RetentionPeriod. It
// removes nothing else, and refuses a symbolic link or a path that resolves outside the store directory. A missing
// or invalid marker is reset to now and deletes nothing. It reports whether it deleted the model.
func CleanupUnused(o CleanupOptions) bool {
	if o.Enabled || o.ConfigUnreadable {
		return false
	}
	log := o.Log
	if log == nil {
		log = func(string) {}
	}
	root, model := storeRoot(o.AgentDir), ModelDirectory(o.AgentDir)
	if info, err := os.Lstat(root); err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	if _, err := os.Lstat(model); err != nil {
		return false
	}
	used, ok := readLastUsed(root, o.Now)
	if !ok {
		if err := writeMarker(root, o.Now); err != nil {
			log(fmt.Sprintf("lancet last-used marker reset failed: %v", err))
			return false
		}
		log("lancet last-used marker missing or invalid; reset to now: " + markerPath(root))
		return false
	}
	if o.Now.Sub(used) <= RetentionPeriod {
		return false
	}
	if info, err := os.Lstat(model); err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		log("lancet model cleanup refused: not a plain directory: " + model)
		return false
	}
	realRoot, rootErr := filepath.EvalSymlinks(root)
	realModel, modelErr := filepath.EvalSymlinks(model)
	if rootErr != nil || modelErr != nil || realModel != filepath.Join(realRoot, ModelID) {
		log("lancet model cleanup refused: path resolves outside the model root: " + model)
		return false
	}
	if err := os.RemoveAll(model); err != nil {
		log(fmt.Sprintf("lancet model cleanup failed: %v", err))
		return false
	}
	log(fmt.Sprintf("lancet model deleted: %s; reason: unused for more than 7 days (last used %s)", model, used.UTC().Format(time.RFC3339)))
	return true
}
