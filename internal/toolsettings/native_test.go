package toolsettings

import (
	"path/filepath"
	"testing"
)

func TestNativeProfileIsExclusiveAndLaunchOnly(t *testing.T) {
	m := &Menu{}
	m.Handle("p")
	m.Handle("2")
	if !m.NativeFiles || m.presetLabel() != MinimalPreset {
		t.Fatal("native profile not selected")
	}
	m.ApplyPreset(DefaultPreset)
	if !m.NativeFiles {
		t.Fatal("default did not select native tools")
	}
	args, profile, err := ParseProfile([]string{"--profile", "minimal", "hello"})
	if err != nil || profile != "minimal" || len(args) != 1 || args[0] != "hello" {
		t.Fatalf("%v %s %v", args, profile, err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(ProfileEnv, "minimal")
	c, err := LoadEffective(path)
	if err != nil || !c.NativeFiles || c.SkillAllowlist == nil {
		t.Fatalf("native: %+v %v", c, err)
	}
	t.Setenv(ProfileEnv, "minimum")
	c, err = LoadEffective(path)
	if err == nil {
		t.Fatal("removed minimum profile was accepted")
	}
}
