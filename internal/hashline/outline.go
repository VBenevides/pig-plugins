package hashline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dlclark/regexp2"
)

// Symbol outline of a source file, built from regular expressions and indentation.
//
// There is no parser behind this: Tree-sitter and language servers are not available here, so the outline is a
// heuristic. It is reliable for conventionally formatted code and can miss or mis-name symbols in unusual
// layouts. Every reported range is a valid 1-based inclusive range inside the file, which read can fetch with
// offset and limit.
//
// How ranges are found: a symbol starts at a line that matches a language pattern (decorators and attributes
// directly above it are included). It ends before the next non-blank line whose indentation is not deeper than
// the symbol's own; for brace languages a closing bracket line at that indentation is included in the range.
// Markdown headings end before the next heading of the same or a higher level.

// Symbol is one outline entry.
type Symbol struct {
	Start, End int
	Kind, Name string
	Depth      int
}

type language string

const (
	langScript   language = "script"
	langPython   language = "python"
	langGo       language = "go"
	langRust     language = "rust"
	langJava     language = "java"
	langCLike    language = "clike"
	langShell    language = "shell"
	langMarkdown language = "markdown"
)

var extensions = map[string]language{
	"ts": langScript, "tsx": langScript, "js": langScript, "jsx": langScript, "mjs": langScript, "cjs": langScript, "mts": langScript, "cts": langScript,
	"py": langPython, "pyi": langPython,
	"go":   langGo,
	"rs":   langRust,
	"java": langJava, "kt": langJava, "kts": langJava, "scala": langJava, "cs": langJava,
	"c": langCLike, "h": langCLike, "cc": langCLike, "cpp": langCLike, "cxx": langCLike, "hpp": langCLike, "hh": langCLike, "swift": langCLike, "php": langCLike,
	"sh": langShell, "bash": langShell, "zsh": langShell,
	"md": langMarkdown, "markdown": langMarkdown,
}

// languageOf returns the outline language of a file name, or "" when there is none.
func languageOf(filePath string) language {
	base := filePath
	if slash := strings.LastIndexByte(base, '/'); slash >= 0 {
		base = base[slash+1:]
	}
	dot := strings.LastIndexByte(base, '.')
	if dot < 0 {
		return ""
	}
	return extensions[strings.ToLower(base[dot+1:])]
}

type pattern struct {
	regex *regexp2.Regexp
	kind  string
	group int
	// container symbols have children (members are only listed inside these).
	container bool
	// member symbols are only matched when nested in a container.
	member bool
}

// The patterns need negative lookahead, which Go's regexp does not have, so they run on regexp2 in ECMAScript
// mode: the TypeScript pattern strings are used unchanged.
const (
	mods    = `(?:(?:public|private|protected|internal|static|final|abstract|override|virtual|async|export|default|declare|readonly|sealed|open|suspend|inline|unsafe|extern|const|pub(?:\([^)]*\))?)\s+)*`
	notCall = `(?!(?:if|for|while|switch|catch|return|else|do|try|new|throw|await|yield|delete|typeof|function|super|this)\b)`
)

// matchTimeout bounds one pattern on one line. A runaway pattern is an error, not a silently missing symbol.
const matchTimeout = 5 * time.Second

func compile(source string, kind string, group int, container, member bool) pattern {
	re := regexp2.MustCompile(source, regexp2.ECMAScript)
	re.MatchTimeout = matchTimeout
	return pattern{regex: re, kind: kind, group: group, container: container, member: member}
}

