package hashline

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
)

// A bare read of a file above either limit returns an outline instead of the text.
const (
	LargeFileLines = 400
	LargeFileBytes = 24 * 1024
	// MaxRangeLines and MaxRangeBytes cap an explicit range.
	MaxRangeLines = 2000
	MaxRangeBytes = 50 * 1024
	MaxLineChars  = 2000
	MaxEditBytes  = 16 * 1024 * 1024
	// MaxImageBytes bounds an image attachment; Pi's own read resizes images, this port does not.
	MaxImageBytes = 5 * 1024 * 1024

	resultContextLines = 2
	resultMaxLines     = 80
)

// Image is an image attachment of a read result.
type Image struct {
	Data     string // base64
	MimeType string
}

// Result is what a tool returns to the model.
type Result struct {
	Text    string
	Details map[string]any
	Images  []Image
}

// ReadDescription, ReadSnippet, ReadGuidelines, EditDescription, EditSnippet and EditGuidelines are the model-facing
// text of the two tools.
var (
	ReadDescription = "Read a file. Text lines are printed as <line>#<hash>|text; the <line>#<hash> anchor is what edit takes. " +
		fmt.Sprintf("A bare read of a small file returns it all. A large source file (over %d lines or %d KB) returns an outline of symbols with line ranges instead; read the range you need ", LargeFileLines, LargeFileBytes/1024) +
		"with offset (first line, 1-based) and limit (number of lines). offset/limit always return exactly that range. " +
		"Images are returned as attachments."
	ReadSnippet    = "Read file contents as anchored lines; large source files return a symbol outline"
	ReadGuidelines = []string{
		"Use read to examine files instead of cat or sed. For a large file, read the outline first, then the range you need.",
	}
	EditDescription = "Edit one existing text file with the line anchors printed by read (<line>#<hash>). Every op is checked against " +
		"the file as it was read: if any anchor no longer matches its line, the whole call is rejected, nothing changes, " +
		"and you must read again. Text is never searched for. Ops: replace (anchor, optional end for a range, lines), " +
		"insert_before / insert_after (anchor, lines), delete (anchor, optional end). lines is an array with one string " +
		"per line, without line breaks. All ops use the anchors of the original read; ops in one call must not overlap " +
		"and must not insert at the same position. Create files with write."
	EditSnippet    = "Edit a file with line anchors from read; stale or ambiguous anchors are rejected"
	EditGuidelines = []string{
		"Read the lines you want to change first, and copy anchors exactly as printed (for example 12#a3f9) when using edit.",
		"Put all changes to one file in a single edit call; every op refers to the original anchors, not to earlier ops.",
	}
)

// ReadSchema and EditSchema are the JSON schemas of the tool parameters.
func ReadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Path to the file to read (relative or absolute)"},
			"offset": map[string]any{"type": "number", "description": "Line number to start reading from (1-indexed)"},
			"limit":  map[string]any{"type": "number", "description": "Maximum number of lines to read"},
		},
		"required": []string{"path"},
	}
}

func EditSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path to the file to edit (relative or absolute)"},
			"edits": map[string]any{
				"type":        "array",
				"description": "Anchored ops, all validated against the file as read.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"op":     map[string]any{"type": "string", "enum": []string{"replace", "insert_before", "insert_after", "delete"}},
						"anchor": map[string]any{"type": "string", "description": "First (or only) line, as <line>#<hash> from read."},
						"end":    map[string]any{"type": "string", "description": "Last line of a range for replace/delete, as <line>#<hash>."},
						"lines":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "New lines, one string per line."},
					},
					"required": []string{"op", "anchor"},
				},
			},
		},
		"required": []string{"path", "edits"},
	}
}

// jsonValue renders a value like JSON.stringify, for error messages.
func jsonValue(value any) string {
	if number, ok := value.(float64); ok {
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return "null"
		}
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

// positiveInteger reads an optional integer >= 1 parameter; 0 means absent.
func positiveInteger(params map[string]any, name string) (int, error) {
	value, present := params[name]
	if !present {
		return 0, nil
	}
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	default:
		return 0, fmt.Errorf("%s must be an integer >= 1, got %s.", name, jsonValue(value))
	}
	if number != math.Trunc(number) || math.IsInf(number, 0) || number < 1 {
		return 0, fmt.Errorf("%s must be an integer >= 1, got %s.", name, jsonValue(value))
	}
	return int(min(number, 1<<40)), nil
}

