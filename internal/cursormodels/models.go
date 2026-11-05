// Package cursormodels collapses Cursor catalog variants into PiG model entries.
//
// A catalog may describe variants in two ways: as one model with parameters
// (effort, fast), or as separate identifiers such as grok-4.6-low-fast. Both are
// presented as "grok-4.6" and "grok-4.6-fast", with the effort chosen through
// PiG's thinking selector. The exact Cursor identifier or parameters are kept
// internally; nothing is ever substituted silently.
package cursormodels

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

// Levels are PiG thinking levels in ascending order. Only these names can be
// mapped from Cursor values; unknown Cursor efforts stay unavailable.
var Levels = []string{"minimal", "low", "medium", "high", "xhigh"}

var suffix = regexp.MustCompile(`^(.+?)-(minimal|low|medium|high|xhigh)(-fast)?$`)

// Entry is one selectable PiG model and the exact Cursor selections behind it.
type Entry struct {
	ID        string
	Name      string
	Fast      bool
	Reasoning bool
	// Selections maps a PiG thinking level to an exact Cursor selection. The empty
	// key is the catalog default, used only when the model has no thinking levels.
	Selections map[string]cursorbridge.Selection
}

// Levels returns the supported PiG levels in ascending order.
func (e Entry) Levels() []string {
	var out []string
	for _, level := range Levels {
		if _, ok := e.Selections[level]; ok {
			out = append(out, level)
		}
	}
	return out
}

// Collapse builds entries from a catalog. Ordering is stable. Duplicate PiG
// identifiers are an error: the catalog is ambiguous and must not be guessed.
func Collapse(models []cursorbridge.Model) ([]Entry, error) {
	byID := map[string]*Entry{}
	var order []string
	add := func(entry Entry) error {
		if _, exists := byID[entry.ID]; exists {
			return fmt.Errorf("Cursor catalog has ambiguous model %q", entry.ID)
		}
		copy := entry
		byID[entry.ID] = &copy
		order = append(order, entry.ID)
		return nil
	}
	exact := map[string]bool{}
	for _, model := range models {
		exact[model.ID] = true
	}
	// Separate-identifier variants group by base name and speed.
	type group struct{ levels map[string]string }
	groups := map[string]*group{}
	var groupOrder []string
	grouped := map[string]bool{}
	for _, model := range models {
		if len(model.Parameters) > 0 || len(model.Variants) > 1 {
			continue
		}
		match := suffix.FindStringSubmatch(model.ID)
		if match == nil {
			continue
		}
		key := match[1] + match[3]
		g := groups[key]
		if g == nil {
			g = &group{levels: map[string]string{}}
			groups[key] = g
			groupOrder = append(groupOrder, key)
		}
		if previous, duplicate := g.levels[match[2]]; duplicate {
			return nil, fmt.Errorf("Cursor catalog has duplicate %s variants %q and %q", match[2], previous, model.ID)
		}
		g.levels[match[2]] = model.ID
	}
	for _, key := range groupOrder {
		g := groups[key]
		// One lone suffixed identifier is a plain model, not a collapsed family.
		// A collapsed name must not hide an exact catalog entry.
		if len(g.levels) < 2 || exact[key] {
			continue
		}
		entry := Entry{ID: key, Name: displayName(key), Fast: strings.HasSuffix(key, "-fast"), Reasoning: true, Selections: map[string]cursorbridge.Selection{}}
		for level, id := range g.levels {
			entry.Selections[level] = cursorbridge.Selection{ID: id}
			grouped[id] = true
		}
		if err := add(entry); err != nil {
			return nil, err
		}
	}
	for _, model := range models {
		if grouped[model.ID] {
			continue
		}
		entries, err := fromParameters(model)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err := add(entry); err != nil {
				return nil, err
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i] < order[j] })
	result := make([]Entry, 0, len(order))
	for _, id := range order {
		result = append(result, *byID[id])
	}
	return result, nil
}

func displayName(id string) string { return strings.ReplaceAll(id, "-", " ") }

func isEffort(parameter cursorbridge.ParameterDefinition) bool {
	id := strings.ToLower(parameter.ID)
	return strings.Contains(id, "effort") || strings.Contains(id, "reasoning") || strings.Contains(id, "thinking")
}
func isFast(parameter cursorbridge.ParameterDefinition) bool {
	return strings.Contains(strings.ToLower(parameter.ID), "fast")
}
func truthy(value string) bool {
	switch strings.ToLower(value) {
	case "true", "on", "yes", "fast", "1":
		return true
	}
	return false
}

