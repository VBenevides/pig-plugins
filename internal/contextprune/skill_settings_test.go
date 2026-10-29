package contextprune

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillExpirySettingsValidateAndPreserveTheSwitch(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	if age, err := SkillMaxAge(file); err != nil || age != 0 {
		t.Fatal("expiry must default off")
	}
	if err := SetEnabled(file, false); err != nil {
		t.Fatal(err)
	}
	if err := SetSkillMaxAge(file, 3); err != nil {
		t.Fatal(err)
	}
	if age, err := SkillMaxAge(file); err != nil || age != 3 {
		t.Fatal("expiry not saved")
	}
	if enabled, err := Enabled(file); err != nil || enabled {
		t.Fatal("switch changed")
	}
	for _, data := range []string{`{"skillMaxAge":-1}`, `{"skillMaxAge":1.5}`, `{"skillMaxAge":"3"}`, `{"skillMaxAge":10001}`, `{broken`} {
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := SkillMaxAge(file); err == nil {
			t.Fatalf("invalid expiry accepted: %s", data)
		}
	}
	if err := SetSkillMaxAge(file, -1); err == nil {
		t.Fatal("invalid expiry write accepted")
	}
}
