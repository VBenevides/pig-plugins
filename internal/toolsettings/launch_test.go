package toolsettings

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseProfile(t *testing.T) {
	for _, tt := range []struct {
		args, out []string
		profile   string
		bad       bool
	}{
		{[]string{"--profile", "minimal", "hello"}, []string{"hello"}, "minimal", false},
		{[]string{"--profile=default", "--model", "x"}, []string{"--model", "x"}, "default", false},
		{[]string{"--profile", "default-benchmark"}, []string{}, "default-benchmark", false},
		{[]string{"--profile=DeFaUlT-BeNcHmArK"}, []string{}, "default-benchmark", false},
		{[]string{"--profile", "MINIMAL"}, []string{}, "minimal", false},
		{[]string{"--profile", "minimum"}, nil, "", true},
		{[]string{"--profile", "native"}, nil, "", true},
		{[]string{"--", "--profile", "minimum"}, []string{"--", "--profile", "minimum"}, "", false},
		{[]string{"--profile"}, nil, "", true},
		{[]string{"--profile=bare"}, nil, "", true},
		{[]string{"--profile=minimum", "--profile=default"}, nil, "", true},
	} {
		out, profile, err := ParseProfile(tt.args)
		if (err != nil) != tt.bad || profile != tt.profile || !reflect.DeepEqual(out, tt.out) {
			t.Fatalf("%v: %v %s %v", tt.args, out, profile, err)
		}
	}
}

func TestLaunchProfileDoesNotChangeSavedChoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	saved, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	saved.DisabledTools["read"] = true
	saved.ToolAllowlist = []string{"edit"}
	saved.SkillAllowlist = []string{}
	if err := Save(path, saved); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(ProfileEnv, "minimal")
	c, err := LoadEffective(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"read", "bash", "edit", "write"} {
		if !c.ToolEnabled(name) {
			t.Fatal(name)
		}
	}
	if c.ToolEnabled("extra") || c.SkillEnabled("/skill/SKILL.md") {
		t.Fatal("minimum restriction missing")
	}
	t.Setenv(ProfileEnv, "default")
	c, err = LoadEffective(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ToolEnabled("read") || c.SkillAllowlist != nil {
		t.Fatal("default did not override saved restrictions")
	}
	t.Setenv(ProfileEnv, "DEFAULT-BENCHMARK")
	c, err = LoadEffective(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.ToolEnabled("ask_user_question") || !c.ToolEnabled("read") || !c.ToolEnabled("extra") || !c.SkillEnabled("/skill/SKILL.md") || !c.NativeFiles {
		t.Fatal("benchmark did not apply default permissions without questions")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("launch changed saved choices")
	}
	t.Setenv(ProfileEnv, "")
	c, err = LoadEffective(path)
	if err != nil || c.ToolEnabled("read") {
		t.Fatalf("saved choices not restored: %v", err)
	}
}
