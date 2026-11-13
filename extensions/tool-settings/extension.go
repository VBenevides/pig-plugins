// Package toolsettingsext controls global tool and skill discovery and invocation.
package toolsettingsext

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
	"github.com/VBenevides/pig-plugins/internal/tui"
)

const Name = "tool-settings"

type component struct {
	menu  *toolsettings.Menu
	theme sdk.UITheme
}

func (c *component) Render(width int) []string {
	lines := c.menu.Render(width)
	// The box is capped in width; center it in wider terminals. Frame glyphs are one cell each.
	margin := ""
	if len(lines) > 0 {
		margin = strings.Repeat(" ", max(0, (width-utf8.RuneCountInString(lines[0]))/2))
	}
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "│ ›"):
			lines[i] = c.theme.Bg("selectedBg", c.theme.Fg("accent", line))
		case i == 1 || i == 2:
			lines[i] = c.theme.Bold(c.theme.Fg("accent", line))
		case strings.HasPrefix(line, "╭"), strings.HasPrefix(line, "├"), strings.HasPrefix(line, "╰"):
			lines[i] = c.theme.Fg("muted", line)
		}
	}
	for i := range lines {
		lines[i] = margin + lines[i]
	}
	return lines
}
func (c *component) HandleInput(data string) (sdk.RemoteComponentResult, error) {
	done, save := c.menu.Handle(tui.Key(data))
	return sdk.RemoteComponentResult{Done: done, Value: save}, nil
}

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	var mu sync.Mutex
	var loaded map[string]int
	e.OnEvent(sdk.EventContext, func(_ sdk.Context, data map[string]any) (any, error) {
		messages, _ := data["messages"].([]any)
		counts := toolsettings.LoadedSkills(messages)
		mu.Lock()
		loaded = counts
		mu.Unlock()
		return nil, nil
	})
	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		mu.Lock()
		loaded = nil
		mu.Unlock()
		return nil, apply(ctx)
	})
	e.OnEvent(sdk.EventBeforeAgentStart, func(ctx sdk.Context, _ map[string]any) (any, error) { return nil, apply(ctx) })
	e.OnEvent(sdk.EventToolCall, func(_ sdk.Context, data map[string]any) (any, error) {
		cfg, err := toolsettings.LoadEffective(toolsettings.Path())
		if err != nil {
			return map[string]any{"block": true, "reason": err.Error()}, nil
		}
		name, _ := data["toolName"].(string)
		if !cfg.ToolEnabled(name) {
			return map[string]any{"block": true, "reason": "tool-settings: tool disabled globally: " + name}, nil
		}
		return nil, nil
	})
	e.RegisterCommand(Name, sdk.CommandOptions{Description: "Choose global tools and skills", Handler: func(ctx sdk.Context, _ string) error {
		mu.Lock()
		snapshot := maps.Clone(loaded)
		mu.Unlock()
		return configure(ctx, snapshot)
	}})
	return e
}

func apply(ctx sdk.Context) error {
	cfg, err := toolsettings.LoadEffective(toolsettings.Path())
	if err != nil {
		return err
	}
	active, err := ctx.GetActiveTools()
	if err != nil {
		return err
	}
	tools, err := ctx.GetAllTools()
	if err != nil {
		return err
	}
	// Start each user request with only the core declarations. Permissions remain broad;
	// deferred tools become active only after discovery and remain active for that request.
	for _, tool := range tools {
		if coreTool(tool.Name) && tool.Name != "tool_search" && cfg.ToolEnabled(tool.Name) && !slices.Contains(active, tool.Name) {
			active = append(active, tool.Name)
		}
	}
	enabled := slices.DeleteFunc(slices.Clone(active), func(name string) bool {
		return !cfg.ToolEnabled(name) || !coreTool(name)
	})
	ctx.SetActiveTools(enabled)
	if !slices.Contains(enabled, "tool_search") {
		ctx.Notify("Tool discovery is unavailable. Only read, write, edit, and bash remain exposed.", "warning")
	}
	return nil
}

func configure(ctx sdk.Context, loaded map[string]int) error {
	profile := os.Getenv(toolsettings.ProfileEnv)
	if !ctx.HasUI() {
		return fmt.Errorf("tool-settings requires an interactive UI")
	}
	cfg, err := toolsettings.Load(toolsettings.Path())
	if err != nil {
		return err
	}
	previous := cfg
	previous.DisabledTools = maps.Clone(cfg.DisabledTools)
	previous.DisabledSkills = maps.Clone(cfg.DisabledSkills)
	previous.EnabledTools = maps.Clone(cfg.EnabledTools)
	previous.Defaults = maps.Clone(cfg.Defaults)
	if profile != "" {
		cfg, err = toolsettings.LoadEffective(toolsettings.Path())
		if err != nil {
			return err
		}
	}
	tools, err := ctx.GetAllTools()
	if err != nil {
		return err
	}
	active, err := ctx.GetActiveTools()
	if err != nil {
		return err
	}
	options, err := ctx.GetSystemPromptOptions()
	if err != nil {
		return err
	}
	known := map[string]toolsettings.Skill{}
	for _, s := range cfg.Skills {
		known[s.Path] = s
	}
	for _, s := range options.Skills {
		path := toolsettings.Canonical(s.FilePath)
		known[path] = toolsettings.Skill{Name: s.Name, Description: s.Description, Path: path, Hidden: s.DisableModelInvocation}
	}
	cfg.Skills = nil
	for _, s := range known {
		cfg.Skills = append(cfg.Skills, s)
	}
	for path := range loaded {
		if _, ok := known[path]; !ok {
			cfg.Skills = append(cfg.Skills, toolsettings.Skill{Name: filepath.Base(filepath.Dir(path)), Path: path, Hidden: true})
		}
	}
	sort.Slice(cfg.Skills, func(i, j int) bool { return cfg.Skills[i].Name < cfg.Skills[j].Name })
	menu := &toolsettings.Menu{LoadedKnown: loaded != nil, RestrictTools: cfg.ToolAllowlist != nil, RestrictSkills: cfg.SkillAllowlist != nil, NativeFiles: cfg.NativeFiles, ProfileOverride: profile}
	registered := map[string]bool{}
	for _, tool := range tools {
		registered[tool.Name] = true
	}
	for _, tool := range tools {
		if _, ok := cfg.Defaults[tool.Name]; !ok {
			cfg.Defaults[tool.Name] = slices.Contains(active, tool.Name) || tool.Exposure == sdk.ToolExposureDeferred || tool.Exposure == sdk.ToolExposureCodemode
		}
		row, include, err := modelToolRow(tool, cfg, active, options.HiddenTools, registered)
		if err != nil {
			return err
		}
		if include {
			menu.Tabs[0] = append(menu.Tabs[0], row)
		}
	}
	menu.ExpandFileVariants()
	menu.CollapseGroups("")
	menu.MarkInitial()
	addModelSkills(menu, cfg, loaded)
	result, err := ctx.Custom(&component{menu: menu, theme: ctx.UITheme()}, nil)
	if err != nil {
		return err
	}
	save, _ := result.(bool)
	if !save {
		return nil
	}
	cfg = settingsToSave(menu, previous, cfg)
	if err := toolsettings.SaveIfUnchanged(toolsettings.Path(), previous, cfg); err != nil {
		return err
	}
	// Reload rebuilds future tool declarations and the skill command catalog. It does not remove old messages.
	return ctx.Reload()
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}
