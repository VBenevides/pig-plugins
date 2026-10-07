package lancet

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// fakeModel is a tiny stand-in release: two files, a pinned archive built from them, and its pins.
type fakeModel struct {
	files   map[string][]byte
	archive []byte
	pins    ModelPins
}

func newFakeModel(t *testing.T, mutate func(entries map[string][]byte)) fakeModel {
	t.Helper()
	files := map[string][]byte{"a.bin": []byte("alpha-weights"), "b.json": []byte(`{"b":1}`)}
	pins := ModelPins{Prefix: "rel/model/", Files: map[string]Artifact{}}
	for name, data := range files {
		pins.Files[name] = Artifact{Bytes: int64(len(data)), SHA256: sum(data)}
	}
	entries := map[string][]byte{}
	for name, data := range files {
		entries[pins.Prefix+name] = data
	}
	entries["rel/other.txt"] = []byte("ignored")
	if mutate != nil {
		mutate(entries)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	pins.Bytes, pins.SHA256 = int64(buf.Len()), sum(buf.Bytes())
	return fakeModel{files: files, archive: buf.Bytes(), pins: pins}
}

// serve returns a TLS test server (always https) that answers every request with body, and the client that
// trusts it. hits counts requests.
func serve(t *testing.T, body []byte, handler func(w http.ResponseWriter, r *http.Request)) (url string, client *http.Client, hits *int) {
	t.Helper()
	count := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if handler != nil {
			handler(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/model.zip", srv.Client(), &count
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestInstallModelDownloadsVerifiesAndSwapsAtomically(t *testing.T) {
	m := newFakeModel(t, nil)
	url, client, hits := serve(t, m.archive, nil)
	m.pins.URL = url
	agent := t.TempDir()
	opts := InstallOptions{Client: client, Model: &m.pins}

	res, err := InstallModel(context.Background(), agent, opts)
	if err != nil {
		t.Fatal(err)
	}
	dir := ModelDirectory(agent)
	if res.Reason != "downloaded" || res.Directory != dir {
		t.Fatalf("result = %+v", res)
	}
	if got := listDir(t, dir); len(got) != 2 {
		t.Errorf("model dir has %v, want exactly the two pinned files", got)
	}
	for name, data := range m.files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("%s = %q, %v", name, got, err)
		}
		if info, _ := os.Stat(filepath.Join(dir, name)); info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", name, info.Mode().Perm())
		}
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v, want 0700", info.Mode().Perm())
	}
	if leftovers := listDir(t, filepath.Dir(dir)); len(leftovers) != 1 {
		t.Errorf("staging or retired directories left behind: %v", leftovers)
	}
	if !modelVerified(dir, m.pins.Files) {
		t.Error("installed model does not verify")
	}

	// A verified install needs no network.
	res, err = InstallModel(context.Background(), agent, opts)
	if err != nil || res.Reason != "already-current" || *hits != 1 {
		t.Errorf("second install: %+v, %v, hits=%d", res, err, *hits)
	}
}

func TestInstallModelRefusesBadDownloadsAndLeavesNothing(t *testing.T) {
	good := newFakeModel(t, nil)
	tests := []struct {
		name    string
		handler func(w http.ResponseWriter, r *http.Request)
		mutate  func(p *ModelPins)
		want    string
	}{
		{name: "HTTP error", handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", 503) }, want: "HTTP 503"},
		{name: "wrong checksum", handler: func(w http.ResponseWriter, _ *http.Request) {
			data := append([]byte(nil), good.archive...)
			data[len(data)/2] ^= 0xff
			_, _ = w.Write(data)
		}, want: "failed its checksum"},
		{name: "larger than pinned", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(append(append([]byte(nil), good.archive...), "extra"...))
		}, want: "wrong size"},
		{name: "truncated", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(good.archive[:len(good.archive)-1])
		}, want: "wrong size"},
		{name: "declared size differs", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "5")
			_, _ = w.Write([]byte("short"))
		}, want: "wrong size"},
		{name: "redirect off https", handler: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://127.0.0.1:1/model.zip", http.StatusFound)
		}, want: "redirected off HTTPS"},
		{name: "plain http url", handler: nil, mutate: func(p *ModelPins) { p.URL = "http://example.invalid/model.zip" }, want: "not HTTPS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, client, _ := serve(t, good.archive, tt.handler)
			pins := good.pins
			pins.URL = url
			if tt.mutate != nil {
				tt.mutate(&pins)
			}
			agent := t.TempDir()
			_, err := InstallModel(context.Background(), agent, InstallOptions{Client: client, Model: &pins})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			if _, statErr := os.Lstat(ModelDirectory(agent)); statErr == nil {
				t.Error("a model directory exists after a failed install")
			}
			if left := listDir(t, filepath.Join(agent, "smart-approve-lancet")); len(left) != 0 {
				t.Errorf("staging left behind: %v", left)
			}
		})
	}
}

