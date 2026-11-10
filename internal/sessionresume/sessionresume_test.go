package sessionresume

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	header  = `{"type":"session","id":"x"}` + "\n"
	message = `{"type":"message","id":"m"}` + "\n"
)

func TestLatestPicksNewestSessionWithConversation(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "old.jsonl", header+message, 3*time.Hour)
	want := write(t, dir, "mid.jsonl", header+message, 2*time.Hour)
	write(t, dir, "empty-new.jsonl", header, time.Hour)
	current := write(t, dir, "current.jsonl", header+message, time.Minute)
	write(t, dir, "notes.txt", message, 0)
	got, err := Latest(dir, current)
	if err != nil || got != want {
		t.Fatalf("Latest = %q, %v; want %q", got, err, want)
	}
}

func TestLatestNoCandidate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "empty.jsonl", header, time.Hour)
	if got, err := Latest(dir, ""); err != nil || got != "" {
		t.Fatalf("Latest = %q, %v", got, err)
	}
	if got, err := Latest(filepath.Join(dir, "missing"), ""); err != nil || got != "" {
		t.Fatalf("missing dir: %q, %v", got, err)
	}
}

func TestShouldSkip(t *testing.T) {
	for _, args := range [][]string{{"-c"}, {"--continue"}, {"--session", "x"}, {"--session=x"}, {"--no-session"}, {"-p", "hi"}, {"-r"}} {
		if !ShouldSkip(args) {
			t.Errorf("%v should skip", args)
		}
	}
	for _, args := range [][]string{nil, {"hello"}, {"--model", "x"}, {"@file.md"}} {
		if ShouldSkip(args) {
			t.Errorf("%v should not skip", args)
		}
	}
}

func TestMarkerIsConsumedOnce(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	session := "/s/one.jsonl"
	if ok, err := Consume(session, time.Now()); err != nil || ok {
		t.Fatalf("no marker: %v %v", ok, err)
	}
	if err := Mark(session); err != nil {
		t.Fatal(err)
	}
	if ok, err := Consume(session, time.Now()); err != nil || !ok {
		t.Fatalf("first consume: %v %v", ok, err)
	}
	if ok, _ := Consume(session, time.Now()); ok {
		t.Fatal("second consume must be false")
	}
	if err := Mark(session); err != nil {
		t.Fatal(err)
	}
	if ok, err := Consume(session, time.Now().Add(2*markerMaxAge)); err != nil || ok {
		t.Fatalf("stale marker: %v %v", ok, err)
	}
	if err := Unmark(session); err != nil {
		t.Fatalf("unmark missing: %v", err)
	}
}
