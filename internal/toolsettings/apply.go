package toolsettings

// MarkInitial records each tool's checkbox as shown when the dialog opened. Call it once rows are final.
func (m *Menu) MarkInitial() {
	for i := range m.Tabs[0] {
		m.Tabs[0][i].Initial = m.Tabs[0][i].Enabled
	}
}

// ApplyTo writes the dialog's choices into cfg. A tool is "off" in the dialog when it is merely inactive
// this session (for example deferred), so only tools whose checkbox changed are written to the enabled or
// disabled sets; untouched tools keep their saved state. Allowlists still list every enabled tool.
func (m *Menu) ApplyTo(cfg *Config) {
	if m.Reset {
		cfg.Reset()
		return
	}
	cfg.NativeFiles = m.NativeFiles
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
		if r.Enabled == r.Initial {
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
}
