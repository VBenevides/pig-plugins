package goal

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func entry(data any) map[string]any {
	return map[string]any{"type": "custom", "customType": EntryType, "data": data}
}

func TestRestoreKeepsLatestAndHonorsClear(t *testing.T) {
	working := map[string]any{"objective": "ship it", "status": "working"}
	finished := map[string]any{"objective": "ship it", "status": "finished", "outcome": "reached", "summary": "done"}
	branch := []map[string]any{{"type": "message"}, entry(working), entry(finished)}
	state, warnings := Restore(branch)
	if len(warnings) != 0 || state == nil || state.Status != Finished || state.Outcome != Reached {
		t.Fatalf("state=%+v warnings=%v", state, warnings)
	}
	state, _ = Restore(append(branch, entry(map[string]any{"cleared": true})))
	if state != nil {
		t.Fatalf("cleared goal restored: %+v", state)
	}
}

func TestRestoreReportsMalformedEntryAndKeepsPreviousState(t *testing.T) {
	branch := []map[string]any{
		entry(map[string]any{"objective": "a", "status": "working"}),
		entry(map[string]any{"objective": "", "status": "working"}),
		entry(map[string]any{"objective": "b", "status": "paused"}),
		entry("not an object"),
	}
	state, warnings := Restore(branch)
	if state == nil || state.Objective != "a" || len(warnings) != 3 {
		t.Fatalf("state=%+v warnings=%v", state, warnings)
	}
}

func TestDescribe(t *testing.T) {
	if got := Describe(nil, true); !strings.Contains(got, "No goal") {
		t.Fatal(got)
	}
	working := &State{Objective: "fix bug", Status: Working}
	if got := Describe(working, false); got != "Current goal: fix bug\nCurrent status: working" {
		t.Fatalf("running: %q", got)
	}
	if got := Describe(working, true); !strings.Contains(got, "working (idle") {
		t.Fatalf("idle: %q", got)
	}
	done := &State{Objective: "fix bug", Status: Finished, Outcome: Blocked, Summary: "needs a key"}
	want := "Current goal: fix bug\nCurrent status: finished (blocked)\nResult: needs a key"
	if got := Describe(done, true); got != want {
		t.Fatalf("finished: %q", got)
	}
}

func TestSkillNames(t *testing.T) {
	got := SkillNames("/skill:alpha now fix it, use /skill:beta-2 and /skill:alpha again; path/skill:no x/skill:no")
	if !slices.Equal(got, []string{"alpha", "beta-2"}) {
		t.Fatalf("names = %q", got)
	}
	if len(SkillNames("nothing here")) != 0 {
		t.Fatal("unexpected names")
	}
}

func TestSkillBlockStripsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: demo\ndescription: d\n---\n\nDo the demo.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := SkillBlock("demo", path)
	if err != nil {
		t.Fatal(err)
	}
	want := "<skill name=\"demo\" location=\"" + path + "\">\nReferences are relative to " + dir + ".\n\nDo the demo.\n</skill>"
	if got != want {
		t.Fatalf("block = %q, want %q", got, want)
	}
	if _, err := SkillBlock("demo", filepath.Join(dir, "missing.md")); err == nil {
		t.Fatal("a missing skill file must be an error")
	}
}

func TestInitialMessage(t *testing.T) {
	if got := InitialMessage(nil, "x"); got != "Goal: x" {
		t.Fatal(got)
	}
	if got := InitialMessage([]string{"<skill/>", "<skill2/>"}, "x"); got != "<skill/>\n\n<skill2/>\n\nGoal: x" {
		t.Fatal(got)
	}
}
