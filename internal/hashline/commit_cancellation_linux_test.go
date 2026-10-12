//go:build linux

package hashline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Cancel just after a chosen checkpoint has observed a live request. This
// deterministically models cancellation racing the subsequent publication syscall.
type boundaryCancellation struct {
	context.Context
	cancel      context.CancelFunc
	calls       int
	cancelAfter int
}

func (request *boundaryCancellation) Err() error {
	request.calls++
	err := request.Context.Err()
	if request.calls == request.cancelAfter {
		request.cancel()
	}
	return err
}

func TestCancellationAfterFinalCheckpointReportsCommittedResult(t *testing.T) {
	for _, operation := range []string{"edit", "replace-write", "new-write"} {
		t.Run(operation, func(t *testing.T) {
			var checkpoints int
			for _, racing := range []bool{false, true} {
				dir := realTempDir(t)
				path := filepath.Join(dir, "f.txt")
				if operation != "new-write" {
					writeFile(t, path, "original\n", 0644)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				request := &boundaryCancellation{Context: ctx, cancel: cancel}
				if racing {
					request.cancelAfter = checkpoints
				}
				var result Result
				var err error
				if operation == "edit" {
					result, err = EditContext(request, dir, map[string]any{
						"path": "f.txt", "version": ContentVersion([]byte("original\n")),
						"edits": []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{"changed"}}},
					})
				} else {
					result, err = WriteContext(request, dir, map[string]any{"path": "f.txt", "content": "changed\n"})
				}
				if err != nil {
					t.Fatalf("committed operation reported failure: %v", err)
				}
				if racing && ctx.Err() != context.Canceled {
					t.Fatal("cancellation was not triggered at the final checkpoint")
				}
				checkpoints = request.calls
				content, err := os.ReadFile(path)
				if err != nil || string(content) != "changed\n" || result.Details["version"] != ContentVersion(content) {
					t.Fatalf("reported version does not describe the committed file: %v", err)
				}
				if len(tempFiles(t, dir)) != 0 {
					t.Fatal("commit left preparation artifacts")
				}
			}
		})
	}
}
