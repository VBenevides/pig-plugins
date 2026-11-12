package toolsettings

const (
	DefaultPreset = "Default"
	MinimumPreset = "Minimum"
	NativePreset  = "Native"
)

func minimumTool(name string) bool {
	return name == "read" || name == "bash" || name == "edit" || name == "write"
}

// ApplyPreset stages choices in the dialog. Enter saves them; Escape cancels the dialog.
func (m *Menu) ApplyPreset(preset string) {
	if preset != DefaultPreset && preset != MinimumPreset && preset != NativePreset {
		return
	}
	m.ApplyAll = true
	restricted := preset != DefaultPreset
	m.NativeFiles = preset == NativePreset
	m.RestrictTools, m.RestrictSkills = restricted, restricted
	for i := range m.Tabs[0] {
		row := &m.Tabs[0][i]
		row.Enabled = row.DefaultEnabled
		if restricted {
			row.Enabled = minimumTool(row.ID)
		}
		row.Enabled = row.Enabled && m.SelectedVariant(*row)
	}
	for i := range m.Tabs[1] {
		m.Tabs[1][i].Enabled = !restricted
	}
}

func (m *Menu) presetLabel() string {
	full, files := !m.RestrictTools && !m.RestrictSkills, m.RestrictTools && m.RestrictSkills
	for _, r := range m.Tabs[0] {
		if !m.SelectedVariant(r) {
			continue
		}
		full = full && r.Enabled == r.DefaultEnabled
		files = files && r.Enabled == minimumTool(r.ID)
	}
	for _, r := range m.Tabs[1] {
		full = full && r.Enabled
		files = files && !r.Enabled
	}
	full = full && !m.NativeFiles
	if full {
		return DefaultPreset
	}
	if files {
		if m.NativeFiles {
			return NativePreset
		}
		return MinimumPreset
	}
	return "Custom"
}
