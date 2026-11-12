package toolsettings

import "strings"

const (
	DefaultPreset   = "default"
	MinimumPreset   = "minimum"
	NativePreset    = "native"
	BenchmarkPreset = "default-benchmark"
)

func minimumTool(name string) bool {
	return name == "read" || name == "bash" || name == "edit" || name == "write"
}

// ApplyPreset stages choices in the dialog. Enter saves them; Escape cancels the dialog.
func (m *Menu) ApplyPreset(preset string) {
	preset = strings.ToLower(preset)
	if preset != DefaultPreset && preset != MinimumPreset && preset != NativePreset && preset != BenchmarkPreset {
		return
	}
	m.ApplyAll = true
	restricted := preset == MinimumPreset || preset == NativePreset
	m.NativeFiles = preset == NativePreset
	m.RestrictTools, m.RestrictSkills = restricted, restricted
	for i := range m.Tabs[0] {
		row := &m.Tabs[0][i]
		row.Enabled = row.DefaultEnabled
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
	benchmark := !m.RestrictTools && !m.RestrictSkills && !m.NativeFiles
	for _, r := range m.Tabs[0] {
		if !m.SelectedVariant(r) {
			continue
		}
		want := r.DefaultEnabled && r.ID != "ask_user_question"
		benchmark = benchmark && r.Enabled == want
		full = full && r.Enabled == r.DefaultEnabled
		files = files && r.Enabled == minimumTool(r.ID)
	}
	for _, r := range m.Tabs[1] {
		benchmark = benchmark && r.Enabled
		full = full && r.Enabled
		files = files && !r.Enabled
	}
	full = full && !m.NativeFiles
	if full {
		return DefaultPreset
	}
	if benchmark {
		return BenchmarkPreset
	}
	if files {
		if m.NativeFiles {
			return NativePreset
		}
		return MinimumPreset
	}
	return "custom"
}
