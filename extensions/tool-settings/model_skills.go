package toolsettingsext

import (
	"fmt"

	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func addModelSkills(menu *toolsettings.Menu, cfg toolsettings.Config, loaded map[string]int) {
	for _, skill := range cfg.Skills {
		if skill.Hidden {
			menu.OtherLoadedChars += loaded[skill.Path]
			if cfg.SkillEnabled(skill.Path) {
				menu.KeepSkills = append(menu.KeepSkills, skill.Path)
			}
			continue
		}
		text := fmt.Sprintf("<skill>\n<name>%s</name>\n<description>%s</description>\n<location>%s</location>\n</skill>", escape(skill.Name), escape(skill.Description), escape(skill.Path))
		menu.Tabs[1] = append(menu.Tabs[1], toolsettings.Row{Name: skill.Name, ID: skill.Path, Source: "skill", Context: text, Enabled: cfg.SkillEnabled(skill.Path), Visible: true, LoadedChars: loaded[skill.Path]})
	}
}
