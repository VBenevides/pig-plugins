// Package sessionresume picks the session file that opening pig should resume.
package sessionresume

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// maxCandidates bounds how many recent files Latest inspects.
const maxCandidates = 20

// scanLimit is the most bytes read from a session file when looking for a message.
const scanLimit = 4 << 20

// SkipFlags are start-up options that already choose a session, or run without one.
var SkipFlags = []string{"-c", "--continue", "-r", "--resume", "--session", "--fork", "--no-session", "-p", "--print"}

// ShouldSkip reports whether the command line already decides which session to open.
func ShouldSkip(args []string) bool {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		if slices.Contains(SkipFlags, name) {
			return true
		}
	}
	return false
}

// Latest returns the newest session file in dir that holds a conversation, ignoring current. It returns ""
// when there is none. A file that cannot be read stops the search with an error naming it.
func Latest(dir, current string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return "", fmt.Errorf("list sessions in %s: %w", dir, err)
	}
	type candidate struct {
		path string
		mod  int64
	}
	var found []candidate
	for _, file := range files {
		if sameFile(file, current) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil {
			return "", fmt.Errorf("stat session %s: %w", file, err)
		}
		found = append(found, candidate{file, info.ModTime().UnixNano()})
	}
	slices.SortFunc(found, func(a, b candidate) int {
		switch {
		case a.mod > b.mod:
			return -1
		case a.mod < b.mod:
			return 1
		}
		return strings.Compare(b.path, a.path)
	})
	for _, c := range found[:min(len(found), maxCandidates)] {
		ok, err := HasMessage(c.path)
		if err != nil {
			return "", err
		}
		if ok {
			return c.path, nil
		}
	}
	return "", nil
}

func sameFile(a, b string) bool {
	if b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// HasMessage reports whether a session file has at least one message entry.
func HasMessage(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open session %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(f, 64<<10)
	read := 0
	for read < scanLimit {
		line, err := r.ReadBytes('\n')
		read += len(line)
		if bytes.Contains(line, []byte(`"type":"message"`)) {
			return true, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			return false, fmt.Errorf("read session %s: %w", path, err)
		}
	}
	return false, nil
}

// markerMaxAge is how long an automatic-resume marker stays valid. The switch takes seconds at most.
const markerMaxAge = time.Minute

// The extension process restarts when the session is replaced, so the fact that pig chose this session itself
// travels in a small marker file instead of in memory.
func markerPath(session string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(session)))
	return filepath.Join(os.TempDir(), "pig-session-resume-"+hex.EncodeToString(sum[:8]))
}

// Mark records that session is about to be opened automatically.
func Mark(session string) error {
	if err := os.WriteFile(markerPath(session), nil, 0o600); err != nil {
		return fmt.Errorf("write resume marker for %s: %w", session, err)
	}
	return nil
}

// Unmark removes the marker of session. A missing marker is not an error.
func Unmark(session string) error {
	if err := os.Remove(markerPath(session)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove resume marker for %s: %w", session, err)
	}
	return nil
}

// Consume reports whether session was opened automatically, and removes the marker. An old marker, left by an
// interrupted switch, counts as absent.
func Consume(session string, now time.Time) (bool, error) {
	path := markerPath(session)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat resume marker for %s: %w", session, err)
	}
	fresh := now.Sub(info.ModTime()) <= markerMaxAge
	return fresh, Unmark(session)
}
