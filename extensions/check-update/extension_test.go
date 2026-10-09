package checkupdate

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/updates"
)

func TestVersionBannerAndPersistentPreference(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Text("unused"))
	defer mock.Close()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	options := pigtest.RPCOptions{
		Extensions: []string{path},
		Env:        map[string]string{"PI_OFFLINE": "1", "PIG_PLUGINS_HOST_VERSION": "0.4.1+1.0.3"},
		Prompts:    []string{"/check-update off", "/check-update invalid"},
	}
	result := home.RunRPC(t, mock, options)
	want := []string{"Startup update checks: off", "usage: /check-update on|off"}
	if !slices.Equal(result.Notices(), want) {
		t.Fatalf("notices = %q; stderr: %s", result.Notices(), result.Stderr)
	}
	preference := filepath.Join(home.AgentDir(), "check-update.json")
	if enabled, err := updates.Enabled(preference); err != nil || enabled {
		t.Fatalf("saved preference = %v, %v", enabled, err)
	}
	// Without offline mode, the persisted off preference must still skip all network checks.
	delete(options.Env, "PI_OFFLINE")
	options.Prompts = []string{"/check-update on"}
	result = home.RunRPC(t, mock, options)
	if !slices.Equal(result.Notices(), []string{"Startup update checks: on"}) {
		t.Fatalf("restart notices = %q", result.Notices())
	}
	if enabled, err := updates.Enabled(preference); err != nil || !enabled {
		t.Fatalf("saved preference = %v, %v", enabled, err)
	}
	info, err := os.Stat(preference)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("preference permissions = %v", info.Mode())
	}
}
