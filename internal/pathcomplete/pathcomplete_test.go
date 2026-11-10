package pathcomplete

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFind(t *testing.T) {
	tests := []struct {
		name, line string
		wantPath   string
		wantRaw    string
		ok         bool
	}{
		{"home", "look at ~/Proj", "~/Proj", "~/Proj", true},
		{"relative", "see ./src/", "./src/", "./src/", true},
		{"parent", "../x", "../x", "../x", true},
		{"absolute", "cat /etc/ho", "/etc/ho", "/etc/ho", true},
		{"plain word", "hello wor", "", "", false},
		{"slash inside word", "a/b", "", "", false},
		{"escaped space", `open ~/My\ Pro`, "~/My Pro", `~/My\ Pro`, true},
		{"quoted space", `open "~/My Pro`, "~/My Pro", `"~/My Pro`, true},
		{"escaped quote inside quotes", `"~/a\"b`, `~/a"b`, `"~/a\"b`, true},
		{"after closed quote", `"~/a b" ~/c`, "~/c", "~/c", true},
		{"empty", "", "", "", false},
		{"bare slash is for skills", "text /", "", "", false},
		{"slash word is for skills", "text /skill:ab", "", "", false},
		{"double slash is text", "see //", "", "", false},
		{"double slash path is text", "see //etc/x", "", "", false},
		{"url is text", "https://a/b", "", "", false},
		{"root dir", "see /etc/", "/etc/", "/etc/", true},
		{"trailing space", "~/a ", "", "", false},
		{"unicode before", "é ~/ü", "~/ü", "~/ü", true},
	}
	for _, tt := range tests {
		tok, ok := Find(tt.line, len(tt.line))
		if ok != tt.ok || tok.Path != tt.wantPath || tok.Raw != tt.wantRaw {
			t.Errorf("%s: Find(%q) = %+v, %v; want path %q raw %q ok %v", tt.name, tt.line, tok, ok, tt.wantPath, tt.wantRaw, tt.ok)
		}
	}
}

func TestByteOffsetCountsUTF16(t *testing.T) {
	line := "😀 ~/a" // the emoji is two UTF-16 units and four bytes
	if got := ByteOffset(line, 5); got != len("😀 ~/") {
		t.Fatalf("ByteOffset = %d", got)
	}
	if got := UTF16Len(line); got != 6 {
		t.Fatalf("UTF16Len = %d", got)
	}
	if got := ByteOffset(line, 99); got != len(line) {
		t.Fatalf("past end = %d", got)
	}
}

func TestApply(t *testing.T) {
	tests := []struct {
		name, line, value string
		wantLine          string
		wantCursor        int // bytes into wantLine
	}{
		{"plain dir", "see ~/Pro", "~/Projects/", "see ~/Projects/", len("see ~/Projects/")},
		{"plain file", "see ~/Pro", "~/Projects.txt", "see ~/Projects.txt", len("see ~/Projects.txt")},
		{"space dir wraps and stays inside quote", "see ~/My", "~/My Projects/", `see "~/My Projects/"`, len(`see "~/My Projects/`)},
		{"space file wraps and leaves cursor after quote", "see ~/My", "~/My file.txt", `see "~/My file.txt"`, len(`see "~/My file.txt"`)},
		{"escaped input is replaced by quoted form", `see ~/My\ P`, "~/My Projects/", `see "~/My Projects/"`, len(`see "~/My Projects/`)},
		{"open quote kept without space", `see "~/Pro`, "~/Projects/", `see "~/Projects/"`, len(`see "~/Projects/`)},
		{"quote in name is escaped", "~/a", `~/a"b.txt`, `"~/a\"b.txt"`, len(`"~/a\"b.txt"`)},
	}
	for _, tt := range tests {
		tok, ok := Find(tt.line, len(tt.line))
		if !ok {
			t.Fatalf("%s: no token", tt.name)
		}
		got, cursor := Apply(tt.line, len(tt.line), tok, tt.value)
		if got != tt.wantLine || cursor != tt.wantCursor {
			t.Errorf("%s: Apply = %q, %d; want %q, %d", tt.name, got, cursor, tt.wantLine, tt.wantCursor)
		}
		again, ok := Find(got, cursor)
		if !ok {
			t.Errorf("%s: result %q has no token at cursor", tt.name, got)
			continue
		}
		_ = again
	}
}

