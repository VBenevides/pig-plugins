package hashline

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadNumericBoundaries(t *testing.T) {
	for _, value := range []any{math.NaN(), math.Inf(1), math.Inf(-1), 1.5, float64(1 << 53), float64(1 << 63), int64(-1), 0} {
		if _, err := positiveInteger(map[string]any{"offset": value}, "offset"); err == nil {
			t.Fatalf("accepted unsupported value %v", value)
		}
	}
	if got, err := positiveInteger(map[string]any{"limit": math.MaxInt}, "limit"); err != nil || got != math.MaxInt {
		t.Fatalf("native int changed: %d %v", got, err)
	}
	if strconv.IntSize == 32 {
		for _, value := range []any{int64(1 << 31), float64(1 << 31)} {
			if _, err := positiveInteger(map[string]any{"limit": value}, "limit"); err == nil {
				t.Fatalf("accepted 32-bit overflow %v", value)
			}
		}
	}
	dir := realTempDir(t)
	for _, text := range []string{"one\ntwo\nthree\n", "one\ntwo\n\xffthree\n"} {
		writeFile(t, filepath.Join(dir, "f.txt"), text, 0600)
		result, err := Read(dir, map[string]any{"path": "f.txt", "offset": 2, "limit": math.MaxInt})
		if err != nil || !strings.Contains(result.Text, "two") || !strings.Contains(result.Text, "three") {
			t.Fatalf("overflowing end: %q %v", result.Text, err)
		}
		if _, err := Read(dir, map[string]any{"path": "f.txt", "offset": math.MaxInt}); err == nil {
			t.Fatal("accepted beyond-end offset")
		}
	}
	for _, anchor := range []string{"0#abcd", "9007199254740992#abcd", "9223372036854775808#abcd", "18446744073709551615#abcd", "18446744073709551616#abcd"} {
		if _, err := parseAnchor(anchor, "anchor"); err == nil {
			t.Fatalf("accepted unsupported anchor %q", anchor)
		}
	}
	if anchor, err := parseAnchor("12#abcd", "anchor"); err != nil || anchor.line != 12 {
		t.Fatalf("valid anchor rejected: %+v %v", anchor, err)
	}
	if strconv.IntSize == 32 {
		if _, err := parseAnchor("2147483648#abcd", "anchor"); err == nil {
			t.Fatal("accepted narrow anchor overflow")
		}
	}
}
