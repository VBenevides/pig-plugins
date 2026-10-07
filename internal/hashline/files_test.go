package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var pngHeader = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R'}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func tempFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConcurrentModificationBeforeRenameIsRejected(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "one\ntwo\n", 0o644)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	err = ReplaceAtomically(snapshot, "ONE\ntwo\n", func() { writeFile(t, path, "someone else\n", 0o644) })
	if err == nil || !strings.Contains(err.Error(), "modified by someone else") {
		t.Fatalf("want rejection, got %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "someone else\n" {
		t.Fatalf("the other writer's content was overwritten: %q", got)
	}
	if leftovers := tempFiles(t, dir); len(leftovers) > 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
}

func TestFileDeletedBeforeRenameIsRejected(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "one\n", 0o644)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	err = ReplaceAtomically(snapshot, "ONE\n", func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "changed while it was being edited (ENOENT)") {
		t.Fatalf("want ENOENT rejection, got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("file must stay deleted: %v", statErr)
	}
	if leftovers := tempFiles(t, dir); len(leftovers) > 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
}

func TestCleanReplaceKeepsModeAndLeavesNoTemporaryFile(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "run.sh")
	writeFile(t, path, "echo a\n", 0o750)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAtomically(snapshot, "echo b\n", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "echo b\n" {
		t.Fatalf("content %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("mode %v, want 0750", info.Mode().Perm())
	}
	if leftovers := tempFiles(t, dir); len(leftovers) > 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
}

func TestEditRefusesUnsafeTargets(t *testing.T) {
	dir := realTempDir(t)
	outside := filepath.Join(realTempDir(t), "outside.txt")
	writeFile(t, outside, "secret\n", 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "blob.bin"), "a\x00b\n", 0o644)
	writeFile(t, filepath.Join(dir, "latin1.txt"), "caf\xe9\n", 0o644)
	ops := []any{map[string]any{"op": "delete", "anchor": "1#" + LineHash("x")}}
	for _, c := range []struct{ file, want string }{
		{"escape", "outside the working directory"},
		{"dangling", "broken symlink"},
		{"sub", "is a directory"},
		{"blob.bin", "binary or not valid UTF-8"},
		{"latin1.txt", "binary or not valid UTF-8"},
		{"nope.txt", "The file must exist"},
	} {
		_, err := Edit(dir, map[string]any{"path": c.file, "edits": ops})
		if err == nil || !strings.HasPrefix(err.Error(), "edit rejected: ") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want rejection containing %q, got %v", c.file, c.want, err)
		}
	}
	if got, _ := os.ReadFile(outside); string(got) != "secret\n" {
		t.Fatalf("file outside the working directory changed: %q", got)
	}
}

func TestSymlinkInsideWorkingDirectoryIsEditedAtItsTarget(t *testing.T) {
	dir := realTempDir(t)
	target := filepath.Join(dir, "real.txt")
	writeFile(t, target, "x\n", 0o644)
	if err := os.Symlink("real.txt", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	_, err := Edit(dir, map[string]any{"path": "alias", "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(1, "x"), "lines": []any{"y"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dir, "alias"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("alias must stay a symlink: %v %v", info, err)
	}
	if got, _ := os.ReadFile(target); string(got) != "y\n" {
		t.Fatalf("target content %q", got)
	}
}

func TestConcurrentEditsOfOneFileAreSerialisedAndLoseNothing(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	const lines = 20
	var original strings.Builder
	for i := range lines {
		original.WriteString("line" + string(rune('a'+i)) + "\n")
	}
	writeFile(t, path, original.String(), 0o644)
	var wg sync.WaitGroup
	errs := make([]error, lines)
	for i := range lines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			text := "line" + string(rune('a'+i))
			_, errs[i] = Edit(dir, map[string]any{"path": "f.txt", "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(i+1, text), "lines": []any{strings.ToUpper(text)}},
			}})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("edit %d: %v", i, err)
		}
	}
	got, _ := os.ReadFile(path)
	want := strings.ToUpper(original.String())
	if string(got) != want {
		t.Fatalf("lost update\nwant: %q\n got: %q", want, got)
	}
	if leftovers := tempFiles(t, dir); len(leftovers) > 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
}

func TestReadOfNonTextFiles(t *testing.T) {
	dir := realTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "dot.png"), pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "blob.bin"), "a\x00b\n", 0o644)
	writeFile(t, filepath.Join(dir, "latin1.txt"), "caf\xe9\nsecond\n", 0o644)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	image, err := Read(dir, map[string]any{"path": "dot.png"})
	if err != nil || image.Text != "Read image file [image/png]" || len(image.Images) != 1 || image.Images[0].MimeType != "image/png" {
		t.Fatalf("image: %+v %v", image, err)
	}
	if _, err := Read(dir, map[string]any{"path": "blob.bin"}); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("binary: %v", err)
	}
	plain, err := Read(dir, map[string]any{"path": "latin1.txt"})
	if err != nil || strings.Contains(plain.Text, "#") && strings.Contains(plain.Text, "|") || !strings.Contains(plain.Text, "caf\ufffd\nsecond") {
		t.Fatalf("non-UTF-8 text: %q %v", plain.Text, err)
	}
	if _, err := Read(dir, map[string]any{"path": "sub"}); err == nil || !strings.Contains(err.Error(), "EISDIR") {
		t.Fatalf("directory: %v", err)
	}
	if _, err := Read(dir, map[string]any{"path": "absent.txt"}); err == nil || !strings.Contains(err.Error(), "absent.txt") || !strings.Contains(err.Error(), "ENOENT") {
		t.Fatalf("missing: %v", err)
	}
}