func kilobytes(bytes int) string {
	// bytes/1024 is exactly representable, so adding one half before flooring rounds half up like toFixed.
	return fmt.Sprintf("%.1f KB", math.Floor(float64(bytes)/1024*10+0.5)/10)
}

func utf16Length(text string) int {
	if len(text) <= MaxLineChars { // UTF-16 units never outnumber UTF-8 bytes
		return len(text)
	}
	count := 0
	for _, r := range text {
		count += utf16.RuneLen(r)
	}
	return count
}

// firstUTF16 returns the first n UTF-16 units of text; a split surrogate pair becomes U+FFFD.
func firstUTF16(text string, n int) string {
	units := utf16.Encode([]rune(text))
	return string(utf16.Decode(units[:n]))
}

// renderWindow renders anchored lines from..to, stopping at the line or byte cap, and reports the last line shown.
func renderWindow(file ParsedFile, from, to, maxLines, maxBytes int) (body string, last int) {
	var rows []string
	size := 0
	last = from - 1
	for n := from; n <= to; n++ {
		original := file.Lines[n-1].Text
		shown := original
		if length := utf16Length(original); length > MaxLineChars {
			shown = fmt.Sprintf("%s …[line truncated, %d more chars]", firstUTF16(original, MaxLineChars), length-MaxLineChars)
		}
		// The anchor hashes the full line, so a truncated line can still be edited by anchor.
		row := FormatAnchor(n, original) + "|" + shown
		rowSize := len(row) + 1
		if len(rows) > 0 && (len(rows) >= maxLines || size+rowSize > maxBytes) {
			break
		}
		rows = append(rows, row)
		size += rowSize
		last = n
	}
	return strings.Join(rows, "\n"), last
}

func rangeResult(file ParsedFile, from, to, maxLines, maxBytes int, mode string) Result {
	body, last := renderWindow(file, from, to, maxLines, maxBytes)
	stoppedEarly := last < to
	notice := ""
	if stoppedEarly {
		notice = fmt.Sprintf("\n[Showing lines %d-%d of %d; output cap reached. Continue with offset=%d.]", from, last, len(file.Lines), last+1)
	}
	return Result{Text: body + notice, Details: map[string]any{
		"mode": mode, "from": from, "to": last, "totalLines": len(file.Lines), "truncated": stoppedEarly,
	}}
}

func largeFileResult(filePath string, file ParsedFile, size int) (Result, error) {
	texts := make([]string, len(file.Lines))
	for i, line := range file.Lines {
		texts[i] = line.Text
	}
	symbols, err := BuildOutline(filePath, texts)
	if err != nil {
		return Result{}, err
	}
	dimensions := fmt.Sprintf("%d lines, %s", len(file.Lines), kilobytes(size))
	if len(symbols) == 0 {
		head := rangeResult(file, 1, len(file.Lines), LargeFileLines, LargeFileBytes, "head")
		shown := head.Details["to"].(int)
		return Result{
			Text: fmt.Sprintf("%s is large (%s) and no symbols were detected (the outline uses regular expressions and indentation, "+
				"not a parser, and knows only common source and markdown files). Showing lines 1-%d; "+
				"read the rest with offset and limit.\n\n%s", filePath, dimensions, shown, head.Text),
			Details: head.Details,
		}, nil
	}
	return Result{
		Text: fmt.Sprintf("%s is large (%s), so this is an outline instead of the text. Symbols come from regular "+
			"expressions and indentation, not a parser (Tree-sitter and language servers are not available), so ranges "+
			"can be slightly off for unusual layouts. Ranges are 1-based, inclusive; nesting is shown by indentation.\n\n"+
			"%s\n\n"+
			"To see code, call read again with path and offset/limit for a range above (for example offset=%d, "+
			"limit=%d). Lines come back as <line>#<hash>|text; use those anchors with edit.",
			filePath, dimensions, RenderOutline(symbols), symbols[0].Start, symbols[0].End-symbols[0].Start+1),
		Details: map[string]any{"mode": "outline", "symbols": len(symbols), "totalLines": len(file.Lines)},
	}, nil
}

