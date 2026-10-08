package guard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selector records the select dialogs and answers them with a fixed choice.
type selector struct {
	asked   []string
	options [][]string
	choice  string
}

func (s *selector) selectOne(title string, options []string) (string, bool, error) {
	s.asked = append(s.asked, title)
	s.options = append(s.options, options)
	return s.choice, s.choice != "", nil
}

func workDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func selectCall(cwd, tool string, input map[string]any, s *selector) Call {
	return Call{Tool: tool, Input: input, Cwd: cwd, HasUI: true, Select: s.selectOne}
}

func TestRecursiveDeleteInsideScratchFoldersIsAllowedWithoutAsking(t *testing.T) {
	cwd := workDir(t)
	tests := []struct {
		command   string
		wantAsked bool
	}{
		{"rm -rf .agent-work/tmp/build", false},
		{"rm -rf ./.agent-work/scripts/x .agent-work/debug", false},
		{"rm -rf tmp/cache", false},
		{"rm -rf src/tmp/cache", false},
		{"rm -rf 'tmp/with space'", true}, // the shell words split inside the quotes, so the paths are not known
		{"rm -rf " + filepath.Join(cwd, "tmp", "abs"), false},
		{"rm -rf tmp", true},                     // the scratch folder itself
		{"rm -rf .agent-work", true},             // the whole agent workspace
		{"rm -rf src/build", true},               // not scratch
		{"rm -rf tmp/../src", true},              // leaves the scratch folder
		{"rm -rf tmp/ok build", true},            // one target is not scratch
		{"rm -rf $HOME/tmp/x", true},             // the shell decides the path
		{"rm -rf /tmp/x", true},                  // tmp outside the working folder
		{"rm -rf tmp/x && rm -rf src/y", true},   // second delete is not scratch
		{"rm -rf tmp/x; git push --force", true}, // another dangerous behavior
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			s := &selector{choice: choiceDeny}
			got := newGate(Interactive, false, nil).Check(context.Background(), selectCall(cwd, "bash", bash(tt.command), s))
			if asked := len(s.asked) > 0; asked != tt.wantAsked {
				t.Fatalf("asked=%v, want %v (%+v)", asked, tt.wantAsked, got)
			}
			if got.Block != tt.wantAsked {
				t.Errorf("block=%v for %q", got.Block, tt.command)
			}
		})
	}
}

func TestScratchDeleteThroughSymlinkOutOfScratchStillAsks(t *testing.T) {
	cwd := workDir(t)
	outside := workDir(t)
	if err := os.MkdirAll(filepath.Join(cwd, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cwd, "tmp", "link")); err != nil {
		t.Fatal(err)
	}
	s := &selector{choice: choiceDeny}
	got := newGate(Interactive, false, nil).Check(context.Background(), selectCall(cwd, "bash", bash("rm -rf tmp/link/data"), s))
	if !got.Block || len(s.asked) != 1 {
		t.Fatalf("a symlink out of tmp must still ask: %+v asked=%v", got, s.asked)
	}
}

