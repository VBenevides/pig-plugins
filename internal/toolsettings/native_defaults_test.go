package toolsettings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeFileDefaultAndExplicitHashlinePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, payload := range []string{"", `{}`, `{"disabledTools":{}}`} {
		if payload != "" {
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
		}
		cfg, err := Load(path)
		if err != nil || !cfg.NativeFiles {
			t.Fatalf("payload=%s cfg=%+v err=%v", payload, cfg, err)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.NativeFiles = false
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	for restart := 0; restart < 2; restart++ {
		saved, err := Load(path)
		if err != nil || saved.NativeFiles {
			t.Fatalf("explicit hashline lost: %+v %v", saved, err)
		}
	}
	cfg.Reset()
	if !cfg.NativeFiles {
		t.Fatal("reset did not restore native tools")
	}
}
