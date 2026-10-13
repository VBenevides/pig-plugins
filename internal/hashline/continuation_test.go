package hashline

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var continuationPattern = regexp.MustCompile(`Continue with offset=(\d+)\.`)

// Following every continuation notice must cover each line exactly once, with
// no gap and no loop, under both the line cap and the byte cap.
func TestExplicitRangeContinuationCoversFileWithoutGapsOrLoops(t *testing.T) {
	builders := map[string]func(int) string{
		"line-cap": func(i int) string { return fmt.Sprintf("row %d", i) },
		"byte-cap": func(i int) string { return fmt.Sprintf("%s %d", strings.Repeat("w", 1500), i) },
	}
	counts := map[string]int{"line-cap": 5000, "byte-cap": 300}
	for name, line := range builders {
		t.Run(name, func(t *testing.T) {
			dir := realTempDir(t)
			total := counts[name]
			var content strings.Builder
			for i := 1; i <= total; i++ {
				content.WriteString(line(i) + "\n")
			}
			writeFile(t, filepath.Join(dir, "f.txt"), content.String(), 0600)
			next, requests, seen := 1, 0, 0
			for next <= total {
				requests++
				if requests > total {
					t.Fatalf("continuation loops at offset %d", next)
				}
				result, err := Read(dir, map[string]any{"path": "f.txt", "offset": next, "limit": total})
				if err != nil {
					t.Fatal(err)
				}
				ranges := result.Details["fullyDisplayedRanges"].([][2]int)
				first := regexp.MustCompile(`(?m)^(\d+)#`).FindStringSubmatch(result.Text)
				if first == nil || first[1] != strconv.Itoa(next) {
					t.Fatalf("response does not start at requested offset %d: %q", next, result.Text[:min(40, len(result.Text))])
				}
				last := next
				for _, r := range ranges {
					last = max(last, r[1])
				}
				shown := last - next + 1
				seen += shown
				match := continuationPattern.FindStringSubmatch(result.Text)
				if last == total {
					if match != nil {
						t.Fatalf("complete delivery still advertises continuation: %v", match)
					}
					break
				}
				if match == nil || match[1] != strconv.Itoa(last+1) {
					t.Fatalf("continuation must name the first undisplayed line %d: %v", last+1, match)
				}
				if !strings.Contains(result.Text, "[Showing lines "+strconv.Itoa(next)+"-"+strconv.Itoa(last)+" of "+strconv.Itoa(total)) {
					t.Fatalf("notice misstates the displayed endpoint %d", last)
				}
				next = last + 1
			}
			if seen != total {
				t.Fatalf("covered %d of %d lines", seen, total)
			}
		})
	}
}
