//go:build linux

package hashline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cancelOnPreparedBytes struct {
	context.Context
	cancel    context.CancelFunc
	directory string
	observed  int64
}

func (request *cancelOnPreparedBytes) Err() error {
	if err := request.Context.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(request.directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.Contains(entry.Name(), "pig-plugins") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() >= cancellationChunkBytes {
			request.observed = info.Size()
			request.cancel()
			return request.Context.Err()
		}
	}
	return nil
}

func TestCancellationDuringPreparationPreservesTargets(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-write", true: "existing-edit"}[existing], func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			if existing {
				writeFile(t, path, "original\n", 0644)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := &cancelOnPreparedBytes{Context: ctx, cancel: cancel, directory: dir}
			payload := strings.Repeat("x", cancellationChunkBytes*2+1)
			var err error
			if existing {
				_, err = EditContext(request, dir, map[string]any{"path": "f.txt", "version": ContentVersion([]byte("original\n")), "edits": []any{
					map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{payload}},
				}})
			} else {
				_, err = WriteContext(request, dir, map[string]any{"path": "f.txt", "content": payload})
			}
			if !errors.Is(err, context.Canceled) || request.observed != cancellationChunkBytes {
				t.Fatalf("preparation did not cancel after a bounded partial chunk: %d, %v", request.observed, err)
			}
			content, err := os.ReadFile(path)
			if existing && (err != nil || string(content) != "original\n") {
				t.Fatalf("cancelled preparation modified the original: %v", err)
			}
			if !existing && !os.IsNotExist(err) {
				t.Fatal("cancelled new write published partial content")
			}
			if len(tempFiles(t, dir)) != 0 {
				t.Fatal("cancelled preparation left temporary files")
			}
		})
	}
}

func TestCancelledMutationWaiterReleasesItsRegistration(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0644)
	unlock := lockPath(path)
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := WriteContext(ctx, dir, map[string]any{"path": "f.txt", "content": "changed\n"})
		done <- err
	}()
	queued := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		editLocks.Lock()
		queued = editLocks.held[path].holders == 2
		editLocks.Unlock()
		if queued {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !queued {
		t.Fatal("writer did not join the coordinator")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lock wait did not preserve cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter remained blocked on the held lock")
	}
	editLocks.Lock()
	holders := editLocks.held[path].holders
	editLocks.Unlock()
	if holders != 1 {
		t.Fatalf("cancelled waiter leaked registration: %d holders", holders)
	}
	unlock()
	editLocks.Lock()
	_, retained := editLocks.held[path]
	editLocks.Unlock()
	if retained {
		t.Fatal("released mutation lock was retained")
	}
}

func TestCancellationImmediatelyBeforeCommitPreservesOriginal(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0751)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = ReplaceAtomicallyContext(ctx, snapshot, "changed\n", cancel)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-commit cancellation was ignored: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original\n" || !os.SameFile(snapshot.info, after) || snapshot.info.Mode() != after.Mode() || len(tempFiles(t, dir)) != 0 {
		t.Fatal("pre-commit cancellation changed target content/metadata or left artifacts")
	}
}
