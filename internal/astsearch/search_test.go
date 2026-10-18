package astsearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func realBinary(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("PI_TEST_AST_GREP"); binary != "" {
		return binary
	}
	binary, err := exec.LookPath("ast-grep")
	if err != nil {
		t.Skip("optional ast-grep unavailable; set PI_TEST_AST_GREP for real CLI tests")
	}
	return binary
}
func fixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRealStructuralSearchIsReadOnly(t *testing.T) {
	binary := realBinary(t)
	dir := t.TempDir()
	source := "package example\nfunc Example() { println( 1 ); println(2); other(3) }\n"
	path := fixture(t, dir, "source ; $(touch injected).go", source)
	fixture(t, dir, "other.go", "package example\nfunc Other() { println(4) }\n")
	// An invalid repository config must never be loaded, including during version
	// checks. It could otherwise request native custom-language library loading.
	fixture(t, dir, "sgconfig.yml", "not: [valid config\n")
	result, err := (Searcher{Binary: binary}).Search(context.Background(), dir, Options{Pattern: "println($A)", Language: "go", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 3 || result.Truncated {
		t.Fatalf("%+v", result)
	}
	for _, match := range result.Matches {
		if match.Start.Line < 1 || match.Start.Column < 1 || !strings.Contains(match.Text, "println") {
			t.Fatalf("%+v", match)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != source {
		t.Fatal("search modified source", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "injected")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shell interpolation")
	}
	empty, err := (Searcher{Binary: binary}).Search(context.Background(), dir, Options{Pattern: "absent($A)", Language: "go"})
	if err != nil || len(empty.Matches) != 0 {
		t.Fatalf("no-match: %+v %v", empty, err)
	}
	limited, err := (Searcher{Binary: binary}).Search(context.Background(), dir, Options{Pattern: "println($A)", Language: "go", Limit: 1})
	if err != nil || len(limited.Matches) != 1 || !limited.Truncated {
		t.Fatalf("limit: %+v %v", limited, err)
	}
}

func TestRealPatternAndLanguageFailures(t *testing.T) {
	binary := realBinary(t)
	dir := t.TempDir()
	for _, test := range []struct{ pattern, lang, code string }{
		{"", "go", "invalid_pattern"}, {"func {", "go", "invalid_pattern"}, {"x", "not-a-real-language", "unsupported_language"},
	} {
		_, err := (Searcher{Binary: binary}).Search(context.Background(), dir, Options{Pattern: test.pattern, Language: test.lang})
		var searchErr *Error
		if !errors.As(err, &searchErr) || searchErr.Code != test.code {
			t.Errorf("%q %q: %v", test.pattern, test.lang, err)
		}
	}
	// A dash-prefixed pattern is a value, never a CLI option.
	fixture(t, dir, "a.js", "const x = -a;\n")
	result, err := (Searcher{Binary: binary}).Search(context.Background(), dir, Options{Pattern: "-$A", Language: "javascript"})
	if err != nil || len(result.Matches) != 1 {
		t.Fatalf("dash pattern: %+v %v", result, err)
	}
}

func TestRealDiscoveryExclusionsAndOversize(t *testing.T) {
	binary := realBinary(t)
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	fixture(t, dir, ".gitignore", "ignored.go\n")
	fixture(t, dir, "a.go", "package example\nfunc A(){println(1)}\n")
	fixture(t, dir, "ignored.go", "package example\nfunc I(){println(2)}\n")
	fixture(t, dir, "huge.go", strings.Repeat(" ", maxSourceBytes+1))
	result, err := (Searcher{Binary: binary}).Search(context.Background(), dir, Options{Pattern: "println($A)", Language: "go"})
	if err != nil || len(result.Matches) != 1 || result.Skipped != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}

// Helper subprocess exercises adapter failures without depending on an installed
// ast-grep binary. Actual pattern semantics are covered by the real CLI tests.
func TestAstHelper(t *testing.T) {
	if os.Getenv("PI_AST_HELPER") != "1" {
		return
	}
	args := os.Args
	i := slicesIndex(args, "--")
	if i < 0 {
		os.Exit(2)
	}
	args = args[i+1:]
	if log := os.Getenv("PI_AST_LOG"); log != "" {
		data, _ := json.Marshal(args)
		f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(3)
		}
		if _, err := fmt.Fprintln(f, string(data)); err != nil {
			os.Exit(3)
		}
		if err := f.Close(); err != nil {
			os.Exit(3)
		}
	}
	mode := os.Getenv("PI_AST_MODE")
	if slicesIndex(args, "--version") >= 0 {
		if mode == "old" {
			fmt.Println("ast-grep 0.41.0")
		} else {
			fmt.Println("ast-grep 0.42.2")
		}
		os.Exit(0)
	}
	if slicesIndex(args, "--stdin") >= 0 {
		os.Exit(1)
	}
	switch mode {
	case "sleep":
		time.Sleep(time.Minute)
	case "bad-json":
		fmt.Println("{invalid")
	case "stderr":
		fmt.Fprintln(os.Stderr, "permission denied")
		os.Exit(2)
	case "oversized-record":
		fmt.Println(strings.Repeat("x", maxCaptureBytes+100))
	default:
		count := 1
		if mode == "many" {
			count = 1000
		}
		for n := range count {
			data, _ := json.Marshal(map[string]any{"file": "source.go", "text": strings.Repeat("x", 1000), "range": map[string]any{"start": Position{n, 0}, "end": Position{n, 1}}})
			fmt.Println(string(data))
		}
	}
	os.Exit(0)
}
func slicesIndex(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}
func helper(t *testing.T) string {
	t.Helper()
	t.Setenv("PI_AST_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "ast-grep")
	data := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestAstHelper$ -- \"$@\"\n"
	if err := os.WriteFile(script, []byte(data), 0700); err != nil {
		t.Fatal(err)
	}
	return script
}
func TestAdapterFailuresAndBudgets(t *testing.T) {
	for _, mode := range []string{"old", "sleep", "bad-json", "stderr", "oversized-record", "many"} {
		t.Run(mode, func(t *testing.T) {
			binary := helper(t)
			t.Setenv("PI_AST_MODE", mode)
			dir := t.TempDir()
			fixture(t, dir, "source.go", "package example\n")
			timeout := time.Duration(0)
			if mode == "sleep" {
				timeout = 200 * time.Millisecond
			}
			result, err := (Searcher{Binary: binary, Timeout: timeout}).Search(context.Background(), dir, Options{Pattern: "x", Language: "go", Limit: 200})
			switch mode {
			case "sleep":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			case "many", "oversized-record":
				if err != nil || !result.Truncated {
					t.Fatalf("%+v %v", result, err)
				}
			default:
				if err == nil {
					t.Fatal("failure hidden")
				}
			}
		})
	}
}
func TestArgvIsolationAndCancellation(t *testing.T) {
	binary := helper(t)
	dir := t.TempDir()
	source := fixture(t, dir, "--rewrite=$A; touch pwn.go", "package example")
	log := filepath.Join(t.TempDir(), "argv")
	t.Setenv("PI_AST_LOG", log)
	pattern := "-$A; $(touch injected)"
	_, err := (Searcher{Binary: binary}).Search(context.Background(), dir, Options{Pattern: pattern, Language: "go", Path: source})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var args []string
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &args); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args[len(args)-2:], []string{"--", source}) || slicesIndex(args, "--pattern="+pattern) < 0 {
		t.Fatal(args)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Searcher{Binary: binary}).Search(ctx, dir, Options{Pattern: "x", Language: "go"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestMissingDependencyAndInvalidArguments(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := (Searcher{}).Search(context.Background(), t.TempDir(), Options{Pattern: "x", Language: "go"})
	var searchErr *Error
	if !errors.As(err, &searchErr) || searchErr.Code != "dependency_unavailable" {
		t.Fatal(err)
	}
	for _, options := range []Options{{Pattern: "x", Language: "go", Limit: -1}, {Pattern: "x", Language: "--rewrite"}, {Pattern: "x", Language: "go", Path: "\x00"}} {
		if _, err := (Searcher{}).Search(context.Background(), t.TempDir(), options); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
}
