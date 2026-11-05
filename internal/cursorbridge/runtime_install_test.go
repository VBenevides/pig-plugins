package cursorbridge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type runtimeEntry struct {
	name, body string
	kind       byte
}

func runtimeArchive(t *testing.T, entries ...runtimeEntry) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: 0700, Typeflag: kind, Size: int64(len(entry.body))}
		if kind == tar.TypeSymlink {
			header.Linkname = "outside"
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := io.WriteString(tw, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(buf.Bytes())
	return buf.Bytes(), base64.StdEncoding.EncodeToString(sum[:])
}

func TestRuntimeInstallPreservesBridgeAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	bridge := filepath.Join(dir, "cursor-sdk-bridge")
	if err := os.WriteFile(bridge, []byte("existing bridge"), 0700); err != nil {
		t.Fatal(err)
	}
	data, hash := runtimeArchive(t, runtimeEntry{name: "package/bin/cursorsandbox", body: "sandbox"}, runtimeEntry{name: "package/bin/rg", body: "rg"}, runtimeEntry{name: "package/vendor/parser.js", body: "parser"})
	calls := 0
	fetch := func(context.Context) (io.ReadCloser, error) { calls++; return io.NopCloser(bytes.NewReader(data)), nil }
	for i := 0; i < 2; i++ {
		if err := installRuntime(context.Background(), dir, "linux-x64", hash, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("downloaded %d times", calls)
	}
	got, err := os.ReadFile(bridge)
	if err != nil || string(got) != "existing bridge" {
		t.Fatal("bridge was replaced", err)
	}
	root := filepath.Join(dir, "node_modules/@cursor/sdk-linux-x64")
	if err := checkRuntime(root, hash); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin/cursorsandbox"), []byte("corrupt"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := installRuntime(context.Background(), dir, "linux-x64", hash, fetch); err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatal("corrupt runtime was accepted", err)
	}
	if calls != 1 {
		t.Fatal("corrupt installation was overwritten")
	}
}

func TestRuntimeInstallRejectsUnsafeAndIncompleteArchives(t *testing.T) {
	for name, extra := range map[string]runtimeEntry{
		"traversal": {name: "package/../../escape", body: "x"},
		"absolute":  {name: "/absolute", body: "x"},
		"backslash": {name: "package/..\\escape", body: "x"},
		"symlink":   {name: "package/bin/link", kind: tar.TypeSymlink},
		"duplicate": {name: "package/bin/rg", body: "x"},
		"record":    {name: "package/" + runtimeRecord, body: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			data, hash := runtimeArchive(t, runtimeEntry{name: "package/bin/rg", body: "rg"}, runtimeEntry{name: "package/bin/cursorsandbox", body: "sandbox"}, extra)
			err := installRuntime(context.Background(), dir, "linux-x64", hash, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
			if err == nil {
				t.Fatal("unsafe archive accepted")
			}
			if _, err := os.Lstat(filepath.Join(dir, "node_modules/@cursor/sdk-linux-x64")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("partial runtime became visible", err)
			}
		})
	}
	t.Run("missing helper", func(t *testing.T) {
		dir := t.TempDir()
		data, hash := runtimeArchive(t, runtimeEntry{name: "package/bin/rg", body: "rg"})
		err := installRuntime(context.Background(), dir, "linux-x64", hash, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
		if err == nil || !strings.Contains(err.Error(), "cursorsandbox") {
			t.Fatal("missing helper accepted", err)
		}
	})
}

func TestRuntimeInstallRetriesOnlyAfterExplicitNewCall(t *testing.T) {
	dir := t.TempDir()
	data, hash := runtimeArchive(t, runtimeEntry{name: "package/bin/rg", body: "rg"}, runtimeEntry{name: "package/bin/cursorsandbox", body: "sandbox"})
	calls := 0
	fetch := func(context.Context) (io.ReadCloser, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("download interrupted")
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	if err := installRuntime(context.Background(), dir, "linux-x64", hash, fetch); err == nil {
		t.Fatal("failure hidden")
	}
	if calls != 1 {
		t.Fatal("download retried implicitly")
	}
	if err := installRuntime(context.Background(), dir, "linux-x64", hash, fetch); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeInstallRejectsWrongDigestAndSymlinkParent(t *testing.T) {
	dir := t.TempDir()
	data, _ := runtimeArchive(t, runtimeEntry{name: "package/bin/rg", body: "rg"})
	err := installRuntime(context.Background(), dir, "linux-x64", "wrong", func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	if err == nil || !strings.Contains(err.Error(), "pinned integrity") {
		t.Fatal("unverified archive accepted", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	linked := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(linked, "node_modules")); err != nil {
		t.Fatal(err)
	}
	err = installRuntime(context.Background(), linked, "linux-x64", "wrong", func(context.Context) (io.ReadCloser, error) {
		t.Fatal("downloaded through symlink parent")
		return nil, nil
	})
	if err == nil {
		t.Fatal("symlink parent accepted")
	}
}

func TestRuntimePlatformMappingsHavePinnedIntegrity(t *testing.T) {
	for _, tc := range []struct{ os, arch string }{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}} {
		hash := runtimeHashes[runtimePlatform(tc.os, tc.arch)]
		digest, err := base64.StdEncoding.DecodeString(hash)
		if err != nil || len(digest) != sha512.Size {
			t.Fatalf("invalid pin for %s/%s", tc.os, tc.arch)
		}
	}
}

func TestRuntimeInstallChecksWindowsHelpers(t *testing.T) {
	data, hash := runtimeArchive(t, runtimeEntry{name: "package/bin/rg.exe", body: "rg"}, runtimeEntry{name: "package/bin/cursorsandbox.exe", body: "sandbox"})
	dir := t.TempDir()
	if err := installRuntime(context.Background(), dir, "win32-x64", hash, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeInstallBoundsDeclaredSizeAndEntryCount(t *testing.T) {
	// Deliberately omit file data and tar footer. The size bound must reject the
	// header before attempting to read the missing oversized payload.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "package/large", Mode: 0600, Size: maxRuntimeContent + 1}); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(buf.Bytes())
	err := installRuntime(context.Background(), t.TempDir(), "linux-x64", base64.StdEncoding.EncodeToString(sum[:]), func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(buf.Bytes())), nil })
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatal("oversized entry accepted", err)
	}
	entries := make([]runtimeEntry, 129)
	for i := range entries {
		entries[i] = runtimeEntry{name: "package/file-" + strings.Repeat("x", i+1), body: "x"}
	}
	data, hash := runtimeArchive(t, entries...)
	err = installRuntime(context.Background(), t.TempDir(), "linux-x64", hash, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	if err == nil || !strings.Contains(err.Error(), "too many entries") {
		t.Fatal("unbounded entry count", err)
	}
}
