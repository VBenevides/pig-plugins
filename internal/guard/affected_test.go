package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAffectedItemDescriptions(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "output.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("build", filepath.Join(cwd, "link")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		command string
		want    string
	}{
		{"rm -rf build output.txt missing", "Affected items:\n- folder: " + filepath.Join(cwd, "build") + " - delete\n- file: " + filepath.Join(cwd, "output.txt") + " - delete\n- path: " + filepath.Join(cwd, "missing") + " - delete (file or folder; type unknown)"},
		{"rm -rf link", "Affected items:\n- symlink: " + filepath.Join(cwd, "link") + " - delete"},
		{"git push --force origin feat/test", "Affected items:\n- branch: feat/test (remote: origin) - push changes (overwrite remote history)"},
		{"git push origin feat/test", "Affected items:\n- branch: feat/test (remote: origin) - push changes"},
		{"git push --force-with-lease origin 'feat/test'", "Affected items:\n- branch: feat/test (remote: origin) - push changes (overwrite remote history)"},
		{"git push origin +feat/test", "Affected items:\n- branch: feat/test (remote: origin) - push changes (overwrite remote history)"},
		{"git push origin local:feat/remote", "Affected items:\n- branch: feat/remote (remote: origin) - push changes"},
		{"git push --delete origin feat/old feat/other", "Affected items:\n- branch: feat/old (remote: origin) - delete remote reference\n- branch: feat/other (remote: origin) - delete remote reference"},
		{"git push origin :feat/old", "Affected items:\n- branch: feat/old (remote: origin) - delete remote reference"},
		{"git push --force origin refs/tags/v1", "Affected items:\n- tag: v1 (remote: origin) - push changes (overwrite remote history)"},
		{"git push --force origin tag v1", "Affected items:\n- tag: v1 (remote: origin) - push changes (overwrite remote history)"},
		{"git branch -D feat/old feat/other", "Affected items:\n- branch: feat/old - delete local branch\n- branch: feat/other - delete local branch"},
		{"git tag -d v1", "Affected items:\n- tag: v1 - delete local tag"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			analysis, err := Analyze(tt.command)
			if err != nil {
				t.Fatal(err)
			}
			items, _ := bashAffected(analysis, cwd)
			if got := describeAffected(tt.command, analysis, items); got != tt.want {
				t.Errorf("description = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAffectedItemsDoNotGuessDynamicOrImplicitTargets(t *testing.T) {
	for _, command := range []string{
		"git push --force", "git push --mirror origin", "git push --force origin $BRANCH",
		"git -C other push --force origin feat/test", "git push --force origin feat/test; rm -rf build",
		"git push --force origin feat/test && sudo reboot", "sudo custom-tool", "rm -rf $TARGET",
		"git push --force --repo other feat/test", "git push --force origin HEAD",
		"git push --force origin HEAD~1", "git push --force origin feat/test # comment",
		"rm -rf 'folder with spaces'", "rm -rf build\ngit push --force origin feat/test",
	} {
		t.Run(command, func(t *testing.T) {
			analysis, err := Analyze(command)
			if err != nil {
				t.Fatal(err)
			}
			items, _ := bashAffected(analysis, "/work")
			got := describeAffected(command, analysis, items)
			if !strings.HasPrefix(got, "Affected items:\n- scope:") || !strings.Contains(got, "targets not determined") {
				t.Errorf("must disclose uncertain targets: %s", got)
			}
		})
	}
}