func TestInstallModelRefusesBadArchiveContents(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(entries map[string][]byte)
		want   string
	}{
		{name: "entry with wrong content, right size", mutate: func(e map[string][]byte) { e["rel/model/a.bin"] = []byte("ALPHA-weights") }, want: "failed its checksum"},
		{name: "entry with wrong size", mutate: func(e map[string][]byte) { e["rel/model/a.bin"] = []byte("alpha-weights!") }, want: "wrong size"},
		{name: "missing entry", mutate: func(e map[string][]byte) { delete(e, "rel/model/b.json") }, want: "missing b.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The pins describe the good files; only the archive is altered, then re-pinned so it passes the
			// archive checksum and the entry checks are what must catch it.
			pins := newFakeModel(t, nil).pins
			bad := newFakeModel(t, tt.mutate)
			pins.Bytes, pins.SHA256 = bad.pins.Bytes, bad.pins.SHA256
			url, client, _ := serve(t, bad.archive, nil)
			pins.URL = url
			agent := t.TempDir()
			_, err := InstallModel(context.Background(), agent, InstallOptions{Client: client, Model: &pins})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			if _, statErr := os.Lstat(ModelDirectory(agent)); statErr == nil {
				t.Error("a model directory exists after a failed install")
			}
		})
	}
}

func TestFailedInstallKeepsTheExistingDamagedDirectoryUntouched(t *testing.T) {
	m := newFakeModel(t, nil)
	url, client, _ := serve(t, m.archive, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", 500) })
	m.pins.URL = url
	agent := t.TempDir()
	dir := ModelDirectory(agent)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallModel(context.Background(), agent, InstallOptions{Client: client, Model: &m.pins}); err == nil {
		t.Fatal("install succeeded")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.bin")); string(got) != "damaged" {
		t.Errorf("existing directory was modified: %q", got)
	}
}

func TestInstallReplacesADamagedModel(t *testing.T) {
	m := newFakeModel(t, nil)
	url, client, _ := serve(t, m.archive, nil)
	m.pins.URL = url
	agent := t.TempDir()
	dir := ModelDirectory(agent)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res, err := InstallModel(context.Background(), agent, InstallOptions{Client: client, Model: &m.pins}); err != nil || res.Reason != "downloaded" {
		t.Fatalf("install: %+v, %v", res, err)
	}
	if !modelVerified(dir, m.pins.Files) {
		t.Error("model not repaired")
	}
	if left := listDir(t, filepath.Dir(dir)); len(left) != 1 {
		t.Errorf("retired directory left behind: %v", left)
	}
}

func TestInstallRefusesASymlinkedParent(t *testing.T) {
	m := newFakeModel(t, nil)
	url, client, hits := serve(t, m.archive, nil)
	m.pins.URL = url
	agent := t.TempDir()
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(agent, "smart-approve-lancet")); err != nil {
		t.Fatal(err)
	}
	_, err := InstallModel(context.Background(), agent, InstallOptions{Client: client, Model: &m.pins})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") || *hits != 0 {
		t.Errorf("error = %v, hits=%d", err, *hits)
	}
	if got := listDir(t, elsewhere); len(got) != 0 {
		t.Errorf("wrote through the symlink: %v", got)
	}
}

