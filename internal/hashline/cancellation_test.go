package hashline

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

type cancellingRead struct {
	cancel context.CancelFunc
	cause  error
}

func (reader cancellingRead) Read(buffer []byte) (int, error) {
	reader.cancel()
	return copy(buffer, "partial"), reader.cause
}

func TestReadCancellationPreservesUnderlyingFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("read operation failed")
	content, err := readBoundedContext(ctx, cancellingRead{cancel: cancel, cause: cause}, 32)
	if content != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatalf("cancelled read returned partial content or lost an error: %v", err)
	}
}

func TestAlreadyCancelledToolsDoNotPublishResults(t *testing.T) {
	dir := realTempDir(t)
	writeFile(t, filepath.Join(dir, "f.txt"), "original\n", 0644)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, tool := range map[string]func(Cancellation, string, map[string]any) (Result, error){
		"read": ReadContext, "edit": EditContext, "write": WriteContext,
	} {
		t.Run(name, func(t *testing.T) {
			params := map[string]any{"path": "f.txt"}
			if name == "edit" {
				params["version"] = ContentVersion([]byte("original\n"))
				params["edits"] = []any{map[string]any{"op": "delete", "anchor": FormatAnchor(1, "original")}}
			}
			if name == "write" {
				params["content"] = "changed\n"
			}
			result, err := tool(ctx, dir, params)
			if !errors.Is(err, context.Canceled) || result.Text != "" || result.Details != nil {
				t.Fatalf("cancelled tool published a result: %v", err)
			}
		})
	}
}

type shortTextWriter struct{}

func (shortTextWriter) WriteString(text string) (int, error) { return len(text) - 1, nil }

func TestPreparedWritesRejectShortOutput(t *testing.T) {
	if err := writePreparedText(context.Background(), shortTextWriter{}, "text"); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short preparation write was accepted: %v", err)
	}
}

func TestCancelledParsingAndValidationReturnNoPartialFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	parsed, err := ParseFileContext(ctx, "one\ntwo\n")
	if !errors.Is(err, context.Canceled) || parsed.Lines != nil {
		t.Fatalf("cancelled parser returned a file: %v", err)
	}
	original := ParseFile("one\ntwo\n")
	result, err := ApplyEditsContext(ctx, original, []any{map[string]any{"op": "delete", "anchor": FormatAnchor(1, "one")}})
	if !errors.Is(err, context.Canceled) || result.File.Lines != nil || original.Serialize() != "one\ntwo\n" {
		t.Fatalf("cancelled validation modified or published a file: %v", err)
	}
}
