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
		name, downloadExit, installExit string
		wantError, wantExecuted         bool
	}{
		{"success", "0", "0", false, true},
		{"partial download", "22", "0", true, false},
		{"installer failure", "0", "7", true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("PIG_PLUGINS_UPDATE", "0")
			marker := filepath.Join(dir, "executed")
			t.Setenv("UPDATE_MARKER", marker)
			// Even the failed transfer writes a valid installer: it must never run.
			script := "#!/bin/sh\nwhile [ \"$1\" != -o ]; do shift; done\n" +
				"printf '%s\\n' 'printf \"%s\" \"$PIG_PLUGINS_UPDATE\" > \"$UPDATE_MARKER\"' 'exit " + scenario.installExit + "' > \"$2\"\nexit " + scenario.downloadExit + "\n"
			if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			err := Update()
			if (err != nil) != scenario.wantError {
				t.Fatalf("err=%v", err)
			}
			data, readErr := os.ReadFile(marker)
			if scenario.wantExecuted {
				if readErr != nil || string(data) != "1" {
					t.Fatalf("marker=%q, err=%v", data, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("partial installer executed: %q, %v", data, readErr)
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, "pig-plugins-update.*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary installers: %v, %v", leftovers, err)
			}
		})
	}
}