// Read implements the read tool. Errors are returned to the model as tool errors.
func Read(cwd string, params map[string]any) (Result, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return Result{}, errors.New("path must be a non-empty string.")
	}
	offset, err := positiveInteger(params, "offset")
	if err != nil {
		return Result{}, err
	}
	limit, err := positiveInteger(params, "limit")
	if err != nil {
		return Result{}, err
	}
	absolute := ResolveToolPath(path, cwd)
	stat, err := os.Stat(absolute)
	if err != nil {
		return Result{}, fmt.Errorf("%s: no such file or directory, access '%s'", errnoName(err), absolute)
	}
	if stat.IsDir() {
		return Result{}, fmt.Errorf("EISDIR: illegal operation on a directory, read '%s'; list a directory with bash (ls).", absolute)
	}
	if !stat.Mode().IsRegular() {
		return Result{}, fmt.Errorf("%s is not a regular file.", absolute)
	}
	if stat.Size() > MaxEditBytes {
		return Result{}, fmt.Errorf("%s is larger than %d bytes and cannot be anchored; inspect part of it with bash (head, tail, sed -n).", absolute, MaxEditBytes)
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", absolute, err)
	}
	if IsBinary(content) {
		return readNonText(path, absolute, content, offset, limit)
	}
	file := ParseFile(string(content))
	total := len(file.Lines)
	if total == 0 {
		return Result{Text: "(empty file)", Details: map[string]any{"mode": "full", "totalLines": 0}}, nil
	}
	if offset == 0 && limit == 0 {
		if total > LargeFileLines || len(content) > LargeFileBytes {
			return largeFileResult(path, file, len(content))
		}
		return rangeResult(file, 1, total, math.MaxInt, math.MaxInt, "full"), nil
	}
	from := max(offset, 1)
	if from > total {
		return Result{}, fmt.Errorf("Offset %d is beyond the end of the file (%d lines).", from, total)
	}
	to := total
	if limit > 0 {
		to = min(total, from+limit-1)
	}
	return rangeResult(file, from, to, MaxRangeLines, MaxRangeBytes, "range"), nil
}

// readNonText handles what anchors cannot address: images are attached, text in another encoding is shown
// without anchors (and cannot be edited), NUL-containing content is refused.
func readNonText(path, absolute string, content []byte, offset, limit int) (Result, error) {
	head := content[:min(len(content), 8192)]
	if mime := ImageMIME(head); mime != "" {
		if len(content) > MaxImageBytes {
			return Result{}, fmt.Errorf("%s is an image of %d bytes; images above %d bytes are not attached.", absolute, len(content), MaxImageBytes)
		}
		return Result{
			Text:    fmt.Sprintf("Read image file [%s]", mime),
			Details: map[string]any{"mode": "image", "mimeType": mime, "bytes": len(content)},
			Images:  []Image{{Data: base64.StdEncoding.EncodeToString(content), MimeType: mime}},
		}, nil
	}
	if isBMP(head) || strings.IndexByte(string(head), 0) >= 0 {
		return Result{}, fmt.Errorf("%s is a binary file; read does not print binary content.", path)
	}
	// Invalid UTF-8 without NUL bytes: show it as text with replacement characters, as Pi's own read does.
	file := ParseFile(strings.ToValidUTF8(string(content), "\ufffd"))
	lines := make([]string, len(file.Lines))
	for i, line := range file.Lines {
		lines[i] = line.Text
	}
	from := max(offset, 1)
	if from > len(lines) {
		return Result{}, fmt.Errorf("Offset %d is beyond the end of the file (%d lines).", from, len(lines))
	}
	to := len(lines)
	if limit > 0 {
		to = min(len(lines), from+limit-1)
	}
	to = min(to, from+MaxRangeLines-1)
	text := strings.Join(lines[from-1:to], "\n")
	for len(text) > MaxRangeBytes {
		to--
		text = strings.Join(lines[from-1:to], "\n")
	}
	notice := "\n[This file is not valid UTF-8; bytes that do not decode are shown as U+FFFD. Lines carry no anchors, and edit refuses the file.]"
	if to < len(lines) {
		notice += fmt.Sprintf("\n[Showing lines %d-%d of %d. Continue with offset=%d.]", from, to, len(lines), to+1)
	}
	return Result{Text: text + notice, Details: map[string]any{"mode": "plain", "from": from, "to": to, "totalLines": len(lines)}}, nil
}

