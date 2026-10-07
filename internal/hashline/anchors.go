// Package hashline implements anchored `read` and strict anchored `edit`: the logic of the TypeScript
// harness-hashline extension, without any dependency on the PiG SDK.
//
// Anchor format: `<line>#<hash>`, shown by read as `<line>#<hash>|<text>`.
//   - line is the 1-based line number in the file as read.
//   - hash is the first four hex digits of the SHA-1 of the line's text (without its line terminator,
//     whitespace untouched). A changed line keeps the same hash only with probability 2^-16.
//
// An anchor is valid only when the file's CURRENT line `line` still hashes to `hash`. Anything else is rejected:
// there is no search for the text elsewhere, no fuzzy match and no exact-text fallback.
//
// Every op in one edit call is validated against the same original snapshot, then all ops are applied together.
// Ops that touch the same line or the same gap are ambiguous and rejected as a whole; nothing is applied unless
// every op is valid.
//
// Line terminators: lines are separated by "\n" or "\r\n". Untouched lines keep their own terminator, new lines
// use the file's first terminator style, and a missing final newline stays missing.
package hashline

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// EditError is a problem with the edit request or the file; the edit tool reports it as "edit rejected: ...".
type EditError struct{ Message string }

func (e *EditError) Error() string { return e.Message }

func editErrorf(format string, args ...any) *EditError {
	return &EditError{Message: fmt.Sprintf(format, args...)}
}

// Line is one line of text and its terminator.
type Line struct {
	Text string
	// EOL is "\n", "\r\n" or "" (last line of a file without a final newline).
	EOL string
}

// ParsedFile is a file split into lines.
type ParsedFile struct {
	BOM   bool
	Lines []Line
	// EOL is the terminator for lines an edit creates.
	EOL string
}

const byteOrderMark = "\ufeff"

// LineHash is the first four hex digits of the SHA-1 of text.
func LineHash(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])[:4]
}

// FormatAnchor renders `<line>#<hash>`.
func FormatAnchor(lineNumber int, text string) string {
	return strconv.Itoa(lineNumber) + "#" + LineHash(text)
}

// FormatLine renders `<line>#<hash>|<text>`.
func FormatLine(lineNumber int, text string) string {
	return FormatAnchor(lineNumber, text) + "|" + text
}

// ParseFile splits text into lines. The empty string has no lines, and a final terminator does not add an empty
// line.
func ParseFile(content string) ParsedFile {
	bom := strings.HasPrefix(content, byteOrderMark)
	body := strings.TrimPrefix(content, byteOrderMark)
	var lines []Line
	for start := 0; start < len(body); {
		newline := strings.IndexByte(body[start:], '\n')
		if newline < 0 {
			lines = append(lines, Line{Text: body[start:]})
			break
		}
		newline += start
		crlf := newline > start && body[newline-1] == '\r'
		if crlf {
			lines = append(lines, Line{Text: body[start : newline-1], EOL: "\r\n"})
		} else {
			lines = append(lines, Line{Text: body[start:newline], EOL: "\n"})
		}
		start = newline + 1
	}
	firstEOL := "\n"
	for _, line := range lines {
		if line.EOL != "" {
			firstEOL = line.EOL
			break
		}
	}
	return ParsedFile{BOM: bom, Lines: lines, EOL: firstEOL}
}

// Serialize is the inverse of ParseFile.
func (f ParsedFile) Serialize() string {
	var out strings.Builder
	if f.BOM {
		out.WriteString(byteOrderMark)
	}
	for _, line := range f.Lines {
		out.WriteString(line.Text)
		out.WriteString(line.EOL)
	}
	return out.String()
}

var anchorPattern = regexp.MustCompile(`^(\d+)#([0-9a-f]{4})$`)

// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER; the TypeScript original rejects larger line numbers.
const maxSafeInteger = 1<<53 - 1

type parsedAnchor struct {
	line int
	hash string
}

