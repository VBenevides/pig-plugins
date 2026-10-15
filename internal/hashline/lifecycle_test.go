package hashline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLifecycleInvalidationRequiresFreshReadEvenInSameSession(t *testing.T) {
	dir := realTempDir(t)
	engine := NewEngine()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "one\n", 0644)
	read := engineRead(t, engine, dir, "f")
	engine.Invalidate()
	if _, err := engineDelete(engine, dir, "f", read.Details["version"].(string)); err == nil {
		t.Fatal("tree navigation retained read authority")
	}
	engineRead(t, engine, dir, "f")
	if _, err := engineDelete(engine, dir, "f", read.Details["version"].(string)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "one\n", 0644)
	if _, err := engineDelete(NewEngine(), dir, "f", read.Details["version"].(string)); err == nil {
		t.Fatal("reload/restart inherited read authority")
	}
}

func TestLifecycleInvalidationAbortsPreparedMutationBeforeCommit(t *testing.T) {
	dir := realTempDir(t)
	engine := NewEngine()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "one\n", 0644)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	request := lifecycleCancellation{Cancellation: context.Background(), engine: engine, generation: engine.epoch.Load()}
	err = ReplaceAtomicallyContext(request, snapshot, "changed\n", engine.Invalidate)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lifecycle revocation did not abort preparation: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "one\n" || len(tempFiles(t, dir)) != 0 {
		t.Fatal("revoked preparation changed content or leaked artifacts")
	}
}
