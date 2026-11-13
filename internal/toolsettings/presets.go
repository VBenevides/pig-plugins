package toolsettings

import "strings"

const (
	DefaultPreset   = "default"
	MinimalPreset   = "minimal"
	BenchmarkPreset = "default-benchmark"
)

func minimumTool(name string) bool {
	return name == "read" || name == "bash" || name == "edit" || name == "write"
}

// defaultTool permits every non-browser tool. Exposure is controlled separately.
func defaultTool(name string) bool {
	return !strings.HasPrefix(name, "browser_")
}

// ApplyPreset stages choices in the dialog. Enter saves them; Escape cancels the dialog.
func (m *Menu) ApplyPreset(preset string) {
	preset = strings.ToLower(preset)
	if preset != DefaultPreset && preset != MinimalPreset && preset != BenchmarkPreset {
		return
	}
	m.ApplyAll = true
	restricted := preset == MinimalPreset
	m.NativeFiles = true
	m.RestrictTools, m.RestrictSkills = restricted, restricted
	for i := range m.Tabs[0] {
		row := &m.Tabs[0][i]
		row.Enabled = defaultTool(row.ID)
		if restricted {
			row.Enabled = minimumTool(row.ID)
		}
		if preset == BenchmarkPreset && row.ID == "ask_user_question" {
			row.Enabled = false
		}
		row.Enabled = row.Enabled && m.SelectedVariant(*row)
	}
	for i := range m.Tabs[1] {
		m.Tabs[1][i].Enabled = !restricted
	}
}

func (m *Menu) presetLabel() string {
	full, files := !m.RestrictTools && !m.RestrictSkills, m.RestrictTools && m.RestrictSkills
	benchmark := !m.RestrictTools && !m.RestrictSkills && m.NativeFiles
	for _, r := range m.Tabs[0] {
		if !m.SelectedVariant(r) {
			continue
		}
		want := defaultTool(r.ID) && r.ID != "ask_user_question"
		benchmark = benchmark && r.Enabled == want
		full = full && r.Enabled == defaultTool(r.ID)
		files = files && r.Enabled == minimumTool(r.ID)
	}
	for _, r := range m.Tabs[1] {
		benchmark = benchmark && r.Enabled
		full = full && r.Enabled
		files = files && !r.Enabled
	}
	full = full && m.NativeFiles
	if full {
		return DefaultPreset
	}
	if benchmark {
		return BenchmarkPreset
	}
	if files && m.NativeFiles {
		return MinimalPreset
	}
	return "custom"
}