var patterns = map[language][]pattern{
	langScript: {
		compile(`^\s*`+mods+`(?:abstract\s+)?class\s+([\w$]+)`, "class", 1, true, false),
		compile(`^\s*`+mods+`interface\s+([\w$]+)`, "interface", 1, true, false),
		compile(`^\s*`+mods+`(?:const\s+)?enum\s+([\w$]+)`, "enum", 1, true, false),
		compile(`^\s*`+mods+`namespace\s+([\w$.]+)`, "namespace", 1, true, false),
		compile(`^\s*`+mods+`type\s+([\w$]+)\s*(?:<[^=]*>)?\s*=`, "type", 1, false, false),
		compile(`^\s*`+mods+`function\*?\s+([\w$]+)`, "function", 1, false, false),
		compile(`^\s*`+mods+`(?:const|let|var)\s+([\w$]+)\s*(?::[^=]+)?=\s*(?:async\s*)?(?:function\b|\([^)]*\)\s*(?::[^=]+)?=>|[\w$]+\s*=>)`, "function", 1, false, false),
		compile(`^\s+`+mods+`(?:get\s+|set\s+|\*\s*)?`+notCall+`([\w$#]+)\s*(?:<[^>(]*>)?\s*\([^;]*$`, "method", 1, false, true),
	},
	langPython: {
		compile(`^\s*class\s+(\w+)`, "class", 1, true, false),
		compile(`^\s*(?:async\s+)?def\s+(\w+)`, "def", 1, false, false),
	},
	langGo: {
		compile(`^func\s+\(\s*\w*\s*\*?\s*([\w.\[\]]+)\s*\)\s*(\w+)`, "method", 2, false, false),
		compile(`^func\s+(\w+)`, "func", 1, false, false),
		compile(`^type\s+(\w+)\s+(?:struct|interface)`, "type", 1, false, false),
		compile(`^type\s+(\w+)`, "type", 1, false, false),
	},
	langRust: {
		compile(`^\s*`+mods+`(?:unsafe\s+)?impl(?:<[^>]*>)?\s+([^{]+?)\s*(?:\{|where|$)`, "impl", 1, true, false),
		compile(`^\s*`+mods+`trait\s+(\w+)`, "trait", 1, true, false),
		compile(`^\s*`+mods+`mod\s+(\w+)\s*\{`, "mod", 1, true, false),
		compile(`^\s*`+mods+`struct\s+(\w+)`, "struct", 1, false, false),
		compile(`^\s*`+mods+`enum\s+(\w+)`, "enum", 1, false, false),
		compile(`^\s*`+mods+`(?:async\s+)?(?:unsafe\s+)?fn\s+(\w+)`, "fn", 1, false, false),
	},
	langJava: {
		compile(`^\s*`+mods+`(?:data\s+|sealed\s+)?(?:class|interface|enum|record|object|trait)\s+(\w+)`, "class", 1, true, false),
		compile(`^\s+`+mods+`(?:fun\s+)?(?:<[^>]+>\s+)?(?:[\w.<>\[\],?]+\s+)?`+notCall+`(\w+)\s*\([^;]*$`, "method", 1, false, true),
	},
	langCLike: {
		compile(`^\s*(?:typedef\s+)?(?:struct|class|enum|union|namespace|interface|trait)\s+(\w+)`, "type", 1, true, false),
		compile(`^(?:`+mods+`)(?:[\w:*&<>\[\],~]+\s+)+[*&]*`+notCall+`([\w:~]+)\s*\([^;]*$`, "function", 1, false, false),
		compile(`^\s+`+mods+`(?:[\w:*&<>\[\],~]+\s+)*[*&]*`+notCall+`([\w:~]+)\s*\([^;]*$`, "method", 1, false, true),
	},
	langShell: {
		compile(`^\s*function\s+([\w:.-]+)`, "function", 1, false, false),
		compile(`^\s*([\w:.-]+)\s*\(\)\s*\{?`, "function", 1, false, false),
	},
}

type candidate struct {
	Symbol
	// declaration is the line of the declaration itself; Start may be higher up because of decorators.
	declaration int
	container   bool
	member      bool
	indent      int
}

func indentOf(line string) int {
	width := 0
	for _, char := range line {
		switch char {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return width
		}
	}
	return width
}

func isJSSpace(r rune) bool { return r == '\ufeff' || (unicode.IsSpace(r) && r != 0x85) }

func isBlank(line string) bool { return jsTrim(line) == "" }

var (
	fenceMarker = regexp.MustCompile("^\\s{0,3}(`{3,}|~{3,})")
	headingLine = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
)

func markdownOutline(lines []string) []Symbol {
	type heading struct {
		line, level int
		title       string
	}
	var headings []heading
	var fence byte
	for index, text := range lines {
		if marker := fenceMarker.FindStringSubmatch(text); marker != nil {
			if fence == 0 {
				fence = marker[1][0]
			} else if marker[1][0] == fence {
				fence = 0
			}
			continue
		}
		if fence != 0 {
			continue
		}
		if match := headingLine.FindStringSubmatch(text); match != nil {
			headings = append(headings, heading{line: index + 1, level: len(match[1]), title: match[2]})
		}
	}
	symbols := make([]Symbol, 0, len(headings))
	for index, h := range headings {
		end := len(lines)
		for _, next := range headings[index+1:] {
			if next.level <= h.level {
				end = next.line - 1
				break
			}
		}
		for end > h.line && isBlank(lines[end-1]) {
			end--
		}
		symbols = append(symbols, Symbol{Start: h.line, End: end, Kind: "h" + strconv.Itoa(h.level), Name: h.title, Depth: h.level - 1})
	}
	return symbols
}

var (
	pythonSignatureEnd = regexp.MustCompile(`^\s*\)[^#]*:\s*(?:#.*)?$`)
	braceCloser        = regexp.MustCompile(`^\s*[}\]]`)
	signatureContinues = regexp.MustCompile(`^\s*[{)]`)
	pythonDecorator    = regexp.MustCompile(`^\s*@\w`)
	otherDecorator     = regexp.MustCompile(`^\s*(?:@\w|#\[)`)
)