func TestModelStateAndVerification(t *testing.T) {
	files := map[string]Artifact{"a.bin": {Bytes: 3, SHA256: sum([]byte("abc"))}}
	dir := filepath.Join(t.TempDir(), "model")
	if got := modelState(dir, files); got.Installed || got.Problem != "not downloaded" {
		t.Errorf("absent: %+v", got)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := modelState(dir, files); got.Installed || got.Problem != "a.bin is missing" {
		t.Errorf("missing file: %+v", got)
	}
	file := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(file, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := modelState(dir, files); got.Installed || got.Problem != "a.bin is damaged" {
		t.Errorf("wrong size: %+v", got)
	}
	if err := os.WriteFile(file, []byte("abd"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := modelState(dir, files); !st.Installed || modelVerified(dir, files) {
		t.Errorf("same size, other bytes must be present but unverified: %+v", st)
	}
	if err := os.WriteFile(file, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !modelVerified(dir, files) {
		t.Error("good file does not verify")
	}
	// A symlink in place of the file is never accepted.
	other := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(other, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, file); err != nil {
		t.Fatal(err)
	}
	if got := modelState(dir, files); got.Installed {
		t.Errorf("symlinked artifact accepted: %+v", got)
	}
}

func fakeRuntime(t *testing.T, library []byte) ([]byte, RuntimePins) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{{"ort-1/include/x.h", []byte("h")}, {"ort-1/lib/libort.so.1", library}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), RuntimePins{Bytes: int64(buf.Len()), SHA256: sum(buf.Bytes()), Entry: "ort-1/lib/libort.so.1", Name: "libort.so.1",
		Library: Artifact{Bytes: int64(len(library)), SHA256: sum(library)}}
}

func TestInstallRuntimeExtractsTheOnePinnedLibrary(t *testing.T) {
	archive, pins := fakeRuntime(t, []byte("ELF-not-really"))
	url, client, _ := serve(t, archive, nil)
	pins.URL = url
	agent := t.TempDir()
	res, err := InstallRuntime(context.Background(), agent, InstallOptions{Client: client, Runtime: &pins})
	if err != nil || res.Reason != "downloaded" {
		t.Fatalf("install: %+v, %v", res, err)
	}
	if got := listDir(t, RuntimeDirectory(agent)); len(got) != 1 || got[0] != "libort.so.1" {
		t.Errorf("runtime dir = %v", got)
	}
	path, err := installedLibrary(agent, pins)
	if err != nil || filepath.Base(path) != "libort.so.1" {
		t.Errorf("installedLibrary = %q, %v", path, err)
	}
	if res, err := InstallRuntime(context.Background(), agent, InstallOptions{Client: client, Runtime: &pins}); err != nil || res.Reason != "already-current" {
		t.Errorf("second install: %+v, %v", res, err)
	}

	// A replaced library is never handed to dlopen.
	if err := os.WriteFile(path, []byte("tampered-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installedLibrary(agent, pins); err == nil || !strings.Contains(err.Error(), "pinned checksum") {
		t.Errorf("tampered library accepted: %v", err)
	}
	if res, err := InstallRuntime(context.Background(), agent, InstallOptions{Client: client, Runtime: &pins}); err != nil || res.Reason != "downloaded" {
		t.Errorf("repair: %+v, %v", res, err)
	}
}

func TestInstallRuntimeRejectsAWrongLibrary(t *testing.T) {
	archive, pins := fakeRuntime(t, []byte("other-library"))
	pins.Library = Artifact{Bytes: int64(len("other-library")), SHA256: sum([]byte("expected-library!"))}
	url, client, _ := serve(t, archive, nil)
	pins.URL = url
	agent := t.TempDir()
	_, err := InstallRuntime(context.Background(), agent, InstallOptions{Client: client, Runtime: &pins})
	if err == nil || !strings.Contains(err.Error(), "failed its checksum") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Lstat(RuntimeDirectory(agent)); statErr == nil {
		t.Error("runtime directory exists after a failed install")
	}
}

func TestLibraryPathPrefersTheEnvironmentOverride(t *testing.T) {
	env := func(k string) string {
		if k == LibraryEnv {
			return "/custom/libonnxruntime.so"
		}
		return ""
	}
	if got, err := LibraryPath(t.TempDir(), env); err != nil || got != "/custom/libonnxruntime.so" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := LibraryPath(t.TempDir(), func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "lancet setup") {
		t.Errorf("without an install the error must say how to fix it: %v", err)
	}
}

func TestPinnedValuesAreConsistent(t *testing.T) {
	p := PinnedModel()
	if len(p.Files) != 6 || !strings.HasPrefix(p.URL, "https://") || p.SHA256 != Archive.SHA256 {
		t.Errorf("pins = %+v", p)
	}
	if r, err := PinnedRuntime(); err == nil {
		if !strings.HasPrefix(r.URL, "https://github.com/microsoft/onnxruntime/releases/download/v1.30.0/") || r.Library.Bytes == 0 {
			t.Errorf("runtime pins = %+v", r)
		}
	}
}
