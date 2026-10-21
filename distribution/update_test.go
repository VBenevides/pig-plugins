package distribution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepare(t *testing.T) {
	t.Setenv("PATH", "/system/bin")
	t.Setenv("PIG_HOME", "/custom/pig")
	t.Setenv("PIG_PLUGINS_HOST_VERSION", "stale")
	if err := Prepare("0.4.1"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("PATH"); got != "/system/bin"+string(os.PathListSeparator)+"/custom/pig/bin" {
		t.Fatal(got)
	}
	if got := os.Getenv("PIG_PLUGINS_HOST_VERSION"); got != "0.4.1" {
		t.Fatal(got)
	}
}

func TestPrepareDefaultHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PIG_HOME", "")
	t.Setenv("PATH", "/system/bin")
	t.Setenv("PIG_PLUGINS_HOST_VERSION", "")
	if err := Prepare("fixture"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(os.Getenv("PATH"), filepath.Join(home, ".pig", "bin")) {
		t.Fatal(os.Getenv("PATH"))
	}
}

func TestUpdate(t *testing.T) {
	for _, scenario := range []struct {
		name, local, remote, ref, failure       string
		wantError, wantDownloaded, wantExecuted bool
	}{
		{name: "newer", local: "0.2.2", remote: "0.2.3", wantDownloaded: true, wantExecuted: true},
		{name: "equal", local: "0.2.2", remote: "0.2.2"},
		{name: "older", local: "0.2.2", remote: "0.2.1"},
		{name: "numeric", local: "0.9.9", remote: "0.10.0", wantDownloaded: true, wantExecuted: true},
		{name: "prefix and metadata", local: "v1.2.3+local", remote: "1.2.3+remote"},
		{name: "unknown local", local: "dev", remote: "1.0.0", wantError: true},
		{name: "prerelease", local: "1.0.0-rc.1", remote: "1.0.0", wantError: true},
		{name: "invalid remote", local: "1.0.0", remote: "bad", wantError: true},
		{name: "remote prerelease", local: "1.0.0", remote: "2.0.0-rc.1", wantError: true},
		{name: "invalid ref", local: "1.0.0", remote: "2.0.0", ref: "bad", wantError: true},
		{name: "ref lookup failure", local: "1.0.0", remote: "2.0.0", failure: "ref", wantError: true},
		{name: "version lookup failure", local: "1.0.0", remote: "2.0.0", failure: "version", wantError: true},
		{name: "partial installer", local: "1.0.0", remote: "2.0.0", failure: "download", wantError: true, wantDownloaded: true},
		{name: "installer failure", local: "1.0.0", remote: "2.0.0", failure: "install", wantError: true, wantDownloaded: true, wantExecuted: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("REMOTE_VERSION", scenario.remote)
			t.Setenv("REMOTE_REF", scenario.ref)
			t.Setenv("FAILURE", scenario.failure)
			t.Setenv("UPDATE_MARKER", filepath.Join(dir, "executed"))
			t.Setenv("DOWNLOAD_MARKER", filepath.Join(dir, "downloaded"))
			// Deliberately hostile ambient values must not change the checked install target.
			t.Setenv("PIG_PLUGINS_COMMIT", "wrong")
			t.Setenv("PIG_PLUGINS_REPO", "https://example.invalid/other")
			script := `#!/bin/sh
set -eu
while [ "$1" != -o ]; do url=$1; shift; done
case "$url" in
  */git/ref/heads/main)
    [ "$FAILURE" != ref ] || exit 22
    printf '{"object":{"sha":"%s"}}' "${REMOTE_REF:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}" > "$2" ;;
  */aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/VERSION)
    [ "$FAILURE" != version ] || exit 22
    printf '%s' "$REMOTE_VERSION" > "$2" ;;
  */aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/scripts/install.sh)
    : > "$DOWNLOAD_MARKER"
    printf '%s\n' 'printf "%s|%s|%s" "$PIG_PLUGINS_UPDATE" "$PIG_PLUGINS_COMMIT" "$PIG_PLUGINS_REPO" > "$UPDATE_MARKER"' '[ "$FAILURE" != install ]' > "$2"
    [ "$FAILURE" != download ] || exit 22 ;;
  *) echo "unexpected URL: $url" >&2; exit 1 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			err := update(scenario.local)
			if (err != nil) != scenario.wantError {
				t.Fatalf("err=%v", err)
			}
			_, downloadErr := os.Stat(filepath.Join(dir, "downloaded"))
			if (downloadErr == nil) != scenario.wantDownloaded {
				t.Fatalf("installer downloaded: %v", downloadErr)
			}
			data, readErr := os.ReadFile(filepath.Join(dir, "executed"))
			if scenario.wantExecuted {
				want := "1|" + strings.Repeat("a", 40) + "|https://github.com/VBenevides/pig-plugins"
				if readErr != nil || string(data) != want {
					t.Fatalf("marker=%q, err=%v", data, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("installer executed: %q, %v", data, readErr)
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, "pig-plugins-update.*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary installers: %v, %v", leftovers, err)
			}
		})
	}
}

func TestReadMetadataBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 4097)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMetadata(path); err == nil {
		t.Fatal("accepted oversized metadata")
	}
}
