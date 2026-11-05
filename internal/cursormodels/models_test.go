package cursormodels

import (
	"reflect"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

func ids(entries []Entry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

func TestCollapsesSeparateIdentifiers(t *testing.T) {
	var catalog []cursorbridge.Model
	for _, id := range []string{"grok-4.6-low", "grok-4.6-medium", "grok-4.6-high", "grok-4.6-low-fast", "grok-4.6-high-fast", "composer-2"} {
		catalog = append(catalog, cursorbridge.Model{ID: id})
	}
	entries, err := Collapse(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids(entries), []string{"composer-2", "grok-4.6", "grok-4.6-fast"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids %v, want %v", got, want)
	}
	selection, err := Resolve(entries, "grok-4.6-fast", "high")
	if err != nil || selection.ID != "grok-4.6-high-fast" {
		t.Fatalf("fast/high resolved to %v, %v", selection, err)
	}
	if selection, _ = Resolve(entries, "grok-4.6", "low"); selection.ID != "grok-4.6-low" {
		t.Fatal(selection)
	}
	// The fast family has no medium; it must fail, not pick a neighbour.
	if _, err = Resolve(entries, "grok-4.6-fast", "medium"); err == nil || !strings.Contains(err.Error(), "low, high") {
		t.Fatalf("unsupported level accepted: %v", err)
	}
	if _, err = Resolve(entries, "grok-4.6", ""); err == nil {
		t.Fatal("reasoning model resolved without a level")
	}
	if selection, err = Resolve(entries, "composer-2", ""); err != nil || selection.ID != "composer-2" {
		t.Fatal(selection, err)
	}
	if _, err = Resolve(entries, "composer-2", "high"); err == nil {
		t.Fatal("model without thinking accepted a level")
	}
}

func TestLoneSuffixAndHyphenatedNamesStayExact(t *testing.T) {
	entries, err := Collapse([]cursorbridge.Model{{ID: "gpt-5-high"}, {ID: "claude-4-sonnet"}, {ID: "o3-mini-high"}, {ID: "o3-mini-low"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids(entries), []string{"claude-4-sonnet", "gpt-5-high", "o3-mini"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids %v, want %v", got, want)
	}
}

func TestCollapsedNameNeverHidesExactEntry(t *testing.T) {
	entries, err := Collapse([]cursorbridge.Model{{ID: "m"}, {ID: "m-low"}, {ID: "m-high"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(entries); !reflect.DeepEqual(got, []string{"m", "m-high", "m-low"}) {
		t.Fatalf("exact entry shadowed: %v", got)
	}
}

func param(id string, values ...string) cursorbridge.ParameterDefinition {
	d := cursorbridge.ParameterDefinition{ID: id}
	for _, v := range values {
		d.Values = append(d.Values, cursorbridge.Parameter{ID: id, Value: v})
	}
	return d
}

func TestParameterizedModelWithFastAndEffort(t *testing.T) {
	model := cursorbridge.Model{ID: "grok-4.6", DisplayName: "Grok 4.6", Parameters: []cursorbridge.ParameterDefinition{param("effort", "low", "medium", "high"), param("fast", "false", "true")}}
	for _, effort := range []string{"low", "medium", "high"} {
		for _, fast := range []string{"false", "true"} {
			if effort == "medium" && fast == "true" {
				continue // not listed by the catalog
			}
			model.Variants = append(model.Variants, cursorbridge.Variant{IsDefault: effort == "medium" && fast == "false", Params: []cursorbridge.Parameter{{ID: "effort", Value: effort}, {ID: "fast", Value: fast}}})
		}
	}
	entries, err := Collapse([]cursorbridge.Model{model})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(entries); !reflect.DeepEqual(got, []string{"grok-4.6", "grok-4.6-fast"}) {
		t.Fatalf("ids %v", got)
	}
	selection, err := Resolve(entries, "grok-4.6-fast", "high")
	want := cursorbridge.Selection{ID: "grok-4.6", Params: []cursorbridge.Parameter{{ID: "effort", Value: "high"}, {ID: "fast", Value: "true"}}}
	if err != nil || !reflect.DeepEqual(selection, want) {
		t.Fatalf("selection %+v, %v", selection, err)
	}
	if _, err = Resolve(entries, "grok-4.6-fast", "medium"); err == nil {
		t.Fatal("unlisted combination accepted")
	}
	if selection, _ = Resolve(entries, "grok-4.6", "medium"); selection.Params[1].Value != "false" {
		t.Fatal(selection)
	}
}

func TestUnknownEffortValuesAndMissingModels(t *testing.T) {
	entries, err := Collapse([]cursorbridge.Model{{ID: "x", Parameters: []cursorbridge.ParameterDefinition{param("effort", "turbo", "low")}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := entries[0].Levels(); !reflect.DeepEqual(got, []string{"low"}) {
		t.Fatalf("levels %v", got)
	}
	if _, err = Resolve(entries, "missing", "low"); err == nil {
		t.Fatal("missing model resolved")
	}
	if entries, err = Collapse(nil); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestDuplicateCatalogEntriesFail(t *testing.T) {
	if _, err := Collapse([]cursorbridge.Model{{ID: "a"}, {ID: "a"}}); err == nil {
		t.Fatal("duplicate catalog accepted")
	}
}
