//go:build linux

package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestExtendedAttributesRejectReplacement(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0755)
	if err := syscall.Setxattr(path, "user.hashline-test", []byte("metadata-value"), 0); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	version := readVersion(t, dir, "f.txt")
	_, err = Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{"changed"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "extended attributes") || strings.Contains(err.Error(), "metadata-value") {
		t.Fatalf("expected non-sensitive metadata rejection, got %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	attribute := make([]byte, 32)
	count, err := syscall.Getxattr(path, "user.hashline-test", attribute)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || after.Mode() != before.Mode() || string(content) != "original\n" || string(attribute[:count]) != "metadata-value" {
		t.Fatal("metadata rejection modified the target")
	}
}

func TestReplacementPreservesOwnershipAndExecutableMode(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0751)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAtomically(snapshot, "changed\n", nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0751 || !sameOwnership(snapshot.info, after) {
		t.Fatal("replacement lost ownership or executable permissions")
	}
	label, err := checkSupportedMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(label) != string(snapshot.securityLabel) {
		t.Fatal("replacement did not retain the exact inherited label")
	}
}

func TestReplacementRejectsSecurityLabelMismatch(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0644)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.securityLabel = []byte("different-private-label")
	err = ReplaceAtomically(snapshot, "changed\n", nil)
	if err == nil || !strings.Contains(err.Error(), "security label") || strings.Contains(err.Error(), "different-private-label") {
		t.Fatalf("expected label mismatch without leaking its value: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original\n" {
		t.Fatal("label mismatch changed target content")
	}
}
