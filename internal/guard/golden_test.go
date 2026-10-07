package guard

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// golden is the output of the original TypeScript policy on the same inputs, captured by
// .agent-work/scripts/smart-approve-golden/generate.mjs. The port must reproduce it exactly.
type golden struct {
	Analyze []struct {
		Command     string   `json:"command"`
		Behaviors   []string `json:"behaviors"`
		Labels      []string `json:"labels"`
		HardBlocked bool     `json:"hardBlocked"`
		DenyTier    bool     `json:"denyTier"`
	} `json:"analyze"`
	DeleteTargets []struct {
		Raw  string `json:"raw"`
		Kind string `json:"kind"`
	} `json:"deleteTargets"`
	Normalize []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"normalize"`
	Paths []struct {
		Path      string `json:"path"`
		Protected bool   `json:"protected"`
	} `json:"paths"`
	DefaultPatterns []string `json:"defaultPatterns"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	data, err := os.ReadFile("testdata/ts-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAnalyzeMatchesTypeScript(t *testing.T) {
	g := loadGolden(t)
	if len(g.Analyze) < 300 {
		t.Fatalf("golden has only %d commands", len(g.Analyze))
	}
	for _, c := range g.Analyze {
		got, err := Analyze(c.Command)
		if err != nil {
			t.Errorf("%q: %v", c.Command, err)
			continue
		}
		if !slices.Equal(got.Behaviors, c.Behaviors) && !(len(got.Behaviors) == 0 && len(c.Behaviors) == 0) {
			t.Errorf("%q behaviors: got %v, TypeScript %v", c.Command, got.Behaviors, c.Behaviors)
		}
		if !slices.Equal(got.Labels, c.Labels) && !(len(got.Labels) == 0 && len(c.Labels) == 0) {
			t.Errorf("%q labels: got %v, TypeScript %v", c.Command, got.Labels, c.Labels)
		}
		if got.HardBlocked != c.HardBlocked || got.DenyTier != c.DenyTier {
			t.Errorf("%q: hard/deny got %v/%v, TypeScript %v/%v", c.Command, got.HardBlocked, got.DenyTier, c.HardBlocked, c.DenyTier)
		}
	}
}

func TestNormalizeMatchesTypeScript(t *testing.T) {
	for _, c := range loadGolden(t).Normalize {
		if got := Normalize(c.Input); got != c.Output {
			t.Errorf("Normalize(%q) = %q, TypeScript %q", c.Input, got, c.Output)
		}
	}
}

func TestDeleteTargetsMatchTypeScript(t *testing.T) {
	for _, c := range loadGolden(t).DeleteTargets {
		got, err := NormalizeDeleteTarget(c.Raw)
		if err != nil {
			t.Errorf("%q: %v", c.Raw, err)
			continue
		}
		if got != c.Kind {
			t.Errorf("NormalizeDeleteTarget(%q) = %s, TypeScript %s", c.Raw, got, c.Kind)
		}
	}
}

func TestDefaultPatternsMatchTypeScript(t *testing.T) {
	if got := loadGolden(t).DefaultPatterns; !slices.Equal(got, DefaultProtectedPaths) {
		t.Fatalf("default patterns differ:\n go %q\n ts %q", DefaultProtectedPaths, got)
	}
}

func TestProtectedPathsMatchTypeScript(t *testing.T) {
	matcher := DefaultPathMatcher()
	for _, c := range loadGolden(t).Paths {
		if got := matcher.IsProtected(c.Path); got != c.Protected {
			t.Errorf("IsProtected(%q) = %v, TypeScript %v", c.Path, got, c.Protected)
		}
	}
}
