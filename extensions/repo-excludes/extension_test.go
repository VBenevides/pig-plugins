package repoexcludes

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func initRepository(t *testing.T, cwd string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "init", "--quiet")
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initialize Git fixture: %v\n%s", err, out)
	}
	return filepath.Join(cwd, ".git", "info", "exclude")
}

func setTrust(t *testing.T, home *pigtest.Home, trusted bool) {
	t.Helper()
	if err := os.MkdirAll(home.AgentDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]bool{home.Work: trusted})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "trust.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	// The host treats projects without local resources as trusted before it reads
	// trust.json. A local settings file makes this fixture exercise saved trust.
	projectConfig := filepath.Join(home.Work, ".pig")
	if err := os.MkdirAll(projectConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectConfig, "settings.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRealRPCStartup(t *testing.T) {
	pigtest.RequirePig(t)
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"trusted", "untrusted", "nonrepository", "error notification"} {
		t.Run(scenario, func(t *testing.T) {
			home := pigtest.NewHome(t)
			setTrust(t, home, scenario != "untrusted")
			var exclude string
			if scenario != "nonrepository" {
				exclude = initRepository(t, home.Work)
				if err := os.WriteFile(exclude, []byte("original-no-newline"), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			outside := filepath.Join(home.Dir, "untouched")
			if scenario == "error notification" {
				if err := os.WriteFile(outside, []byte("outside-sentinel"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(exclude); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, exclude); err != nil {
					t.Fatal(err)
				}
			}
			ignore := filepath.Join(home.Work, ".gitignore")
			if err := os.WriteFile(ignore, []byte("ignore-sentinel\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mock := pigtest.NewMockLLM()
			defer mock.Close()
			for range 2 {
				result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extension}})
				if scenario == "error notification" {
					found := false
					for _, notice := range result.Notices() {
						if strings.Contains(notice, "repo-excludes:") && (strings.Contains(notice, "regular file") ||
							strings.Contains(notice, "exclude path is outside the common info directory")) {
							found = true
						}
					}
					if !found {
						t.Fatalf("startup failure did not notify: %v\nstderr: %s", result.Notices(), result.Stderr)
					}
				} else if len(result.Notices()) != 0 {
					t.Fatalf("unexpected startup notice: %v", result.Notices())
				}
			}
			if len(mock.Requests()) != 0 {
				t.Fatal("startup contacted the LLM")
			}
			if scenario == "nonrepository" {
				if _, err := os.Stat(filepath.Join(home.Work, ".git")); !os.IsNotExist(err) {
					t.Fatalf("nonrepository created Git metadata: %v", err)
				}
			} else {
				data, err := os.ReadFile(exclude)
				if err != nil {
					t.Fatal(err)
				}
				want := "original-no-newline"
				if scenario == "trusted" {
					want += "\n.agent-work/\n.ouro/\n.curator/\n"
				} else if scenario == "error notification" {
					want = "outside-sentinel"
				}
				if string(data) != want {
					t.Fatalf("startup exclude = %q, want %q", data, want)
				}
			}
			data, err := os.ReadFile(ignore)
			if err != nil || string(data) != "ignore-sentinel\n" {
				t.Fatalf("startup changed .gitignore: %q, %v", data, err)
			}
		})
	}
}
