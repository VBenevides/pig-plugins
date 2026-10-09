package guard

import (
	"errors"
	"testing"
)

const inspectionCommand = `readlink /proc/296265/exe; strings /proc/296265/exe | rg -m 5 'ExtensionSelectorComponent.*renderHeight|ExtensionSelectorComponent.*SetTerminalHeight'; strings /home/wdtg/.pig/bin/pig-plugins | rg -m 5 'ExtensionSelectorComponent.*renderHeight|ExtensionSelectorComponent.*SetTerminalHeight'; rg -n 'Select|Confirm|slice|truncate' internal/guard/gate.go internal/guard/*.go | head -35`

func TestReadOnlyCommandsBypassLancet(t *testing.T) {
	t.Setenv("RIPGREP_CONFIG_PATH", "")
	for _, mode := range []Mode{Interactive, Strict} {
		scorer := &fakeScorer{err: errors.New("unavailable")}
		d := &dialog{}
		got := check(newGate(mode, true, scorer), d, "bash", bash(inspectionCommand), true)
		if got.Block || len(scorer.calls) != 0 || len(d.asked) != 0 {
			t.Fatalf("read-only command was gated: %+v calls=%v dialogs=%v", got, scorer.calls, d.asked)
		}
		if got := check(newGate(mode, true, nil), nil, "bash", bash("ls -la"), false); got.Block {
			t.Fatalf("read-only command requires scorer/UI: %+v", got)
		}
	}
}

func TestReadOnlyShellSubset(t *testing.T) {
	t.Setenv("RIPGREP_CONFIG_PATH", "")
	for _, command := range []string{
		inspectionCommand,
		"pwd; ls -la ./internal",
		"cat 'file with spaces' | grep 'a|b' | head -5",
		"readlink /proc/1/exe && stat ./go.mod",
		"rg -n \"literal pattern\" ./internal/*.go || wc -l ./go.mod",
		"strings ./binary | tail -10",
	} {
		if !readOnlyCommand(command) {
			t.Errorf("read-only command not recognized: %s", command)
		}
	}
	for _, command := range []string{
		"", "ls;", "ls |", "ls &", "ls |& cat", "ls &&& cat",
		"ls; rm -rf ./data", "cat ./file > ./output", "ls 2>/dev/null",
		"cat <(touch ./file)", "cat $(touch ./file)", "cat `touch ./file`",
		`cat "$(touch ./file)"`, "cat $INPUT", "ls # comment", "ls \\; touch file",
		"rg --pre=./script pattern ./file", "rg --pre ./script pattern ./file",
		"rg --hostname-bin=./script pattern ./file", "rg --hostname-bin ./script pattern ./file",
		"rg pattern *", "rg pattern --pre=./*.sh", "/tmp/rg pattern file",
		"bash -c 'ls'", "sudo ls", "env ls", "X=1 ls", "find . -exec touch {} +",
		"sed -i 's/a/b/' file", "git reset --hard", "curl https://example.org",
		"ls 'unterminated", "ls (", "ls\x00",
	} {
		if readOnlyCommand(command) {
			t.Errorf("unsafe/unknown syntax bypassed: %s", command)
		}
	}
}

func TestReadOnlyMixedCommandsStillReachLancet(t *testing.T) {
	t.Setenv("RIPGREP_CONFIG_PATH", "")
	for _, command := range []string{"ls; touch file", "cat file > out", "rg --pre=./script pattern file", "cat $(touch file)"} {
		scorer := &fakeScorer{err: errors.New("unavailable")}
		got := check(newGate(Strict, true, scorer), nil, "bash", bash(command), false)
		if !got.Block || len(scorer.calls) != 1 {
			t.Fatalf("mixed/unknown command bypassed guard: %s %+v calls=%v", command, got, scorer.calls)
		}
	}
}

func TestReadOnlyRipgrepConfigRetainsGuard(t *testing.T) {
	t.Setenv("RIPGREP_CONFIG_PATH", "/config/rg")
	if readOnlyCommand("rg pattern ./file") {
		t.Fatal("implicit config hooks bypassed the guard")
	}
	scorer := &fakeScorer{err: errors.New("unavailable")}
	got := check(newGate(Strict, true, scorer), nil, "bash", bash("rg pattern ./file"), false)
	if !got.Block || len(scorer.calls) != 1 {
		t.Fatalf("config hooks bypassed LANCET: %+v", got)
	}
}

func TestReadOnlyBypassPreservesHardBlocks(t *testing.T) {
	got := check(newGate(Strict, true, nil), nil, "bash", bash("ls; rm -rf /"), false)
	if !got.Block {
		t.Fatal("hard-blocked command was allowed")
	}
}