// fromParameters turns one parameterized model into at most two entries
// (normal and fast). Unknown parameters are kept constant from the default
// variant; a combination that the catalog does not list stays unavailable.
func fromParameters(model cursorbridge.Model) ([]Entry, error) {
	name := model.DisplayName
	if name == "" {
		name = displayName(model.ID)
	}
	var effort, fast *cursorbridge.ParameterDefinition
	for i := range model.Parameters {
		switch {
		case isEffort(model.Parameters[i]) && effort == nil:
			effort = &model.Parameters[i]
		case isFast(model.Parameters[i]) && fast == nil:
			fast = &model.Parameters[i]
		}
	}
	if effort == nil && fast == nil {
		return []Entry{{ID: model.ID, Name: name, Selections: map[string]cursorbridge.Selection{"": {ID: model.ID}}}}, nil
	}
	defaults := map[string]string{}
	for _, variant := range model.Variants {
		if variant.IsDefault {
			for _, p := range variant.Params {
				defaults[p.ID] = p.Value
			}
		}
	}
	listed := func(params map[string]string) bool {
		if len(model.Variants) == 0 {
			return true
		}
	next:
		for _, variant := range model.Variants {
			for _, p := range variant.Params {
				if value, ok := params[p.ID]; ok && value != p.Value {
					continue next
				}
			}
			return true
		}
		return false
	}
	speeds := []string{""}
	if fast != nil {
		speeds = nil
		seen := map[bool]bool{}
		for _, value := range fast.Values {
			if !seen[truthy(value.Value)] {
				seen[truthy(value.Value)] = true
				speeds = append(speeds, value.Value)
			}
		}
		sort.SliceStable(speeds, func(i, j int) bool { return !truthy(speeds[i]) && truthy(speeds[j]) })
	}
	var out []Entry
	for _, speed := range speeds {
		isFastEntry := speed != "" && truthy(speed)
		id := model.ID
		entryName := name
		if isFastEntry {
			id += "-fast"
			entryName += " (fast)"
		}
		entry := Entry{ID: id, Name: entryName, Fast: isFastEntry, Selections: map[string]cursorbridge.Selection{}}
		base := map[string]string{}
		for k, v := range defaults {
			base[k] = v
		}
		if fast != nil {
			base[fast.ID] = speed
		}
		build := func(level, value string) {
			params := map[string]string{}
			for k, v := range base {
				params[k] = v
			}
			if effort != nil {
				params[effort.ID] = value
			}
			if !listed(params) {
				return
			}
			selection := cursorbridge.Selection{ID: model.ID}
			keys := make([]string, 0, len(params))
			for k := range params {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				selection.Params = append(selection.Params, cursorbridge.Parameter{ID: k, Value: params[k]})
			}
			entry.Selections[level] = selection
		}
		if effort == nil {
			build("", "")
		} else {
			entry.Reasoning = true
			for _, value := range effort.Values {
				if level := strings.ToLower(value.Value); contains(Levels, level) {
					build(level, value.Value)
				}
			}
		}
		if len(entry.Selections) > 0 {
			out = append(out, entry)
		}
	}
	return out, nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// Resolve returns the exact Cursor selection for a PiG model and thinking level.
// An empty level asks for no reasoning; it is valid only for models without
// thinking levels. Unsupported levels are explicit errors, never clamped.
func Resolve(entries []Entry, id, level string) (cursorbridge.Selection, error) {
	for _, entry := range entries {
		if entry.ID != id {
			continue
		}
		if selection, ok := entry.Selections[level]; ok {
			return selection, nil
		}
		if level == "" {
			return cursorbridge.Selection{}, fmt.Errorf("Cursor model %q requires a thinking level: %s", id, strings.Join(entry.Levels(), ", "))
		}
		if len(entry.Levels()) == 0 {
			return cursorbridge.Selection{}, fmt.Errorf("Cursor model %q has no thinking levels; select off", id)
		}
		return cursorbridge.Selection{}, fmt.Errorf("Cursor model %q does not support thinking level %q; available: %s", id, level, strings.Join(entry.Levels(), ", "))
	}
	return cursorbridge.Selection{}, errors.New("Cursor model " + id + " is not in the selected account's catalog")
}
