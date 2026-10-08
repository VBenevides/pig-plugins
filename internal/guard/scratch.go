package guard

import (
	"os"
	"path/filepath"
	"strings"
)

// Scratch folders hold throwaway files, so a recursive delete that stays inside them needs no confirmation:
// everything below <cwd>/.agent-work, and everything below any folder named tmp inside the working folder.
const (
	agentWorkDir = ".agent-work"
	scratchDir   = "tmp"
)

// inScratch reports whether path lies strictly inside a scratch folder of root. The scratch folder itself is not
// included, so removing .agent-work or tmp as a whole still asks.
func inScratch(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if parts[0] == agentWorkDir {
		return len(parts) > 1
	}
	for _, part := range parts[:len(parts)-1] {
		if part == scratchDir {
			return true
		}
	}
	return false
}

// resolveDeleteTargets turns the words of an rm -rf into absolute paths. resolved is false when a word depends on
// the shell (a variable, a command substitution or an escape) and so does not name a known path.
func resolveDeleteTargets(words []string, cwd string) (paths []string, resolved bool) {
	resolved = true
	for _, word := range words {
		if len(word) >= 2 && (word[0] == '\'' || word[0] == '"') && word[len(word)-1] == word[0] {
			word = word[1 : len(word)-1]
		}
		if word == "" || strings.ContainsAny(word, "$`\\") {
			resolved = false
			paths = append(paths, word)
			continue
		}
		paths = append(paths, resolvePath(word, cwd))
	}
	return paths, resolved
}

// resolvePath makes raw absolute the way a shell would: ~ means the home folder, anything relative starts at cwd.
func resolvePath(raw, cwd string) string {
	switch {
	case filepath.IsAbs(raw):
		return filepath.Clean(raw)
	case raw == "~" || strings.HasPrefix(raw, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return raw // not absolute, so it never counts as scratch
		}
		return filepath.Join(home, strings.TrimPrefix(raw, "~"))
	}
	return filepath.Join(cwd, raw)
}

// allInScratch reports whether every path, and the real path behind it (symlinks followed), lies inside a scratch
// folder of the working folder.
func allInScratch(paths []string, cwd string) bool {
	if len(paths) == 0 {
		return false
	}
	realRoot := RealTarget(cwd)
	if realRoot == "" {
		realRoot = cwd
	}
	for _, path := range paths {
		real := RealTarget(path)
		if !inScratch(path, cwd) || real == "" || !inScratch(real, realRoot) {
			return false
		}
	}
	return true
}

// bashAffected names what a bash command changes. items are the deleted paths when the command names them and the
// working folder otherwise. scratchOnly is true only for a recursive force delete whose every target is known and
// inside a scratch folder; nothing else is ever auto-approved.
func bashAffected(analysis Analysis, cwd string) (items []string, scratchOnly bool) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if len(analysis.DeleteTargets) == 0 {
		return []string{cwd}, false
	}
	paths, resolved := resolveDeleteTargets(analysis.DeleteTargets, cwd)
	onlyDelete := len(analysis.Behaviors) == 1 && analysis.Behaviors[0] == "recursive-force-delete"
	return paths, resolved && onlyDelete && allInScratch(paths, cwd)
}
