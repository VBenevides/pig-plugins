package lsp

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/VBenevides/pig-plugins/internal/hashline"
	"github.com/dlclark/regexp2"
)

var Kinds = []string{"any", "function", "method", "class", "interface", "type", "enum", "module", "import", "heading"}
var normalized = map[string]string{"function": "function", "def": "function", "fn": "function", "func": "function", "method": "method", "class": "class", "struct": "class", "impl": "class", "interface": "interface", "trait": "interface", "type": "type", "enum": "enum", "mod": "module", "namespace": "module", "h": "heading"}
var ignoreDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true, "dist": true, "build": true, "target": true, ".next": true, ".cache": true, ".agent-work": true, "vendor": true}
var sourceExt = map[string]bool{}

func init() {
	for _, ext := range strings.Fields("ts tsx js jsx mts cts mjs cjs py pyi go rs java kt kts scala cs c h cc cpp cxx hpp hh swift php sh bash zsh lua md markdown") {
		sourceExt["."+ext] = true
	}
}

type cappedOutput struct {
	data  []byte
	limit int
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-len(b.data) {
		return 0, fmt.Errorf("output exceeds %d bytes", b.limit)
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

// SourceFiles honors gitignore when Git succeeds, otherwise walks without symlink traversal.
func SourceFiles(ctx context.Context, scope string) ([]string, bool, error) {
	info, err := os.Stat(scope)
	if err != nil {
		return nil, false, err
	}
	if info.Mode().IsRegular() {
		return []string{scope}, false, nil
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("not a source file or directory")
	}
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(call, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = scope
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out := &cappedOutput{limit: 16 << 20}
	cmd.Stdout = out
	var files []string
	if err := cmd.Run(); err == nil {
		for _, name := range strings.Split(string(out.data), "\x00") {
			if name != "" {
				files = append(files, filepath.Join(scope, name))
			}
		}
	} else {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		err = filepath.WalkDir(scope, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() && (ignoreDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && path != scope) {
				return filepath.SkipDir
			}
			if len(files) >= 3001 {
				return fs.SkipAll
			}
			if d.Type().IsRegular() {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, false, err
		}
	}
	files = slices.DeleteFunc(files, func(file string) bool {
		if !sourceExt[strings.ToLower(filepath.Ext(file))] {
			return true
		}
		info, err := os.Lstat(file)
		return err != nil || !info.Mode().IsRegular()
	})
	slices.Sort(files)
	files = slices.Compact(files)
	capped := len(files) > 3000
	return files[:min(3000, len(files))], capped, nil
}

type Match struct {
	File             string
	Start, End       int
	Kind, Name, Text string
}
type SearchOptions struct {
	Name, Kind, Body string
	Regex            bool
	Limit            int
}
type SearchResult struct {
	Matches                 []Match
	Files                   []string
	Total, Scanned, Skipped int
	Capped                  bool
}

var importPatterns = map[string]*regexp.Regexp{
	"script": regexp.MustCompile(`^\s*(?:import\b|export\s+(?:\*|\{[^}]*\})\s+from\b|(?:const|let|var)\s+[\w${}\s,]+=\s*require\()`),
	"python": regexp.MustCompile(`^\s*(?:import\s+\S|from\s+\S+\s+import\b)`),
	"go":     regexp.MustCompile(`^\s*import\s+(?:\(|[\w."]+\s*"|")`),
	"rust":   regexp.MustCompile(`^\s*(?:pub\s+)?use\s+`),
	"java":   regexp.MustCompile(`^\s*import\s+`),
	"clike":  regexp.MustCompile(`^\s*#\s*include\b|^\s*import\s+|^\s*using\s+`),
	"shell":  regexp.MustCompile(`^\s*(?:source|\.)\s+\S`),
}

func importPattern(file string) *regexp.Regexp {
	ext := strings.TrimPrefix(filepath.Ext(file), ".")
	group := ""
	switch ext {
	case "ts", "tsx", "js", "jsx", "mjs", "cjs", "mts", "cts":
		group = "script"
	case "py", "pyi":
		group = "python"
	case "go":
		group = "go"
	case "rs":
		group = "rust"
	case "java", "kt", "kts", "scala", "cs":
		group = "java"
	case "c", "h", "cc", "cpp", "cxx", "hpp", "hh", "swift", "php":
		group = "clike"
	case "sh", "bash", "zsh":
		group = "shell"
	}
	return importPatterns[group]
}
func SearchStructure(ctx context.Context, scope string, options SearchOptions) (SearchResult, error) {
	var result SearchResult
	var nameRE, bodyRE *regexp2.Regexp
	var err error
	if options.Regex && options.Name != "" {
		nameRE, err = regexp2.Compile(options.Name, regexp2.ECMAScript|regexp2.IgnoreCase)
		if err != nil {
			return result, fmt.Errorf("query regex: %w", err)
		}
		nameRE.MatchTimeout = 2 * time.Second
	}
	if options.Body != "" {
		bodyRE, err = regexp2.Compile(options.Body, regexp2.ECMAScript)
		if err != nil {
			return result, fmt.Errorf("body regex: %w", err)
		}
		bodyRE.MatchTimeout = 2 * time.Second
	}
	result.Files, result.Capped, err = SourceFiles(ctx, scope)
	if err != nil {
		return result, err
	}
	for _, file := range result.Files {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		text, err := ReadText(file, 1<<20)
		if err != nil {
			result.Skipped++
			continue
		}
		result.Scanned++
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		var found []Match
		if options.Kind == "any" || options.Kind == "import" {
			if re := importPattern(file); re != nil {
				for i, line := range lines {
					if re.MatchString(line) {
						trim := strings.TrimSpace(line)
						found = append(found, Match{file, i + 1, i + 1, "import", trim, trim})
					}
				}
			}
		}
		if options.Kind != "import" {
			symbols, err := hashline.BuildOutline(file, lines)
			if err != nil {
				return result, err
			}
			for _, s := range symbols {
				if options.Kind != "any" && normalized[s.Kind] != options.Kind {
					continue
				}
				declaration := s.Name
				for i := s.Start - 1; i < min(s.End, s.Start+3); i++ {
					trim := strings.TrimSpace(lines[i])
					if trim != "" && !strings.HasPrefix(trim, "@") && !strings.HasPrefix(trim, "#[") {
						declaration = trim
						break
					}
				}
				found = append(found, Match{file, s.Start, s.End, s.Kind, s.Name, declaration})
			}
		}
		for _, m := range found {
			if options.Name != "" {
				if nameRE != nil {
					hit, err := nameRE.MatchString(m.Name)
					if err != nil {
						return result, err
					}
					if !hit {
						continue
					}
				} else if !strings.Contains(strings.ToLower(m.Name), strings.ToLower(options.Name)) {
					continue
				}
			}
			if bodyRE != nil {
				hit := false
				for _, line := range lines[m.Start-1 : m.End] {
					matched, err := bodyRE.MatchString(line)
					if err != nil {
						return result, err
					}
					if matched {
						hit = true
						break
					}
				}
				if !hit {
					continue
				}
			}
			result.Total++
			if len(result.Matches) < options.Limit {
				result.Matches = append(result.Matches, m)
			}
		}
	}
	return result, nil
}
func FormatMatches(result SearchResult, cwd string) string {
	var rows []string
	for _, m := range result.Matches {
		rel, _ := filepath.Rel(cwd, m.File)
		where := fmt.Sprintf("%s:%d", rel, m.Start)
		if m.End > m.Start {
			where += fmt.Sprintf("-%d", m.End)
		}
		row := fmt.Sprintf("%s  %s %s", where, m.Kind, m.Name)
		if m.Name != m.Text {
			row += "  | " + m.Text
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}
func Overview(ctx context.Context, scope, cwd string, depth, maxFiles int) (string, error) {
	info, err := os.Stat(scope)
	if err != nil {
		return "", err
	}
	if info.Mode().IsRegular() {
		text, err := ReadText(scope, 1<<20)
		if err != nil {
			return "", err
		}
		symbols, err := hashline.BuildOutline(scope, strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n"))
		if err != nil {
			return "", err
		}
		return hashline.RenderOutline(symbols), nil
	}
	files, capped, err := SourceFiles(ctx, scope)
	if err != nil {
		return "", err
	}
	var rows []string
	shown, deeper := 0, 0
	for _, file := range files {
		rel, _ := filepath.Rel(scope, file)
		if strings.Count(rel, string(filepath.Separator)) > depth {
			deeper++
			continue
		}
		if shown >= maxFiles {
			break
		}
		text, err := ReadText(file, 1<<20)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		symbols, err := hashline.BuildOutline(file, lines)
		if err != nil {
			return "", err
		}
		var top []string
		for _, s := range symbols {
			if s.Depth == 0 {
				top = append(top, s.Kind+" "+s.Name)
			}
		}
		display, _ := filepath.Rel(cwd, file)
		row := fmt.Sprintf("%s (%d lines, %d symbols)", display, len(lines), len(symbols))
		if len(top) > 0 {
			row += ": " + strings.Join(top[:min(10, len(top))], ", ")
			if len(top) > 10 {
				row += fmt.Sprintf(", +%d more", len(top)-10)
			}
		}
		rows = append(rows, row)
		shown++
	}
	if deeper > 0 {
		rows = append(rows, fmt.Sprintf("... %d more file(s) deeper than depth %d; raise depth or pass a subdirectory.", deeper, depth))
	}
	if len(files) > shown+deeper {
		rows = append(rows, fmt.Sprintf("... %d more file(s) not shown (raise max_files or pass a subdirectory).", len(files)-shown-deeper))
	}
	if capped {
		rows = append(rows, "Only the first 3000 source files were considered.")
	}
	return strings.Join(rows, "\n"), nil
}
