package automodels

import (
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

type choice struct{ value, label, description string }
type picker struct {
	mu         sync.Mutex
	title      string
	items      []choice
	matches    []int
	index      int
	query      string
	searchable bool
	theme      sdk.UITheme
}

func (p *picker) filter() {
	p.matches = p.matches[:0]
	query := strings.ToLower(p.query)
	for i, item := range p.items {
		if fuzzy(item.label, query) {
			p.matches = append(p.matches, i)
		}
	}
	p.index = 0
}
func fuzzy(text, query string) bool {
	next := 0
	for _, r := range text {
		if next == len(query) {
			return true
		}
		wanted, n := utf8.DecodeRuneInString(query[next:])
		if unicode.ToLower(r) == wanted {
			next += n
		}
	}
	return next == len(query)
}
func (p *picker) Render(width int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if width < 1 {
		return nil
	}
	title := p.title
	if p.searchable {
		if p.query == "" {
			title += " (type to search)"
		} else {
			title += " ❯ " + p.query
		}
	}
	lines := []string{p.theme.Fg("borderAccent", strings.Repeat("─", width)), p.theme.Fg("accent", p.theme.Bold(fit(title, width)))}
	start := 0
	if p.index >= 12 {
		start = p.index - 11
	}
	end := min(start+12, len(p.matches))
	for i := start; i < end; i++ {
		item := p.items[p.matches[i]]
		prefix := "  "
		color := "text"
		if i == p.index {
			prefix, color = "> ", "accent"
		}
		text := prefix + item.label
		if item.description != "" {
			text += " — " + item.description
		}
		lines = append(lines, p.theme.Fg(color, fit(text, width)))
	}
	if len(p.matches) == 0 {
		lines = append(lines, p.theme.Fg("warning", "No matching models"))
	}
	if len(p.matches) > 12 {
		lines = append(lines, p.theme.Fg("dim", fmt.Sprintf("%d/%d", p.index+1, len(p.matches))))
	}
	help := "↑↓ select • enter confirm • esc cancel"
	if p.searchable {
		help = "↑↓ select • type to search • enter confirm • esc cancel"
	}
	lines = append(lines, p.theme.Fg("dim", fit(help, width)), p.theme.Fg("borderAccent", strings.Repeat("─", width)))
	return lines
}
func fit(value string, width int) string {
	value = clean(value)
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
func (p *picker) HandleInput(data string) (sdk.RemoteComponentResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch data {
	case "\x1b", "\x03":
		return sdk.RemoteComponentResult{Done: true}, nil
	case "\r", "\n":
		if len(p.matches) > 0 {
			return sdk.RemoteComponentResult{Done: true, Value: p.items[p.matches[p.index]].value}, nil
		}
	case "\x1b[A", "\x1bOA":
		if p.index > 0 {
			p.index--
		}
	case "\x1b[B", "\x1bOB":
		if p.index+1 < len(p.matches) {
			p.index++
		}
	case "\x7f", "\b":
		if p.searchable && p.query != "" {
			_, n := utf8.DecodeLastRuneInString(p.query)
			p.query = p.query[:len(p.query)-n]
			p.filter()
		}
	default:
		if p.searchable && len(p.query)+len(data) <= 256 && utf8.ValidString(data) && clean(data) == data && !strings.ContainsAny(data, "\n\t") {
			p.query += data
			p.filter()
		}
	}
	return sdk.RemoteComponentResult{}, nil
}
func pick(ctx sdk.Context, title string, items []choice, searchable bool) (string, bool, error) {
	if ctx.Mode() == "rpc" {
		labels := make([]string, len(items))
		for i, item := range items {
			labels[i] = item.label
			if item.description != "" {
				labels[i] += " — " + item.description
			}
		}
		selected, ok, err := ctx.Select(title, labels)
		if err != nil || !ok {
			return "", false, err
		}
		for i, label := range labels {
			if selected == label {
				return items[i].value, true, nil
			}
		}
		return "", false, fmt.Errorf("model selection returned an unknown option")
	}
	p := &picker{title: title, items: items, searchable: searchable, theme: ctx.UITheme()}
	p.filter()
	result, err := ctx.Custom(p, sdk.RemoteOverlayOptions{Title: title})
	if err != nil || result == nil {
		return "", false, err
	}
	value, ok := result.(string)
	if !ok {
		return "", false, fmt.Errorf("model selection returned an invalid result")
	}
	for _, item := range items {
		if value == item.value {
			return value, true, nil
		}
	}
	return "", false, fmt.Errorf("model selection returned an unknown value")
}
