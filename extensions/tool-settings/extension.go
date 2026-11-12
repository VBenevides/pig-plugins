// Package toolsettingsext controls global tool and skill discovery and invocation.
package toolsettingsext

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
	"github.com/VBenevides/pig-plugins/internal/tui"
)

const Name = "tool-settings"

type component struct{ menu *toolsettings.Menu }

func (c *component) Render(width int) []string { return c.menu.Render(width) }
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
		cfg, err := toolsettings.Load(toolsettings.Path())
		if err != nil {
			return map[string]any{"block": true, "reason": err.Error()}, nil
		}
		name, _ := data["toolName"].(string)
		if cfg.DisabledTools[name] {
			return map[string]any{"block": true, "reason": "tool-settings: tool disabled globally: " + name}, nil
		}
		return nil, nil
	})
	e.RegisterCommand(Name, sdk.CommandOptions{Description: "Choose global tool and skill visibility (Space toggles; R resets)", Handler: func(ctx sdk.Context, _ string) error {
		mu.Lock()
		snapshot := maps.Clone(loaded)
		mu.Unlock()
		return configure(ctx, snapshot)
	}})
	return e
}

func apply(ctx sdk.Context) error {
	cfg, err := toolsettings.Load(toolsettings.Path())
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
	for _, tool := range tools {
		if cfg.EnabledTools[tool.Name] && !slices.Contains(active, tool.Name) {
			active = append(active, tool.Name)
		}
	}
	enabled := slices.DeleteFunc(slices.Clone(active), func(name string) bool { return cfg.DisabledTools[name] })
	ctx.SetActiveTools(enabled)
	return nil
}

func configure(ctx sdk.Context, loaded map[string]int) error {
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
	menu := &toolsettings.Menu{LoadedKnown: loaded != nil}
	for _, tool := range tools {
		schema := struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		}{tool.Name, tool.Description, tool.Parameters}
		raw, err := json.Marshal(schema)
		if err != nil {
			return fmt.Errorf("count tool %s: %w", tool.Name, err)
		}
		visible := !slices.Contains(options.HiddenTools, tool.Name)
		source := tool.SourceInfo.Path
		if source == "" {
			source = tool.SourceInfo.Origin
		}
		if source == "" {
			source = tool.SourceInfo.Source
		}
		if source == "" {
			source = "builtin"
		}
		if _, ok := cfg.Defaults[tool.Name]; !ok {
			cfg.Defaults[tool.Name] = slices.Contains(active, tool.Name)
		}
		menu.Tabs[0] = append(menu.Tabs[0], toolsettings.Row{Name: tool.Name, ID: tool.Name, Source: source, Context: string(raw), Enabled: !cfg.DisabledTools[tool.Name] && slices.Contains(active, tool.Name), Visible: visible})
	}
	for _, s := range cfg.Skills {
		// Discovery metadata, not the full skill file. XML escaping matches the prompt's metadata representation.
		text := fmt.Sprintf("<skill>\n<name>%s</name>\n<description>%s</description>\n<location>%s</location>\n</skill>", escape(s.Name), escape(s.Description), escape(s.Path))
		menu.Tabs[1] = append(menu.Tabs[1], toolsettings.Row{Name: s.Name, ID: s.Path, Source: "skill", Context: text, Enabled: !cfg.DisabledSkills[s.Path], Visible: !s.Hidden, LoadedChars: loaded[s.Path]})
	}
	result, err := ctx.Custom(&component{menu}, nil)
	if err != nil {
		return err
	}
	save, _ := result.(bool)
	if !save {
		return nil
	}
	if menu.Reset {
		cfg.Reset()
	} else {
		for _, r := range menu.Tabs[0] {
			if r.Enabled {
				delete(cfg.DisabledTools, r.ID)
				cfg.EnabledTools[r.ID] = true
			} else {
				cfg.DisabledTools[r.ID] = true
				delete(cfg.EnabledTools, r.ID)
			}
		}
		for _, r := range menu.Tabs[1] {
			if r.Enabled {
				delete(cfg.DisabledSkills, r.ID)
			} else {
				cfg.DisabledSkills[r.ID] = true
			}
		}
	}
	if err := toolsettings.SaveIfUnchanged(toolsettings.Path(), previous, cfg); err != nil {
		return err
	}
	// Reload rebuilds future tool declarations and the skill command catalog. It does not remove old messages.
	return ctx.Reload()
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}
