//go:build linux

package hashline

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteCreatesPrivateFileAndIsIdempotent(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "nested", "f.txt")
	result, err := Write(dir, map[string]any{"path": "nested/f.txt", "content": "created\n"})
	if err != nil || result.Details["version"] != ContentVersion([]byte("created\n")) {
		t.Fatalf("create/version: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0600 {
		t.Fatalf("new file must be private: %v", before.Mode())
	}
	result, err = Write(dir, map[string]any{"path": "nested/f.txt", "content": "created\n"})
	if err != nil || result.Details["changed"] != false {
		t.Fatalf("identical write: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || len(tempFiles(t, filepath.Dir(path))) != 0 {
		t.Fatal("identical write changed metadata or left preparation artifacts")
	}
}

func TestWriteSharesEditCoordinatorThroughAliases(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	alias := filepath.Join(dir, "alias.txt")
	writeFile(t, path, "original\n", 0644)
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	version := readVersion(t, dir, "f.txt")
	unlock := lockPath(path)
	var once sync.Once
	release := func() { once.Do(unlock) }
	done := make(chan struct{})
	var writeErr error
	go func() {
		_, writeErr = Write(dir, map[string]any{"path": "alias.txt", "content": "explicit overwrite\n"})
		close(done)
	}()
	t.Cleanup(func() {
		release()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("coordinated writer did not release")
		}
	})
	queued := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		editLocks.Lock()
		entry := editLocks.held[path]
		queued = entry != nil && entry.holders == 2
		editLocks.Unlock()
		if queued {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !queued {
		t.Fatal("alias writer did not join the edit's canonical queue")
	}
	if _, err := editLocked(dir, path, version, []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{"edited"}}}, nil); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "edited\n" {
		t.Fatalf("queued write overlapped edit: %v", err)
	}
	release()
	select {
	case <-done:
		if writeErr != nil {
			t.Fatal(writeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("queued write never completed")
	}
	_, err = Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{"stale"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "stale version") {
		t.Fatalf("pre-write version was accepted: %v", err)
	}
	content, err = os.ReadFile(path)
	if err != nil || string(content) != "explicit overwrite\n" {
		t.Fatalf("write ordering/stale rejection lost content: %v", err)
	}
}

func TestNewWriteParentAliasesShareCanonicalTargetAndRejectEscape(t *testing.T) {
	dir := realTempDir(t)
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalMutationPath(filepath.Join(dir, "alias", "nested", "f.txt"))
	if err != nil || canonical != filepath.Join(real, "nested", "f.txt") {
		t.Fatalf("missing-file canonical target: %s, %v", canonical, err)
	}
	if _, err := Write(dir, map[string]any{"path": "alias/nested/f.txt", "content": "new\n"}); err != nil {
		t.Fatal(err)
	}
	outside := realTempDir(t)
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, map[string]any{"path": "escape/missing/f.txt", "content": "unsafe\n"}); err == nil {
		t.Fatal("new write accepted a parent-directory symlink escape")
	}
	if _, err := os.Stat(filepath.Join(outside, "missing")); !os.IsNotExist(err) {
		t.Fatal("escaped write created directories before rejection")
	}
	// Ordinary external paths remain available for the host guard to authorize.
	if _, err := Write(dir, map[string]any{"path": filepath.Join(outside, "normal.txt"), "content": "authorized externally\n"}); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRejectsReadOnlyTargetAndNonTextInput(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0444)
	if _, err := Write(dir, map[string]any{"path": "f.txt", "content": "changed\n"}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("read-only write: %v", err)
	}
	for _, payload := range []string{"NUL\x00", string([]byte{0xff}), strings.Repeat("x", MaxEditBytes+1)} {
		if _, err := Write(dir, map[string]any{"path": "missing.txt", "content": payload}); err == nil {
			t.Fatal("unsafe/oversized new write was accepted")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.txt")); !os.IsNotExist(err) {
		t.Fatal("invalid write created the target")
	}
}

func TestNewFilePublicationDoesNotOverwriteAnotherCreator(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "other creator\n", 0644)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := createTextFile(path, "ours\n"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("exclusive publication must reject an existing destination: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "other creator\n" || !os.SameFile(before, after) || len(tempFiles(t, dir)) != 0 {
		t.Fatal("failed new-file publication overwrote the destination or left artifacts")
	}
}