func parseAnchor(value any, label string) (parsedAnchor, error) {
	text, ok := value.(string)
	if !ok {
		return parsedAnchor{}, editErrorf(`%s: anchor must be a string like "12#a3f9".`, label)
	}
	match := anchorPattern.FindStringSubmatch(jsTrim(text))
	if match == nil {
		return parsedAnchor{}, editErrorf(`%s: "%s" is not a full anchor. Use <line>#<hash> exactly as printed by read, e.g. "12#a3f9".`, label, text)
	}
	line, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil || line > maxSafeInteger || line < 1 {
		return parsedAnchor{}, editErrorf(`%s: line number in "%s" must be >= 1.`, label, text)
	}
	return parsedAnchor{line: int(line), hash: match[2]}, nil
}

func checkAnchor(file ParsedFile, anchor parsedAnchor, label string) error {
	if anchor.line > len(file.Lines) {
		return editErrorf("%s: line %d is out of range; the file has %d line(s). Read the file again.", label, anchor.line, len(file.Lines))
	}
	actual := LineHash(file.Lines[anchor.line-1].Text)
	if actual != anchor.hash {
		return editErrorf("%s: stale anchor %d#%s; line %d now hashes to %s. The file changed since it was read. Read it again; nothing was edited.",
			label, anchor.line, anchor.hash, anchor.line, actual)
	}
	return nil
}

// replacementLines validates the "lines" of an op. A missing "lines" is nil, which is not an array.
func replacementLines(value any, label string) ([]string, error) {
	var lines []string
	switch items := value.(type) {
	case []string:
		lines = items
	case []any:
		lines = make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, editErrorf(`%s: "lines" must be an array of strings, one string per line.`, label)
			}
			lines = append(lines, text)
		}
	default:
		return nil, editErrorf(`%s: "lines" must be an array of strings, one string per line.`, label)
	}
	if len(lines) == 0 {
		return nil, editErrorf(`%s: "lines" must not be empty; use op "delete".`, label)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\r\n") {
			return nil, editErrorf(`%s: each "lines" entry is one line and must not contain a line break.`, label)
		}
	}
	return lines, nil
}

type opKind string

const (
	opReplace      opKind = "replace"
	opInsertBefore opKind = "insert_before"
	opInsertAfter  opKind = "insert_after"
	opDelete       opKind = "delete"
)

type resolved struct {
	index int
	kind  opKind
	// first and last are the 0-based first and last original line replaced or deleted; -1 for inserts.
	first, last int
	// gap is the position before original line `gap` (0-based); -1 unless the op inserts.
	gap   int
	lines []string
}

// EditResult is an edited file and the regions that changed.
type EditResult struct {
	File ParsedFile
	// Regions are changed regions in the NEW file, 1-based inclusive; a pure deletion yields the two lines around it.
	Regions [][2]int
}

func resolveOp(file ParsedFile, raw any, index int) (resolved, error) {
	label := fmt.Sprintf("edits[%d]", index)
	op, ok := raw.(map[string]any)
	if !ok {
		return resolved{}, editErrorf("%s: must be an object.", label)
	}
	kindText, _ := op["op"].(string)
	kind := opKind(kindText)
	if kind != opReplace && kind != opInsertBefore && kind != opInsertAfter && kind != opDelete {
		return resolved{}, editErrorf("%s: op must be one of replace, insert_before, insert_after, delete.", label)
	}
	start, err := parseAnchor(op["anchor"], label+".anchor")
	if err != nil {
		return resolved{}, err
	}
	if err := checkAnchor(file, start, label+".anchor"); err != nil {
		return resolved{}, err
	}
	_, hasEnd := op["end"]
	if kind == opInsertBefore || kind == opInsertAfter {
		if hasEnd {
			return resolved{}, editErrorf(`%s: "end" only applies to replace and delete.`, label)
		}
		lines, err := replacementLines(op["lines"], label)
		if err != nil {
			return resolved{}, err
		}
		gap := start.line
		if kind == opInsertBefore {
			gap = start.line - 1
		}
		return resolved{index: index, kind: kind, first: -1, last: -1, gap: gap, lines: lines}, nil
	}
	last := start.line
	if hasEnd {
		end, err := parseAnchor(op["end"], label+".end")
		if err != nil {
			return resolved{}, err
		}
		if err := checkAnchor(file, end, label+".end"); err != nil {
			return resolved{}, err
		}
		if end.line < start.line {
			return resolved{}, editErrorf("%s: end %s is before anchor %s.", label, op["end"], op["anchor"])
		}
		last = end.line
	}
	if kind == opDelete {
		if _, hasLines := op["lines"]; hasLines {
			return resolved{}, editErrorf(`%s: op delete takes no "lines".`, label)
		}
		return resolved{index: index, kind: kind, first: start.line - 1, last: last - 1, gap: -1}, nil
	}
	lines, err := replacementLines(op["lines"], label)
	if err != nil {
		return resolved{}, err
	}
	return resolved{index: index, kind: kind, first: start.line - 1, last: last - 1, gap: -1, lines: lines}, nil
}

