package automodels

import (
	"fmt"
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
	offset int
	theme  sdk.UITheme
}

func (d *dashboard) Render(width int) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if width < 1 {
		return nil
	}
	rule := d.theme.Fg("borderAccent", strings.Repeat("─", width))
	out := []string{rule, d.theme.Fg("accent", d.theme.Bold(fit(d.title, width)))}
	end := min(d.offset+dashboardRows, len(d.lines))
	for _, line := range d.lines[d.offset:end] {
		out = append(out, fit(line, width))
	}
	help := "↑↓/jk scroll • pgup/pgdn page • q close"
	if len(d.lines) > dashboardRows {
		help = fmt.Sprintf("%d-%d/%d • %s", d.offset+1, end, len(d.lines), help)
	}
	return append(out, d.theme.Fg("dim", fit(help, width)), rule)
}

func (d *dashboard) HandleInput(data string) (sdk.RemoteComponentResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	maxOffset := max(len(d.lines)-dashboardRows, 0)
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
