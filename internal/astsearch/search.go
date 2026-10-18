package astsearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/VBenevides/pig-plugins/internal/lsp"
)

const MaxResults = 200
const maxSourceBytes = 1 << 20
const visibleBudget = 20 << 10

var languageName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]{0,31}$`)
var supportedVersion = regexp.MustCompile(`^ast-grep 0\.42\.[0-9]+\s*$`)

type Options struct {
	Pattern, Language, Path string
	Limit                   int
}
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}
type Match struct {
	File             string   `json:"file"`
	Start            Position `json:"start"`
	End              Position `json:"end"`
	Text             string   `json:"text"`
	SnippetTruncated bool     `json:"snippetTruncated,omitempty"`
}
type Result struct {
	Matches   []Match  `json:"matches"`
	Truncated bool     `json:"truncated"`
	Skipped   int      `json:"skipped"`
	Warnings  []string `json:"warnings,omitempty"`
}
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string             { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error             { return e.Err }
func failure(code string, err error) error { return &Error{code, err} }

// Searcher is native Go orchestration. Binary is empty in production (resolve
// ast-grep on PATH, never util-linux sg); tests may supply an explicit executable.
type Searcher struct {
	Binary  string
	Timeout time.Duration
}

func (s Searcher) Search(ctx context.Context, cwd string, options Options) (result Result, err error) {
	result.Matches = []Match{}
	if strings.TrimSpace(options.Pattern) == "" || len(options.Pattern) > 8192 || strings.ContainsRune(options.Pattern, 0) {
		return result, failure("invalid_pattern", errors.New("pattern must be nonempty, NUL-free and at most 8192 bytes"))
	}
	if !languageName.MatchString(options.Language) {
		return result, failure("unsupported_language", errors.New("supply a built-in ast-grep language name"))
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	if options.Limit < 1 || options.Limit > MaxResults {
		return result, failure("invalid_arguments", fmt.Errorf("limit must be 1..%d", MaxResults))
	}
	if strings.ContainsRune(options.Path, 0) {
		return result, failure("invalid_arguments", errors.New("path contains NUL"))
	}
	timeout := s.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 15 * time.Second
	}
	request, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := request.Err(); err != nil {
		return result, err
	}
	binary := s.Binary
	if binary == "" {
		binary, err = exec.LookPath("ast-grep")
		if err != nil {
			return result, failure("dependency_unavailable", fmt.Errorf("install ast-grep 0.42.x on PATH: %w", err))
		}
	}
	// Explicit safe configuration prevents automatic loading of repository custom
	// language libraries, even for the CLI's version probe. No project config runs.
	configDir, err := os.MkdirTemp("", "pig-ast-search-")
	if err != nil {
		return result, fmt.Errorf("create isolated ast-grep config: %w", err)
	}
	config := filepath.Join(configDir, "sgconfig.yml")
	defer func() { err = errors.Join(err, os.RemoveAll(configDir)) }()
	if err := os.WriteFile(config, []byte("ruleDirs: []\n"), 0600); err != nil {
		return result, fmt.Errorf("write isolated ast-grep config: %w", err)
	}
	version, err := execute(request, binary, configDir, []string{"--config", config, "--version"})
	if err != nil {
		return result, err
	}
	if version.limited || version.exit != 0 || !supportedVersion.MatchString(strings.TrimSpace(version.out)) {
		return result, failure("dependency_incompatible", errors.New("requires ast-grep 0.42.x"))
	}
	base := []string{"run", "--config", config, "--json=stream", "--color=never", "--threads=2", "--lang=" + options.Language, "--pattern=" + options.Pattern}
	// Validate even when the search scope has no eligible files. Stdin is closed.
	probe, err := execute(request, binary, configDir, append(append([]string{}, base...), "--stdin"))
	if err != nil {
		return result, err
	}
	if probe.limited {
		return result, failure("output_limit", errors.New("ast-grep validation exceeded output budget"))
	}
	if (probe.exit != 0 && probe.exit != 1) || strings.TrimSpace(probe.stderr) != "" {
		return result, cliError(probe)
	}
	scope := options.Path
	if scope == "" {
		scope = "."
	}
	if !filepath.IsAbs(scope) {
		scope = filepath.Join(cwd, scope)
	}
	scope, err = filepath.Abs(scope)
	if err != nil {
		return result, err
	}
	files, capped, err := lsp.SourceFiles(request, scope)
	if err != nil {
		return result, fmt.Errorf("discover AST search source files: %w", err)
	}
	result.Truncated = capped
	eligible := make([]string, 0, len(files))
	for _, file := range files {
		if err := request.Err(); err != nil {
			return result, err
		}
		info, statErr := os.Lstat(file)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > maxSourceBytes {
			result.Skipped++
			if len(result.Warnings) < 8 {
				reason := "not a regular source file or exceeds 1 MiB"
				if statErr != nil {
					reason = statErr.Error()
				}
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s", file, reason))
			}
			continue
		}
		eligible = append(eligible, file)
	}
	usedBytes := 0
	for start := 0; start < len(eligible); start += 16 {
		args := append(append([]string{}, base...), "--")
		args = append(args, eligible[start:min(start+16, len(eligible))]...)
		output, runErr := execute(request, binary, cwd, args)
		if runErr != nil {
			return result, runErr
		}
		if output.exit != 0 && output.exit != 1 {
			return result, cliError(output)
		}
		if strings.TrimSpace(output.stderr) != "" {
			return result, cliError(output)
		}
		data := output.out
		if output.limited {
			result.Truncated = true
			if end := strings.LastIndexByte(data, '\n'); end >= 0 {
				data = data[:end+1]
			} else {
				data = ""
			}
		}
		for line := range strings.SplitSeq(data, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var raw struct {
				File, Text string
				Range      struct{ Start, End Position }
			}
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				return result, failure("invalid_output", fmt.Errorf("decode ast-grep match: %w", err))
			}
			if raw.File == "" || raw.Range.Start.Line < 0 || raw.Range.Start.Column < 0 || raw.Range.End.Line < raw.Range.Start.Line || raw.Range.End.Column < 0 || (raw.Range.End.Line == raw.Range.Start.Line && raw.Range.End.Column < raw.Range.Start.Column) {
				return result, failure("invalid_output", errors.New("ast-grep returned invalid source coordinates"))
			}
			snippet := raw.Text
			shortened := len(snippet) > 1024
			if shortened {
				snippet = snippet[:1024]
				for !utf8.ValidString(snippet) {
					snippet = snippet[:len(snippet)-1]
				}
			}
			match := Match{File: raw.File, Start: Position{raw.Range.Start.Line + 1, raw.Range.Start.Column + 1}, End: Position{raw.Range.End.Line + 1, raw.Range.End.Column + 1}, Text: snippet, SnippetTruncated: shortened}
			encoded, _ := json.Marshal(match)
			if len(result.Matches) >= options.Limit || usedBytes+len(encoded) > visibleBudget {
				result.Truncated = true
				return result, nil
			}
			result.Matches = append(result.Matches, match)
			usedBytes += len(encoded)
		}
		if output.limited {
			return result, nil
		}
	}
	return result, nil
}

func cliError(output execution) error {
	message := strings.TrimSpace(output.stderr)
	if message == "" {
		message = fmt.Sprintf("ast-grep exited %d", output.exit)
	}
	code := "execution_failed"
	lower := strings.ToLower(message)
	if strings.Contains(lower, "pattern") && (strings.Contains(lower, "parse") || strings.Contains(lower, "invalid")) {
		code = "invalid_pattern"
	}
	if strings.Contains(lower, "--lang") || strings.Contains(lower, "unsupported language") {
		code = "unsupported_language"
	}
	return failure(code, errors.New(message))
}
