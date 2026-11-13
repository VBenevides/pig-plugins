package toolsettingsext

import "github.com/VBenevides/pig-plugins/internal/toolsettings"

// A launch tool profile is an overlay. Saving a prompt style must not save its temporary permissions.
func settingsToSave(menu *toolsettings.Menu, previous, effective toolsettings.Config) toolsettings.Config {
	if menu.ProfileOverride != "" {
		effective = previous
	}
	menu.ApplyTo(&effective)
	return effective
}