func TestDialogShowsAffectedItemsAndOffersAlwaysAllow(t *testing.T) {
	cwd := workDir(t)
	for _, name := range []string{"build", "dist"} {
		if err := os.Mkdir(filepath.Join(cwd, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gate := newGate(Interactive, false, nil)
	gate.UseAllowlist(NewAllowlist(filepath.Join(t.TempDir(), "allow.json")))
	s := &selector{choice: choiceDeny}
	gate.Check(context.Background(), selectCall(cwd, "bash", bash("rm -rf build dist"), s))
	if len(s.asked) != 1 {
		t.Fatalf("asked %v", s.asked)
	}
	for _, want := range []string{"Affected items:", "- folder: " + filepath.Join(cwd, "build") + " - delete", "- folder: " + filepath.Join(cwd, "dist") + " - delete"} {
		if !strings.Contains(s.asked[0], want) {
			t.Errorf("dialog lacks %q:\n%s", want, s.asked[0])
		}
	}
	if got, want := strings.Join(s.options[0], "|"), strings.Join([]string{choiceDeny, choiceOnce, choiceAlways}, "|"); got != want {
		t.Errorf("options = %q, want %q", got, want)
	}
}

func TestAlwaysAllowRemembersTheCommandAndItemPairOnly(t *testing.T) {
	cwd := workDir(t)
	list := NewAllowlist(filepath.Join(t.TempDir(), "allow.json"))
	gate := newGate(Interactive, false, nil)
	gate.UseAllowlist(list)

	always := &selector{choice: choiceAlways}
	if got := gate.Check(context.Background(), selectCall(cwd, "bash", bash("rm -rf build"), always)); got.Block {
		t.Fatalf("always allow blocked: %+v", got)
	}
	deny := &selector{choice: choiceDeny}
	// Same command and item: no dialog, even in a new gate that reads the same file.
	fresh := newGate(Interactive, false, nil)
	fresh.UseAllowlist(list)
	if got := fresh.Check(context.Background(), selectCall(cwd, "bash", bash("rm -rf build"), deny)); got.Block || len(deny.asked) != 0 {
		t.Errorf("stored pair asked again or blocked: %+v asked=%v", got, deny.asked)
	}
	// Another item, another command, or the same command elsewhere: asks again.
	for _, tt := range []struct{ dir, command string }{
		{cwd, "rm -rf dist"},
		{cwd, "rm -rf build; sudo ls"},
		{workDir(t), "rm -rf build"},
	} {
		deny := &selector{choice: choiceDeny}
		if got := fresh.Check(context.Background(), selectCall(tt.dir, "bash", bash(tt.command), deny)); !got.Block || len(deny.asked) != 1 {
			t.Errorf("%q in %s must ask: %+v asked=%v", tt.command, tt.dir, got, deny.asked)
		}
	}
}

func TestAlwaysAllowDoesNotLoosenStrictHardBlocksOrWrites(t *testing.T) {
	cwd := workDir(t)
	list := NewAllowlist(filepath.Join(t.TempDir(), "allow.json"))
	if err := list.Add(Grant{Tool: "bash", Command: "rm -rf build", Item: filepath.Join(cwd, "build")}); err != nil {
		t.Fatal(err)
	}
	strict := newGate(Strict, false, nil)
	strict.UseAllowlist(list)
	s := &selector{choice: choiceOnce}
	if got := strict.Check(context.Background(), selectCall(cwd, "bash", bash("rm -rf build"), s)); !got.Block || !strings.Contains(got.Reason, "strict mode") {
		t.Errorf("strict mode must still block: %+v", got)
	}
	interactive := newGate(Interactive, false, nil)
	interactive.UseAllowlist(list)
	if got := interactive.Check(context.Background(), selectCall(cwd, "bash", bash("rm -rf /"), s)); !got.Block || len(s.asked) != 0 {
		t.Errorf("hard block must never be offered: %+v asked=%v", got, s.asked)
	}
}

func TestAlwaysAllowForProtectedWriteIsPerPath(t *testing.T) {
	cwd := workDir(t)
	gate := newGate(Interactive, false, nil)
	gate.UseAllowlist(NewAllowlist(filepath.Join(t.TempDir(), "allow.json")))
	always := &selector{choice: choiceAlways}
	if got := gate.Check(context.Background(), selectCall(cwd, "write", map[string]any{"path": ".env"}, always)); got.Block {
		t.Fatalf("blocked: %+v", got)
	}
	if !strings.Contains(always.asked[0], "- file: "+filepath.Join(cwd, ".env")+" - write contents") {
		t.Errorf("dialog lacks the affected file:\n%s", always.asked[0])
	}
	deny := &selector{choice: choiceDeny}
	if got := gate.Check(context.Background(), selectCall(cwd, "write", map[string]any{"path": ".env"}, deny)); got.Block || len(deny.asked) != 0 {
		t.Errorf("same path asked again: %+v", got)
	}
	if got := gate.Check(context.Background(), selectCall(cwd, "edit", map[string]any{"path": ".env"}, deny)); !got.Block {
		t.Errorf("a grant for write must not cover edit: %+v", got)
	}
	if got := gate.Check(context.Background(), selectCall(cwd, "write", map[string]any{"path": "sub/.env"}, deny)); !got.Block {
		t.Errorf("a grant for one path must not cover another: %+v", got)
	}
}

func TestProtectedWriteInsideScratchNeedsNoConfirmation(t *testing.T) {
	cwd := workDir(t)
	s := &selector{choice: choiceDeny}
	gate := newGate(Interactive, false, nil)
	if got := gate.Check(context.Background(), selectCall(cwd, "write", map[string]any{"path": ".agent-work/tmp/.env"}, s)); got.Block || len(s.asked) != 0 {
		t.Errorf("scratch write asked or blocked: %+v asked=%v", got, s.asked)
	}
	if got := gate.Check(context.Background(), selectCall(cwd, "write", map[string]any{"path": ".env"}, s)); !got.Block {
		t.Errorf("a protected path outside scratch must ask: %+v", got)
	}
}

func TestDamagedAllowlistAsksAndGrantsNothing(t *testing.T) {
	cwd := workDir(t)
	file := filepath.Join(t.TempDir(), "allow.json")
	if err := os.WriteFile(file, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	list := NewAllowlist(file)
	if _, err := list.Allows(Grant{}); err == nil {
		t.Error("damaged file read as a valid list")
	}
	gate := newGate(Interactive, false, nil)
	gate.UseAllowlist(list)
	s := &selector{choice: choiceAlways}
	if got := gate.Check(context.Background(), selectCall(cwd, "bash", bash("rm -rf build"), s)); got.Block || len(s.asked) != 1 {
		t.Errorf("must ask once and still allow this time: %+v asked=%v", got, s.asked)
	}
	if data, _ := os.ReadFile(file); string(data) != "{broken" {
		t.Errorf("a damaged file was overwritten: %q", data)
	}
}
