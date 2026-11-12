package toolsettings

// SelectedVariant identifies the one implementation saved under each shared tool name.
func (m *Menu) SelectedVariant(r Row) bool {
	return r.Variant == "" || (r.Variant == "native") == m.NativeFiles
}

// SwitchFileVariant transfers each file tool's enable state to its alternative. File implementations switch
// as a group because Hashline edit requires Hashline read authority.
func (m *Menu) SwitchFileVariant(native bool) {
	enabled := map[string]bool{}
	for _, r := range m.Tabs[0] {
		if r.Variant != "" && m.SelectedVariant(r) {
			enabled[r.ID] = r.Enabled
		}
	}
	m.NativeFiles = native
	for i := range m.Tabs[0] {
		r := &m.Tabs[0][i]
		if r.Variant != "" {
			r.Enabled = m.SelectedVariant(*r) && enabled[r.ID]
		}
	}
}

// ExpandFileVariants preserves active metadata and exposes the inactive implementation as a selectable row.
// The host provides only active schemas. Do not invent counts for unavailable schemas.
func (m *Menu) ExpandFileVariants() {
	var rows []Row
	for _, r := range m.Tabs[0] {
		if r.ID != "read" && r.ID != "edit" && r.ID != "write" {
			rows = append(rows, r)
			continue
		}
		active := r
		active.Variant = "hashline"
		if m.NativeFiles {
			active.Variant = "native"
		}
		active.Name = r.ID + " (" + active.Variant + ")"
		alternative := r
		alternative.Variant = "native"
		if m.NativeFiles {
			alternative.Variant = "hashline"
		}
		alternative.Name = r.ID + " (" + alternative.Variant + ")"
		alternative.Enabled = false
		alternative.Context = ""
		alternative.Source = "alternative; schema available after save and reload"
		alternative.Visible = false
		if m.NativeFiles {
			rows = append(rows, alternative, active)
		} else {
			rows = append(rows, active, alternative)
		}
	}
	m.Tabs[0] = rows
}
