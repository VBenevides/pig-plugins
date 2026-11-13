package toolsettings

import "slices"

// MarkInitial records each tool's checkbox as shown when the dialog opened. Call it once rows are final.
func (m *Menu) MarkInitial() {
	for i := range m.Tabs[0] {
		m.Tabs[0][i].Initial = m.Tabs[0][i].Enabled
	}
}

// ApplyTo saves permission choices without changing exposure. Untouched rows keep their saved state.
// Explicit presets apply every listed choice, even when its checkbox did not change.
func (m *Menu) ApplyTo(cfg *Config) {
	if m.PromptStyle != "" {
		cfg.PromptStyle = m.PromptStyle
	}
	if m.ProfileOverride != "" {
		return
	}
	if m.Reset {
		cfg.Reset()
		return
	}
	cfg.NativeFiles = m.NativeFiles
	previousTools := cfg.ToolAllowlist
	previousSkills := cfg.SkillAllowlist
	cfg.ToolAllowlist, cfg.SkillAllowlist = nil, nil
	if m.RestrictTools {
		cfg.ToolAllowlist = []string{}
	}
	if m.RestrictSkills {
		cfg.SkillAllowlist = []string{}
	}
	for _, r := range m.Tabs[0] {
		if !m.SelectedVariant(r) {
			continue
		}
		if r.Enabled && m.RestrictTools {
			cfg.ToolAllowlist = append(cfg.ToolAllowlist, r.ID)
		}
		if r.Enabled == r.Initial && !m.ApplyAll {
			continue
		}
		if r.Enabled {
			delete(cfg.DisabledTools, r.ID)
			cfg.EnabledTools[r.ID] = true
		} else {
			cfg.DisabledTools[r.ID] = true
			delete(cfg.EnabledTools, r.ID)
		}
	}
	if m.RestrictSkills {
		cfg.SkillAllowlist = append(cfg.SkillAllowlist, m.KeepSkills...)
	}
	for _, r := range m.Tabs[1] {
		if r.Enabled {
			if m.RestrictSkills {
				cfg.SkillAllowlist = append(cfg.SkillAllowlist, r.ID)
			}
			delete(cfg.DisabledSkills, r.ID)
		} else {
			cfg.DisabledSkills[r.ID] = true
		}
	}
	if !m.ApplyAll {
		preserveOmitted := func(previous []string, rows []Row, current *[]string) {
			listed := map[string]bool{}
			for _, r := range rows {
				listed[r.ID] = true
			}
			for _, id := range previous {
				if !listed[id] && !slices.Contains(*current, id) {
					*current = append(*current, id)
				}
			}
		}
		if m.RestrictTools {
			preserveOmitted(previousTools, m.Tabs[0], &cfg.ToolAllowlist)
		}
		if m.RestrictSkills {
			preserveOmitted(previousSkills, m.Tabs[1], &cfg.SkillAllowlist)
		}
	}
}
