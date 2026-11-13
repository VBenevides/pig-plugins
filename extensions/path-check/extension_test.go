package pathcheck

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestPathHintPreservesExactPaths(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"file.txt", " exact.txt ", "internal space.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("KEEP"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"file.txt", " exact.txt ", "internal space.txt", " missing ", "   ", "", " ~/file.txt ", " @file.txt ", " file:///file.txt "} {
		if reason := pathHint(path, root, os.Stat); reason != "" {
			t.Fatalf("unexpected suggestion for %q: %s", path, reason)
		}
	}
	for _, path := range []string{" file.txt", "file.txt ", "\tfile.txt\n", " internal space.txt ", " " + filepath.Join(root, "file.txt") + " "} {
		reason := pathHint(path, root, os.Stat)
		if !strings.Contains(reason, "Retry with that exact path") || !strings.Contains(reason, "not changed automatically") {
			t.Fatalf("missing suggestion for %q: %s", path, reason)
		}
	}
}

func TestOnlyMissingPathsTriggerFallback(t *testing.T) {
	for _, originalError := range []error{nil, os.ErrPermission, errors.New("I/O failure")} {
		calls := 0
		reason := pathHint(" file ", "/workspace", func(string) (os.FileInfo, error) { calls++; return nil, originalError })
		if reason != "" || calls != 1 {
			t.Fatalf("error=%v reason=%s calls=%d", originalError, reason, calls)
		}
	}
	calls := 0
	reason := pathHint(" file ", "/workspace", func(string) (os.FileInfo, error) {
		calls++
		if calls == 1 {
			return nil, os.ErrNotExist
		}
		return nil, os.ErrPermission
	})
	if reason != "" || calls != 2 {
		t.Fatal("candidate failure was treated as a match")
	}
}

func TestWriteKeepsSurroundingWhitespace(t *testing.T) {
	pigtest.RequirePig(t)
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("write", map[string]any{"path": " new.txt ", "content": "EXACT"})), pigtest.Text("done"))
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extension}, Prompt: "write test"})
	if result.ExitCode != 0 {
		t.Fatalf("session failed: %s", result.Stderr)
	}
	data, err := os.ReadFile(filepath.Join(home.Work, " new.txt "))
	if err != nil || string(data) != "EXACT" {
		t.Fatalf("exact write failed: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(home.Work, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("trimmed write path was used: %v", err)
	}
}

func TestNativeReadAndEditReportSuggestedPathWithoutMutation(t *testing.T) {
	pigtest.RequirePig(t)
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"read", "edit"} {
		t.Run(tool, func(t *testing.T) {
			home := pigtest.NewHome(t)
			target := filepath.Join(home.Work, "file.txt")
			if err := os.WriteFile(target, []byte("KEEP"), 0600); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"path": " file.txt "}
			if tool == "edit" {
				input["edits"] = []any{map[string]any{"oldText": "KEEP", "newText": "WRONG"}}
			}
			mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call(tool, input)), pigtest.Text("done"))
			defer mock.Close()
			result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extension}, Prompt: "test path"})
			if result.ExitCode != 0 {
				t.Fatalf("session failed: %s", result.Stderr)
			}
			requests := mock.Requests()
			if len(requests) != 2 {
				t.Fatalf("requests=%d", len(requests))
			}
			found := false
			for _, raw := range requests[1]["messages"].([]any) {
				message := raw.(map[string]any)
				if message["role"] == "tool" {
					content, _ := message["content"].(string)
					if strings.Contains(content, "Possible match") && strings.Contains(content, `"file.txt"`) {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("model did not receive the suggested path")
			}
			actual, err := os.ReadFile(target)
			if err != nil || string(actual) != "KEEP" {
				t.Fatalf("file changed: %q %v", actual, err)
			}
		})
	}
}