// editLocks serialises edits of one file in this process; entries are removed when the last holder leaves.
var editLocks = struct {
	sync.Mutex
	held map[string]*lockEntry
}{held: map[string]*lockEntry{}}

type lockEntry struct {
	mu      sync.Mutex
	holders int
}

func lockPath(path string) (unlock func()) {
	key := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		key = resolved
	}
	editLocks.Lock()
	entry := editLocks.held[key]
	if entry == nil {
		entry = &lockEntry{}
		editLocks.held[key] = entry
	}
	entry.holders++
	editLocks.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		editLocks.Lock()
		entry.holders--
		if entry.holders == 0 {
			delete(editLocks.held, key)
		}
		editLocks.Unlock()
	}
}

// Edit implements the edit tool. A problem with the request or the file is an error starting "edit rejected: ".
func Edit(cwd string, params map[string]any) (Result, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return Result{}, errors.New("path must be a non-empty string.")
	}
	absolute := ResolveToolPath(path, cwd)
	unlock := lockPath(absolute)
	defer unlock()
	result, err := editLocked(cwd, absolute, params["edits"], nil)
	var rejected *EditError
	if errors.As(err, &rejected) {
		return Result{}, fmt.Errorf("edit rejected: %s", rejected.Message)
	}
	return result, err
}

func editLocked(cwd, absolute string, edits any, beforeCommit func()) (Result, error) {
	snapshot, err := TakeSnapshot(absolute, cwd, MaxEditBytes)
	if err != nil {
		return Result{}, err
	}
	original := string(snapshot.Bytes)
	edited, err := ApplyEdits(ParseFile(original), edits)
	if err != nil {
		return Result{}, err
	}
	updated := edited.File.Serialize()
	if updated == original {
		return Result{
			Text:    fmt.Sprintf("No change: the edit produces identical content, %s was left untouched.", absolute),
			Details: map[string]any{"changed": false},
		}, nil
	}
	if err := ReplaceAtomically(snapshot, updated, beforeCommit); err != nil {
		return Result{}, err
	}
	regions := make([][2]int, len(edited.Regions))
	copy(regions, edited.Regions)
	return Result{
		Text:    editSummary(absolute, len(edits.([]any)), edited.File, edited.Regions),
		Details: map[string]any{"changed": true, "regions": regions},
	}, nil
}

func editSummary(absolute string, ops int, file ParsedFile, regions [][2]int) string {
	sorted := append([][2]int(nil), regions...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a][0] < sorted[b][0] })
	var wanted [][2]int
	for _, region := range sorted {
		window := [2]int{max(1, region[0]-resultContextLines), min(len(file.Lines), region[1]+resultContextLines)}
		if n := len(wanted); n > 0 && window[0] <= wanted[n-1][1]+1 {
			wanted[n-1][1] = max(wanted[n-1][1], window[1])
		} else {
			wanted = append(wanted, window)
		}
	}
	var rows []string
	for _, window := range wanted {
		if len(rows) > 0 {
			rows = append(rows, "...")
		}
		for n := window[0]; n <= window[1] && len(rows) < resultMaxLines; n++ {
			rows = append(rows, FormatLine(n, file.Lines[n-1].Text))
		}
	}
	truncated := ""
	if len(rows) >= resultMaxLines {
		truncated = "\n[more changed lines not shown; read again for their anchors]"
	}
	return fmt.Sprintf("Applied %d edit(s) to %s. Anchors of the changed region now (line numbers moved):\n%s%s", ops, absolute, strings.Join(rows, "\n"), truncated)
}
