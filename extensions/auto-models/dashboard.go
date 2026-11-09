package automodels

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/tui"
)

const dashboardRows = 24

// dashboard is a read-only scrollable overlay closed with q, esc or ctrl-c.
type dashboard struct {
	mu     sync.Mutex
	title  string
	lines  []string
	view   []string // lines as laid out for the last rendered width
	offset int
	theme  sdk.UITheme
}

const (
	accountMarker  = "── Account"
	columnGap      = " │ "
	minColumnWidth = 62 // a quota row with its bar and reset time
)

// layout shows the account blocks side by side when the terminal is wide
// enough. Lines before the first account stay full width. Each block goes to
// the shorter column, so the order stays close to the list order.
func layout(lines []string, width int) []string {
	colWidth := (width - cellWidth(columnGap)) / 2
	if colWidth < minColumnWidth {
		return lines
	}
	first := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, accountMarker) })
	if first < 0 {
		return lines
	}
	var blocks [][]string
	for _, line := range lines[first:] {
		if strings.HasPrefix(line, accountMarker) {
			blocks = append(blocks, nil)
		}
		blocks[len(blocks)-1] = append(blocks[len(blocks)-1], line)
	}
	if len(blocks) < 2 {
		return lines
	}
	var left, right []string
	for _, block := range blocks {
		for len(block) > 0 && strings.TrimSpace(block[len(block)-1]) == "" {
			block = block[:len(block)-1]
		}
		target := &left
		if len(right) < len(left) {
			target = &right
		}
		if len(*target) > 0 {
			*target = append(*target, "")
		}
		*target = append(*target, block...)
	}
	out := slices.Clone(lines[:first])
	for i := range max(len(left), len(right)) {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		l = fitCells(l, colWidth)
		out = append(out, l+strings.Repeat(" ", colWidth-cellWidth(l))+columnGap+fitCells(r, colWidth))
	}
	return out
}

// cellWidth counts terminal columns. The emoji used in the usage text take two.
func cellWidth(text string) int {
	n := 0
	for _, r := range text {
		n += runeCells(r)
	}
	return n
}

func runeCells(r rune) int {
	switch {
	case r == 0xfe0f || r == 0x200d:
		return 0
	case r >= 0x1f300 && r <= 0x1faff,
		r >= 0x231a && r <= 0x231b, r >= 0x23e9 && r <= 0x23f3,
		r == 0x2705, r == 0x274c, r >= 0x2753 && r <= 0x2755:
		return 2
	}
	return 1
}

// fitCells cuts text to width columns and marks the cut with an ellipsis.
func fitCells(text string, width int) string {
	if cellWidth(text) <= width {
		return text
	}
	var out strings.Builder
	used := 0
	for _, r := range text {
		n := runeCells(r)
		if used+n > width-1 {
			break
		}
		out.WriteRune(r)
		used += n
	}
	return out.String() + "…"
}

func (d *dashboard) Render(width int) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if width < 1 {
		return nil
	}
	rule := d.theme.Fg("borderAccent", strings.Repeat("─", width))
	out := []string{rule, d.theme.Fg("accent", d.theme.Bold(fit(d.title, width)))}
	d.view = layout(d.lines, width)
	d.offset = min(d.offset, max(len(d.view)-dashboardRows, 0))
	end := min(d.offset+dashboardRows, len(d.view))
	for _, line := range d.view[d.offset:end] {
		out = append(out, fit(line, width))
	}
	help := "↑↓/jk scroll • pgup/pgdn page • q close"
	if len(d.view) > dashboardRows {
		help = fmt.Sprintf("%d-%d/%d • %s", d.offset+1, end, len(d.view), help)
	}
	return append(out, d.theme.Fg("dim", fit(help, width)), rule)
}

func (d *dashboard) HandleInput(data string) (sdk.RemoteComponentResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	total := len(d.view)
	if d.view == nil {
		total = len(d.lines)
	}
	maxOffset := max(total-dashboardRows, 0)
	if tui.Released(data) {
		return sdk.RemoteComponentResult{}, nil
	}
	key := tui.Key(data)
	if data == "G" || key == "shift+g" {
		d.offset = maxOffset
		return sdk.RemoteComponentResult{}, nil
	}
	switch key {
	case "q", "shift+q", "escape", "ctrl+c":
		return sdk.RemoteComponentResult{Done: true}, nil
	case "up", "k":
		d.offset = max(d.offset-1, 0)
	case "down", "j":
		d.offset = min(d.offset+1, maxOffset)
	case "pageup":
		d.offset = max(d.offset-dashboardRows, 0)
	case "pagedown", "space":
		d.offset = min(d.offset+dashboardRows, maxOffset)
	case "g":
		d.offset = 0
	}
	return sdk.RemoteComponentResult{}, nil
}
