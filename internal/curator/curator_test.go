package curator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func child(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "curator")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return file
}
func TestTransportOutputBoundAndSplitUTF8(t *testing.T) {
	bin := child(t, "printf '\\351'; printf '\\233\\252'\n")
	got, err := MemoryCommand(context.Background(), Options{Bin: bin}, []string{"read"})
	if err != nil || got != "雪" {
		t.Fatal(got, err)
	}
	bin = child(t, "yes SECRET | dd bs=4096 count=300 2>/dev/null\n")
	_, err = MemoryCommand(context.Background(), Options{Bin: bin}, []string{"read"})
	if err == nil || !strings.Contains(err.Error(), "output limit") || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
}
func TestTransportTimeoutAndFailureDoNotReflectPayload(t *testing.T) {
	bin := child(t, "sleep 2\n")
	_, err := MemoryCommand(context.Background(), Options{Bin: bin, Timeout: 20 * time.Millisecond}, []string{"read"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatal(err)
	}
	bin = child(t, "echo SECRET >&2; exit 7\n")
	_, err = MemoryCommand(context.Background(), Options{Bin: bin}, []string{"read"})
	if err == nil || !strings.Contains(err.Error(), "exit 7") || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
}
func TestBindingRejectsRemovedRepository(t *testing.T) {
	dir := t.TempDir()
	bin := child(t, "echo leaked\n")
	_, err := MemoryCommand(context.Background(), Options{Bin: bin, Cwd: dir, BoundRoot: dir}, []string{"read"})
	if err == nil || !strings.Contains(err.Error(), "repository changed") {
		t.Fatal(err)
	}
}
func TestIngestWarningsRemainObservable(t *testing.T) {
	bin := child(t, "cat >/dev/null; printf '%s' '{\"results\":[{\"id\":\"a\",\"outcome\":\"durable\"}],\"journal_warnings\":[\"malformed record\"]}'\n")
	var warnings []string
	c := NewCapture(IngestTransport(Options{Bin: bin}), CaptureOptions{OnProblem: func(s string) { warnings = append(warnings, s) }})
	c.Enqueue([]EventIn{ev("a")})
	c.Flush(context.Background())
	if c.Stats().Durable != 1 || len(warnings) != 1 || !strings.Contains(warnings[0], "malformed record") {
		t.Fatal(c.Stats(), warnings)
	}
}
func TestOversizedCaptureDoesNotDiscardNeighbor(t *testing.T) {
	var seen []IngestRequest
	c := NewCapture(acknowledge(func(string) string { return OutcomeDurable }, &seen), CaptureOptions{})
	large := ev("large")
	large.Content = new(strings.Repeat("x", 256<<10+1))
	c.Enqueue([]EventIn{large, ev("good")})
	c.Flush(context.Background())
	if got := c.Stats(); got.Durable != 1 || got.Gaps != 0 {
		t.Fatal(got)
	}
	if len(seen) != 1 || len(seen[0].Events) != 1 || seen[0].Events[0].ID != "good" || seen[0].Gaps[0].EventID != "large" {
		t.Fatal(seen)
	}
}
