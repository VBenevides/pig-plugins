package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSharedModeDefaultsMigrationAndPrecedence(t *testing.T) {
	root := t.TempDir()
	store := ModeStore{Path: SharedModeFile(root)}
	profile := filepath.Join(root, "profile.json")
	state, note, err := store.Effective(profile)
	if err != nil || state.Mode != ModeLax || note != "" {
		t.Fatalf("default: %+v %q %v", state, note, err)
	}
	for _, legacy := range []string{`{"enabled":false}`, `{"enabled":true}`, `{"mode":"auto"}`, `{"mode":"strict"}`, `{"mode":"interactive"}`} {
		if err := os.WriteFile(profile, []byte(legacy), 0600); err != nil {
			t.Fatal(err)
		}
		state, note, err = store.Effective(profile)
		if err != nil || state.Mode != ModeStrict || note == "" {
			t.Fatalf("migration: %+v %q %v", state, note, err)
		}
		data, err := os.ReadFile(profile)
		if err != nil || string(data) != legacy {
			t.Fatalf("legacy modified: %q %v", data, err)
		}
	}
	if err := store.Save(ModeOff); err != nil {
		t.Fatal(err)
	}
	state, note, err = store.Effective(profile)
	if err != nil || state.Mode != ModeOff || note != "" {
		t.Fatalf("precedence: %+v %q %v", state, note, err)
	}
	for _, path := range []string{store.Path, filepath.Dir(store.Path)} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("private: %s %v %v", path, info, err)
		}
	}
}

func TestSharedModeBadStorageAndFailedSave(t *testing.T) {
	store := ModeStore{Path: filepath.Join(t.TempDir(), "mode.json")}
	for _, data := range []string{``, `null`, `{"mode":"off"}`, `{"mode":"bad","revision":"123"}`, strings.Repeat("x", 4097)} {
		if err := os.WriteFile(store.Path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
	before, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(AccessMode("bad")); err == nil {
		t.Fatal("invalid save accepted")
	}
	after, err := os.ReadFile(store.Path)
	if err != nil || string(after) != string(before) {
		t.Fatal("failed save changed storage")
	}
	store.Path = filepath.Join(store.Path, "impossible")
	if err := store.Save(ModeOff); err == nil {
		t.Fatal("file as directory accepted")
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("unreadable storage accepted")
	}
}

func TestSharedModeConcurrentChoices(t *testing.T) {
	store := ModeStore{Path: SharedModeFile(t.TempDir())}
	if err := store.Save(ModeStrict); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := range 8 {
		workers.Go(func() {
			mode := []AccessMode{ModeOff, ModeLax, ModeStrict}[i%3]
			for range 10 {
				if err := store.Save(mode); err != nil {
					t.Error(err)
					return
				}
				if _, err := store.Load(); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	workers.Wait()
	entries, err := filepath.Glob(filepath.Join(filepath.Dir(store.Path), ".mode-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files: %v %v", entries, err)
	}
}

func TestSharedModeProcess(t *testing.T) {
	if os.Getenv("GUARD_MODE_CHILD") != "1" {
		return
	}
	store := ModeStore{Path: os.Getenv("GUARD_MODE_PATH")}
	if mode := os.Getenv("GUARD_MODE_WRITE"); mode != "" {
		if err := store.Save(AccessMode(mode)); err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("mode=%s revision=%s\n", state.Mode, state.Revision)
}

func TestSharedModeAcrossProcessesAndProfiles(t *testing.T) {
	store := ModeStore{Path: SharedModeFile(t.TempDir())}
	child := func(write, profile string) string {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestSharedModeProcess$")
		cmd.Env = append(os.Environ(), "GUARD_MODE_CHILD=1", "GUARD_MODE_PATH="+store.Path, "GUARD_MODE_WRITE="+write,
			"PIG_HOME="+profile, "PIG_CODING_AGENT_DIR="+profile, "PI_CODING_AGENT_DIR="+profile)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v: %s", err, out)
		}
		return string(out)
	}
	for _, mode := range []string{"off", "strict", "lax"} {
		child(mode, t.TempDir())
		if out := child("", t.TempDir()); !strings.Contains(out, "mode="+mode+" revision=") {
			t.Fatal(out)
		}
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ModeLax); err != nil {
		t.Fatal(err)
	}
	next, err := store.Load()
	if err != nil || next.Revision == state.Revision {
		t.Fatalf("revision not renewed: %+v %v", next, err)
	}
}