// ApplyEdits validates every op against file (the original snapshot) and returns the edited file. It returns an
// *EditError and changes nothing on the first problem. ops is the decoded JSON array of ops.
func ApplyEdits(file ParsedFile, ops any) (EditResult, error) {
	list, ok := ops.([]any)
	if !ok || len(list) == 0 {
		return EditResult{}, editErrorf(`"edits" must be a non-empty array of ops.`)
	}
	if len(file.Lines) == 0 {
		return EditResult{}, editErrorf("The file is empty, so it has no anchors. Use write to create its content.")
	}
	all := make([]resolved, len(list))
	for index, raw := range list {
		item, err := resolveOp(file, raw, index)
		if err != nil {
			return EditResult{}, err
		}
		all[index] = item
	}

	var ranges []resolved
	for _, item := range all {
		if item.first >= 0 {
			ranges = append(ranges, item)
		}
	}
	sort.SliceStable(ranges, func(a, b int) bool { return ranges[a].first < ranges[b].first })
	for i := 1; i < len(ranges); i++ {
		if ranges[i].first <= ranges[i-1].last {
			return EditResult{}, editErrorf("Ambiguous edits: edits[%d] and edits[%d] cover the same line(s). Merge them into one op. Nothing was edited.",
				ranges[i-1].index, ranges[i].index)
		}
	}
	gaps := map[int]int{}
	for _, item := range all {
		if item.gap < 0 {
			continue
		}
		if earlier, seen := gaps[item.gap]; seen {
			return EditResult{}, editErrorf("Ambiguous edits: edits[%d] and edits[%d] insert at the same position. Merge them into one insert. Nothing was edited.",
				earlier, item.index)
		}
		gaps[item.gap] = item.index
		for _, rng := range ranges {
			if rng.first < item.gap && item.gap <= rng.last {
				return EditResult{}, editErrorf("Ambiguous edits: edits[%d] inserts inside the lines changed by edits[%d]. Nothing was edited.",
					item.index, rng.index)
			}
		}
	}

	replacements := make(map[int]resolved, len(ranges))
	for _, rng := range ranges {
		replacements[rng.first] = rng
	}
	insertAt := make(map[int]resolved, len(gaps))
	for _, item := range all {
		if item.gap >= 0 {
			insertAt[item.gap] = item
		}
	}
	var output []Line
	var regions [][2]int
	created := func(text string) Line { return Line{Text: text, EOL: file.EOL} }
	for index := 0; index <= len(file.Lines); index++ {
		if insert, ok := insertAt[index]; ok {
			regions = append(regions, [2]int{len(output) + 1, len(output) + len(insert.lines)})
			for _, text := range insert.lines {
				output = append(output, created(text))
			}
		}
		if index == len(file.Lines) {
			break
		}
		if rng, ok := replacements[index]; ok {
			if len(rng.lines) > 0 {
				regions = append(regions, [2]int{len(output) + 1, len(output) + len(rng.lines)})
			} else {
				regions = append(regions, [2]int{len(output), len(output) + 1})
			}
			for _, text := range rng.lines {
				output = append(output, created(text))
			}
			index = rng.last
			continue
		}
		output = append(output, file.Lines[index])
	}
	// Keep the file's final-newline state: the last line's terminator is whatever the original last line had.
	if len(output) > 0 {
		output[len(output)-1].EOL = file.Lines[len(file.Lines)-1].EOL
		for i := range len(output) - 1 {
			if output[i].EOL == "" {
				output[i].EOL = file.EOL
			}
		}
	}
	return EditResult{File: ParsedFile{BOM: file.BOM, Lines: output, EOL: file.EOL}, Regions: regions}, nil
}

// jsTrim trims what JavaScript's String.prototype.trim trims: Unicode white space and the byte order mark.
func jsTrim(text string) string { return strings.TrimFunc(text, isJSSpace) }
