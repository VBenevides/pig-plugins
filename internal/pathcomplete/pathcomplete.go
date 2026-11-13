// Package pathcomplete finds a filesystem path being typed in the prompt and lists the entries that complete it.
//
// A path starts with "~/", "./", "../" or "/dir/". A space inside it is written as "\ " or the whole path sits
// inside double quotes. Inside quotes, \" and \\ stand for " and \.
package pathcomplete

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf16"
)

// MaxEntries is the most entries one listing returns.
const MaxEntries = 50

// Token is the path-like text that ends at the cursor.
type Token struct {
	Start  int    // byte offset in the line where the raw text starts
	Raw    string // raw text from Start to the cursor, as typed
	Path   string // Raw without quotes and escapes
	Quoted bool   // Raw starts with a double quote
	Open   bool   // the quote is still open at the cursor
}

// Entry is one directory entry that matches the typed prefix.
type Entry struct {
	Name string
	Dir  bool
}

// ByteOffset converts a UTF-16 column to a byte offset in line. A column past the end gives len(line).
func ByteOffset(line string, col int) int {
	units := 0
	for i, r := range line {
		if units >= col {
			return i
		}
		units += len(utf16.Encode([]rune{r}))
	}
	return len(line)
}

// UTF16Len returns the length of s in UTF-16 code units.
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}

// Find returns the path token that ends at byte offset cursor of line. The bool is false when the text
// before the cursor is not a path.
func Find(line string, cursor int) (Token, bool) {
	before := line[:cursor]
	tok := Token{Start: -1}
	var path strings.Builder
	escaped := false
	reset := func() {
		tok = Token{Start: -1}
		path.Reset()
		escaped = false
	}
	runes := []rune(before)
	offsets := make([]int, len(runes))
	for i, off := 0, 0; i < len(runes); i++ {
		offsets[i] = off
		off += len(string(runes[i]))
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case tok.Open:
			switch {
			case r == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
				i++
				path.WriteRune(runes[i])
			case r == '"':
				tok.Open = false
			default:
				path.WriteRune(r)
			}
		case escaped:
			path.WriteRune(r)
			escaped = false
		case r == '\\':
			if tok.Start < 0 {
				tok.Start = offsets[i]
			}
			escaped = true
		case unicode.IsSpace(r):
			reset()
		case r == '"' && tok.Start < 0:
			tok = Token{Start: offsets[i], Quoted: true, Open: true}
		default:
			if tok.Start < 0 {
				tok.Start = offsets[i]
			}
			path.WriteRune(r)
		}
	}
	if tok.Start < 0 {
		return Token{}, false
	}
	tok.Raw = before[tok.Start:]
	tok.Path = path.String()
	if !pathLike(tok.Path) {
		return Token{}, false
	}
	return tok, true
}

// pathLike reports whether p starts a path. A bare "/" or "/name" is left to the slash commands and skills
// menu, and "//" is plain text, so an absolute path counts once it has a second slash ("/etc/").
func pathLike(p string) bool {
	if strings.HasPrefix(p, "/") {
		return !strings.HasPrefix(p, "//") && strings.Contains(p[1:], "/")
	}
	return strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
}

// Decode turns text typed after /fs into a path. Quotes and backslash escapes are removed when present;
// otherwise the text is used as typed, so a path with spaces needs no quoting.
func Decode(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, `"`) {
		tok, ok := Find(arg, len(arg))
		if ok {
			return tok.Path
		}
		return strings.TrimPrefix(strings.TrimSuffix(arg, `"`), `"`)
	}
	return strings.ReplaceAll(arg, `\ `, " ")
}

func needsQuote(p string) bool {
	return strings.ContainsFunc(p, func(r rune) bool { return unicode.IsSpace(r) || r == '"' || r == '\\' })
}

// Apply replaces the token with value, the typed form of a chosen entry, and returns the new line and the
// cursor byte offset. A value with spaces (or a token that is already quoted) is wrapped in double quotes.
// After a directory the cursor stays inside the closing quote, so typing goes on in that directory.
func Apply(line string, cursor int, tok Token, value string) (string, int) {
	after := line[cursor:]
	if !tok.Quoted && !needsQuote(value) {
		head := line[:tok.Start] + value
		return head + after, len(head)
	}
	head := line[:tok.Start] + `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	closing := tok.Open && strings.HasPrefix(after, `"`)
	if strings.HasSuffix(value, "/") {
		if closing {
			return head + after, len(head)
		}
		return head + `"` + after, len(head)
	}
	if closing {
		after = after[1:]
	}
	return head + `"` + after, len(head) + 1
}

// Split cuts a typed path at its last slash into the directory text (with the slash) and the name prefix.
func Split(p string) (dir, prefix string) {
	i := strings.LastIndex(p, "/")
	return p[:i+1], p[i+1:]
}

// Resolve gives the directory on disk for typed directory text. "~/" is the home directory and text that is
// not absolute is relative to cwd.
func Resolve(home, cwd, dir string) string {
	switch {
	case dir == "~/" || strings.HasPrefix(dir, "~/"):
		return filepath.Join(home, dir[2:])
	case filepath.IsAbs(dir):
		return filepath.Clean(dir)
	default:
		return filepath.Join(cwd, dir)
	}
}

// List returns the entries of dir whose names start with prefix, directories first, and the number of
// matches left out by MaxEntries. A missing or non-directory dir is not an error: the user is still typing.
// Names that start with a dot show only when prefix starts with a dot.
func List(dir, prefix string) (entries []Entry, more int, err error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("list %s: %w", dir, err)
	}
	showHidden := strings.HasPrefix(prefix, ".")
	lowerPrefix := strings.ToLower(prefix)
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") && !showHidden {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), lowerPrefix) {
			continue
		}
		entries = append(entries, Entry{Name: name, Dir: isDir(dir, item)})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	if len(entries) > MaxEntries {
		more = len(entries) - MaxEntries
		entries = entries[:MaxEntries]
	}
	return entries, more, nil
}

// isDir follows a symlink so a link to a directory completes like a directory. A broken link is a file.
func isDir(dir string, item fs.DirEntry) bool {
	if item.Type()&fs.ModeSymlink == 0 {
		return item.IsDir()
	}
	info, err := os.Stat(filepath.Join(dir, item.Name()))
	return err == nil && info.IsDir()
}

// Value is the typed form of entry inside the typed directory text dir.
func Value(dir string, e Entry) string {
	if e.Dir {
		return dir + e.Name + "/"
	}
	return dir + e.Name
}

// Choice is one completion: Value is the typed form inserted, Label what the menu shows.
type Choice struct {
	Value, Label string
}

// Choices turns the entries for a typed path into menu choices. A folder gets a trailing slash, except the one
// whose name is exactly what was typed: it comes first and keeps no slash, so the path can be sent as typed.
// When that is the only match there is nothing left to complete and no choice is returned.
func Choices(dir, prefix string, entries []Entry) []Choice {
	var exact []Choice
	var rest []Choice
	for _, e := range entries {
		if prefix != "" && e.Name == prefix {
			exact = append(exact, Choice{Value: dir + e.Name, Label: e.Name})
			continue
		}
		rest = append(rest, Choice{Value: Value(dir, e), Label: Value("", e)})
	}
	if len(exact) > 0 && len(rest) == 0 {
		return nil
	}
	return append(exact, rest...)
}