func TestApplyReusesExistingClosingQuote(t *testing.T) {
	line := `see "~/My" tail`
	cursor := len(`see "~/My`)
	tok, _ := Find(line, cursor)
	got, at := Apply(line, cursor, tok, "~/My Dir/")
	if got != `see "~/My Dir/" tail` || at != len(`see "~/My Dir/`) {
		t.Fatalf("dir: %q %d", got, at)
	}
	got, at = Apply(line, cursor, tok, "~/My file")
	if got != `see "~/My file" tail` || at != len(`see "~/My file"`) {
		t.Fatalf("file: %q %d", got, at)
	}
}

func TestDecodeAndSplitAndResolve(t *testing.T) {
	if got := Decode(`~/My Projects/x`); got != "~/My Projects/x" {
		t.Errorf("raw spaces: %q", got)
	}
	if got := Decode(`"~/My Projects/x"`); got != "~/My Projects/x" {
		t.Errorf("quoted: %q", got)
	}
	if got := Decode(`~/My\ Projects`); got != "~/My Projects" {
		t.Errorf("escaped: %q", got)
	}
	if d, p := Split("~/a/b"); d != "~/a/" || p != "b" {
		t.Errorf("split: %q %q", d, p)
	}
	if got := Resolve("/h", "/c", "~/x/"); got != "/h/x" {
		t.Errorf("home: %q", got)
	}
	if got := Resolve("/h", "/c", "../x/"); got != "/x" {
		t.Errorf("relative: %q", got)
	}
	if got := Resolve("/h", "/c", "/abs/"); got != "/abs" {
		t.Errorf("absolute: %q", got)
	}
}

func TestListFiltersSortsAndCaps(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"beta", "Alpha dir", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"alpha.txt", "Zed", ".dot"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "beta"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	got, more, err := List(dir, "")
	if err != nil || more != 0 {
		t.Fatalf("err %v more %d", err, more)
	}
	want := []Entry{{"Alpha dir", true}, {"beta", true}, {"link", true}, {"alpha.txt", false}, {"broken", false}, {"Zed", false}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %v, want %v (all %v)", i, got[i], want[i], got)
		}
	}
	got, _, _ = List(dir, "AL")
	if len(got) != 2 {
		t.Fatalf("case-insensitive prefix: %v", got)
	}
	got, _, _ = List(dir, ".")
	if len(got) != 2 {
		t.Fatalf("dot prefix shows hidden: %v", got)
	}
	if got, _, err := List(filepath.Join(dir, "nope"), ""); err != nil || got != nil {
		t.Fatalf("missing dir: %v %v", got, err)
	}
	if got, _, err := List(filepath.Join(dir, "alpha.txt"), ""); err != nil || got != nil {
		t.Fatalf("file as dir: %v %v", got, err)
	}
	many := t.TempDir()
	for i := range MaxEntries + 7 {
		if err := os.WriteFile(filepath.Join(many, string(rune('a'+i%26))+string(rune('a'+i/26))), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, more, _ = List(many, "")
	if len(got) != MaxEntries || more != 7 {
		t.Fatalf("cap: %d entries, %d more", len(got), more)
	}
}

func TestChoicesKeepExactFolderWithoutSlash(t *testing.T) {
	dirs := []Entry{{"reports", true}, {"reports2", true}, {"notes.txt", false}}
	got := Choices("./", "reports", dirs[:2])
	if len(got) != 2 || got[0] != (Choice{"./reports", "reports"}) || got[1] != (Choice{"./reports2/", "reports2/"}) {
		t.Fatalf("exact first without slash: %v", got)
	}
	if got := Choices("./", "reports", dirs[:1]); got != nil {
		t.Fatalf("lone exact match closes the menu: %v", got)
	}
	got = Choices("./", "re", dirs[:1])
	if len(got) != 1 || got[0].Value != "./reports/" {
		t.Fatalf("partial match still adds slash: %v", got)
	}
	got = Choices("./", "", dirs)
	if len(got) != 3 || got[2].Value != "./notes.txt" {
		t.Fatalf("empty prefix: %v", got)
	}
}