// findEnd returns the last line of the symbol whose declaration is on c.declaration.
func findEnd(c candidate, lines []string, lang language) int {
	braces := lang != langPython
	end := c.declaration
	for cursor := c.declaration + 1; cursor <= len(lines); cursor++ {
		text := lines[cursor-1]
		if isBlank(text) {
			continue
		}
		indent := indentOf(text)
		if indent > c.indent {
			end = cursor
			continue
		}
		if indent != c.indent {
			break
		}
		if !braces {
			if pythonSignatureEnd.MatchString(text) {
				end = cursor
				continue
			}
			break
		}
		if signatureContinues.MatchString(text) {
			end = cursor
			continue
		}
		if braceCloser.MatchString(text) {
			end = cursor
		}
		break
	}
	return end
}

// BuildOutline returns the outline entries in file order, or none for unknown languages and files without
// symbols. A pattern that exceeds its time limit is an error.
func BuildOutline(filePath string, lines []string) ([]Symbol, error) {
	lang := languageOf(filePath)
	if lang == "" {
		return nil, nil
	}
	if lang == langMarkdown {
		return markdownOutline(lines), nil
	}
	languagePatterns := patterns[lang]
	blockComments := lang != langPython && lang != langShell
	hashComments := lang == langPython || lang == langShell
	var candidates []candidate
	inBlockComment := false
	for index, text := range lines {
		if blockComments {
			if inBlockComment {
				if strings.Contains(text, "*/") {
					inBlockComment = false
				}
				continue
			}
			if open := strings.Index(text, "/*"); open >= 0 && !strings.Contains(text[open+2:], "*/") {
				inBlockComment = true
				if isBlank(text[:open]) {
					continue
				}
			}
		}
		trimmed := strings.TrimLeftFunc(text, isJSSpace)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if hashComments && strings.HasPrefix(trimmed, "#") {
			continue
		}
		if blockComments && strings.HasPrefix(trimmed, "*") {
			continue
		}
		for _, p := range languagePatterns {
			match, err := p.regex.FindStringMatch(text)
			if err != nil {
				return nil, fmt.Errorf("outline of %s, line %d: %w", filePath, index+1, err)
			}
			if match == nil {
				continue
			}
			name := ""
			if group := match.GroupByNumber(p.group); group != nil {
				name = jsTrim(group.String())
			}
			if name == "" {
				continue
			}
			candidates = append(candidates, candidate{
				Symbol:      Symbol{Start: index + 1, End: index + 1, Kind: p.kind, Name: name},
				declaration: index + 1, container: p.container, member: p.member, indent: indentOf(text),
			})
			break
		}
	}

	decorator := otherDecorator
	if lang == langPython {
		decorator = pythonDecorator
	}
	for i := range candidates {
		c := &candidates[i]
		// Decorators and attributes directly above, at the same indentation, belong to the symbol.
		first := c.declaration
		for first > 1 {
			above := lines[first-2]
			if !decorator.MatchString(above) || indentOf(above) != c.indent {
				break
			}
			first--
		}
		c.Start = first
		c.End = findEnd(*c, lines, lang)
	}

	// Members need an enclosing container; anything nested in a function is dropped.
	var result []Symbol
	var stack []candidate
	for _, c := range candidates {
		for len(stack) > 0 && stack[len(stack)-1].End < c.declaration {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 && !stack[len(stack)-1].container {
			continue
		}
		if len(stack) == 0 && c.member {
			continue
		}
		result = append(result, Symbol{Start: c.Start, End: c.End, Kind: c.Kind, Name: c.Name, Depth: len(stack)})
		stack = append(stack, c)
	}
	return result, nil
}

// MaxOutlineEntries caps the rendered outline.
const MaxOutlineEntries = 300

// RenderOutline is one line per symbol: indentation shows nesting, then `start-end kind name`.
func RenderOutline(symbols []Symbol) string {
	shown := symbols
	if len(shown) > MaxOutlineEntries {
		shown = shown[:MaxOutlineEntries]
	}
	width := 0
	for _, s := range shown {
		width = max(width, len(fmt.Sprintf("%d-%d", s.Start, s.End)))
	}
	rows := make([]string, 0, len(shown)+1)
	for _, s := range shown {
		rows = append(rows, fmt.Sprintf("%s%-*s  %s %s", strings.Repeat("  ", s.Depth), width, fmt.Sprintf("%d-%d", s.Start, s.End), s.Kind, s.Name))
	}
	if len(symbols) > len(shown) {
		rows = append(rows, fmt.Sprintf("... %d more symbol(s) not listed", len(symbols)-len(shown)))
	}
	return strings.Join(rows, "\n")
}
