//go:build linux

package hashline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplacementSupportsLongDestinationNames(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, strings.Repeat("f", 240))
	writeFile(t, path, "original\n", 0644)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAtomically(snapshot, "changed\n", nil); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "changed\n" {
		t.Fatalf("replacement failed: %v", err)
	}
	if len(tempFiles(t, dir)) != 0 {
		t.Fatal("replacement left temporary artifacts")
	}
}

func TestSimultaneousPreparationDoesNotOverwriteOtherArtifacts(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0644)
	sentinel := filepath.Join(dir, ".pig-plugins-edit-existing.tmp")
	writeFile(t, sentinel, "unrelated", 0600)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	prepared := make(chan struct{}, 2)
	commit := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- ReplaceAtomically(snapshot, "changed\n", func() { prepared <- struct{}{}; <-commit }) }()
	}
	<-prepared
	<-prepared
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("preparations did not receive isolated files: %d entries", len(entries))
	}
	close(commit)
	successes := 0
	for range 2 {
		err := <-done
		if err == nil {
			successes++
			continue
		}
		var rejected *EditError
		if !errors.As(err, &rejected) {
			t.Fatalf("unexpected preparation failure: %v", err)
		}
	}
	if successes == 0 {
		t.Fatal("neither isolated preparation committed")
	}
	content, err := os.ReadFile(sentinel)
	if err != nil || string(content) != "unrelated" {
		t.Fatalf("preparation overwrote an existing artifact: %v", err)
	}
	entries, err = os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("preparation artifacts were not cleaned: %v", err)
	}
}
