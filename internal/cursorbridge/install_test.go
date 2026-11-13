package cursorbridge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func archiveFixture(t *testing.T, name string, kind byte, duplicate bool) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	count := 1
	if duplicate {
		count = 2
	}
	for i := 0; i < count; i++ {
		header := &tar.Header{Name: name, Mode: 0700, Size: 6, Typeflag: kind}
		if kind != tar.TypeReg {
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := tw.Write([]byte("binary")); err != nil {
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
	return out.Bytes()
}
func installer(t *testing.T, dir string, data []byte, expected string) (string, error) {
	t.Helper()
	return install(context.Background(), dir, "linux", expected, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func TestInstallAndReuse(t *testing.T) {
	dir := t.TempDir()
	data := archiveFixture(t, "bin/cursor-sdk-bridge", tar.TypeReg, false)
	path, err := installer(t, dir, data, digest(data))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "binary" {
		t.Fatal(string(got), err)
	}
	_, err = install(context.Background(), dir, "linux", digest(data), func(context.Context) (io.ReadCloser, error) { t.Fatal("repeated install downloaded"); return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Installed(dir, "linux"); err != nil {
		t.Fatal(err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(path)), ".cursor-install-*"))
	if len(leftovers) > 0 {
		t.Fatal(leftovers)
	}
}
func TestInstallRejectsUnsafeArchives(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		kind      byte
		duplicate bool
	}{{"../outside", tar.TypeReg, false}, {"bin/cursor-sdk-bridge", tar.TypeSymlink, false}, {"bin/cursor-sdk-bridge", tar.TypeReg, true}} {
		data := archiveFixture(t, fixture.name, fixture.kind, fixture.duplicate)
		if _, err := installer(t, t.TempDir(), data, digest(data)); err == nil {
			t.Fatal("unsafe archive installed", fixture)
		}
	}
	data := archiveFixture(t, "bin/cursor-sdk-bridge", tar.TypeReg, false)
	if _, err := installer(t, t.TempDir(), data, strings.Repeat("0", 64)); err == nil {
		t.Fatal("bad hash installed")
	}
}
func TestInstallFailurePreservesExisting(t *testing.T) {
	dir := t.TempDir()
	data := archiveFixture(t, "bin/cursor-sdk-bridge", tar.TypeReg, false)
	path, err := installer(t, dir, data, digest(data))
	if err != nil {
		t.Fatal(err)
	}
	// Corruption must be visible, never overwritten by an automatic replacement.
	if err := os.WriteFile(path, []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = installer(t, dir, data, digest(data)); err == nil {
		t.Fatal("corrupt install accepted")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "changed" {
		t.Fatal("existing install replaced")
	}
	dir = t.TempDir()
	_, err = install(context.Background(), dir, "linux", digest(data), func(context.Context) (io.ReadCloser, error) { return nil, errors.New("download interrupted") })
	if err == nil || !strings.Contains(err.Error(), "download interrupted") {
		t.Fatal(err)
	}
	if _, err = installer(t, dir, data, digest(data)); err != nil {
		t.Fatal("retry after failed download", err)
	}
}
func TestConcurrentInstall(t *testing.T) {
	dir := t.TempDir()
	data := archiveFixture(t, "bin/cursor-sdk-bridge", tar.TypeReg, false)
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := install(context.Background(), dir, "linux", digest(data), func(context.Context) (io.ReadCloser, error) {
			close(entered)
			<-release
			return io.NopCloser(bytes.NewReader(data)), nil
		})
		if err != nil {
			t.Error(err)
		}
	}()
	<-entered
	if _, err := installer(t, dir, data, digest(data)); err == nil {
		t.Error("concurrent install accepted")
	}
	close(release)
	wg.Wait()
}
func TestInstallFlag(t *testing.T) {
	for _, args := range [][]string{nil, {"prompt"}, {"--model", "a", "prompt"}} {
		yes, err := InstallFlag(args)
		if yes || err != nil {
			t.Fatal(args, yes, err)
		}
	}
	if yes, err := InstallFlag([]string{"--install-cursor"}); !yes || err != nil {
		t.Fatal(yes, err)
	}
	for _, args := range [][]string{{"--install-cursor", "prompt"}, {"prompt", "--install-cursor"}, {"--install-cursor=yes"}} {
		if _, err := InstallFlag(args); err == nil {
			t.Fatal(args)
		}
	}
	if _, err := Install(context.Background(), t.TempDir(), "plan9", "mips"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
}
