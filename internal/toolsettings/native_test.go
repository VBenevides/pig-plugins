package toolsettings

import (
	"path/filepath"
	"testing"
)

func TestNativeProfileIsExclusiveAndLaunchOnly(t *testing.T) {
	m := &Menu{}
	m.Handle("p")
	m.Handle("3")
	if !m.NativeFiles || m.presetLabel() != NativePreset {
		t.Fatal("native profile not selected")
	}
	m.ApplyPreset(MinimumPreset)
	if m.NativeFiles {
		t.Fatal("minimum retained native tools")
	}
	m.ApplyPreset(DefaultPreset)
	if !m.NativeFiles {
		t.Fatal("default did not select native tools")
	}
	args, profile, err := ParseProfile([]string{"--profile", "native", "hello"})
	if err != nil || profile != "native" || len(args) != 1 || args[0] != "hello" {
		t.Fatalf("%v %s %v", args, profile, err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(ProfileEnv, "native")
	c, err := LoadEffective(path)
	if err != nil || !c.NativeFiles || c.SkillAllowlist == nil {
		t.Fatalf("native: %+v %v", c, err)
	}
	t.Setenv(ProfileEnv, "minimum")
	c, err = LoadEffective(path)
	if err != nil || c.NativeFiles {
		t.Fatalf("minimum: %+v %v", c, err)
	}
}
