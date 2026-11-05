package betterfooter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gitTimeout bounds every git subprocess.
const gitTimeout = 4 * time.Second

// maxGitOutput caps what one git command may print.
const maxGitOutput = 16 << 20

// GitChanges are the working tree's line counts against HEAD, untracked files included.
type GitChanges struct {
	Added, Removed int
	Dirty          bool
}

var errGitOutputTooLarge = errors.New("git output too large")

// ErrPassiveGitCountsUnavailable means conversion would require executable helpers.
var ErrPassiveGitCountsUnavailable = errors.New("git: passive change counts unavailable with configured conversion filters")

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxGitOutput {
		return 0, errGitOutputTooLarge
	}
	return b.Buffer.Write(p)
}

// git runs one fixed git command without a shell. code is the exit status; ran is false when git did not run to
// completion (missing, timed out, killed, output too large). The error retains the
// exit status or cancellation cause, but never includes raw Git stderr.
func git(ctx context.Context, dir string, env []string, args ...string) (stdout string, code int, ran bool, runErr error) {
	return gitWithInput(ctx, dir, env, "", args...)
}

func gitWithInput(ctx context.Context, dir string, env []string, input string, args ...string) (stdout string, code int, ran bool, runErr error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	// Passive inspection must never invoke a configured fsmonitor executable.
	args = append([]string{"-c", "core.fsmonitor=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"GIT_OPTIONAL_LOCKS=0"}, env...)...)
	cmd.Stdin = strings.NewReader(input)
	var out cappedBuffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0, true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil && exit.ExitCode() >= 0 {
		return out.String(), exit.ExitCode(), true, err
	}
	return "", -1, false, errors.Join(err, ctx.Err())
}

// ReadGitBranch returns the checked-out branch of the repository containing dir, "detached" for a detached HEAD,
// or "" when dir is not in a repository (or git cannot run).
func ReadGitBranch(ctx context.Context, dir string) string {
	out, code, ran, _ := git(ctx, dir, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	switch {
	case !ran:
		return ""
	case code == 0:
		return strings.TrimSpace(out)
	case code == 1:
		return "detached"
	}
	return ""
}

// ParseGitNumstat sums the added and removed lines of `git diff --numstat`; binary files ("-") count as zero.
func ParseGitNumstat(output string) (added, removed int) {
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		a, errA := strconv.Atoi(fields[0])
		r, errR := strconv.Atoi(fields[1])
		if (errA != nil && fields[0] != "-") || (errR != nil && fields[1] != "-") {
			continue
		}
		added += a
		removed += r
	}
	return added, removed
}

// ReadGitChanges counts changes against HEAD through a fresh throwaway index.
// Never copy cached stat data: its new timestamp can hide same-size racy edits.
// Files with executable conversion filters are unsupported for passive counts.
// Other errors let callers retain previous counts while reporting the failure.
func ReadGitChanges(ctx context.Context, dir string) (changes GitChanges, ok bool, err error) {
	repo, code, ran, _ := git(ctx, dir, nil, "rev-parse", "--show-toplevel")
	if !ran || code != 0 {
		return GitChanges{}, false, nil // not a repository, or git is unavailable
	}
	root := strings.TrimSpace(repo)
	if root == "" {
		return GitChanges{}, false, nil
	}
	// Configuration alone does not mean that a filter applies to this repository.
	filters, filterCode, ran, runErr := git(ctx, root, nil, "config", "--name-only", "--get-regexp", `^filter\..*\.(clean|process)$`)
	if !ran || (filterCode != 0 && filterCode != 1) {
		return GitChanges{}, false, fmt.Errorf("git: could not inspect conversion filter configuration: %w", runErr)
	}
	_, headCode, ran, runErr := git(ctx, root, nil, "rev-parse", "--verify", "--quiet", "HEAD")
	if !ran {
		return GitChanges{}, false, fmt.Errorf("git: could not resolve HEAD: %w", runErr)
	}
	hasHead := headCode == 0

	tmp, err := os.MkdirTemp("", "pig-better-footer-")
	if err != nil {
		return GitChanges{}, false, fmt.Errorf("git: temporary index: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(tmp); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("git: remove temporary index: %w", removeErr))
		}
	}()
	index := filepath.Join(tmp, "index")
	env := []string{"GIT_INDEX_FILE=" + index}

	seed := []string{"read-tree", "--empty"}
	if hasHead {
		seed = []string{"read-tree", "HEAD"}
	}
	if _, code, ran, err := git(ctx, root, env, seed...); !ran || code != 0 {
		return GitChanges{}, false, fmt.Errorf("git: could not seed the temporary index: %w", err)
	}
	if strings.TrimSpace(filters) != "" {
		// Inspect tracked and non-ignored untracked paths without converting content.
		paths, code, ran, runErr := git(ctx, root, env, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		if !ran || code != 0 {
			return GitChanges{}, false, fmt.Errorf("git: could not list paths for conversion filters: %w", runErr)
		}
		attrs, code, ran, runErr := gitWithInput(ctx, root, env, paths, "check-attr", "-z", "--stdin", "filter")
		if !ran || code != 0 {
			return GitChanges{}, false, fmt.Errorf("git: could not inspect conversion filter attributes: %w", runErr)
		}
		drivers := make(map[string]bool)
		for key := range strings.SplitSeq(strings.TrimSpace(filters), "\n") {
			name := strings.TrimPrefix(key, "filter.")
			name = name[:strings.LastIndexByte(name, '.')]
			drivers[name] = true
		}
		fields := strings.Split(attrs, "\x00")
		for i := 2; i < len(fields); i += 3 {
			if drivers[fields[i]] {
				return GitChanges{}, false, ErrPassiveGitCountsUnavailable
			}
		}
	}
	if _, code, ran, err := git(ctx, root, env, "add", "--intent-to-add", "--all", "--", "."); !ran || code != 0 {
		return GitChanges{}, false, fmt.Errorf("git: could not add untracked files to the temporary index: %w", err)
	}
	diff := []string{"diff", "--numstat", "--no-renames", "--no-ext-diff", "--no-textconv"}
	if hasHead {
		diff = append(diff, "HEAD")
	}
	out, code, ran, runErr := git(ctx, root, env, append(diff, "--")...)
	if !ran || code != 0 {
		return GitChanges{}, false, fmt.Errorf("git: diff failed: %w", runErr)
	}
	added, removed := ParseGitNumstat(out)
	return GitChanges{Added: added, Removed: removed, Dirty: strings.TrimSpace(out) != ""}, true, nil
}

// ReadProjectVersion reads the version of the regular file <dir>/package.json only
// (no parent or plugin-version fallback). It returns "v1.2.3" for a semantic version
// and "" for a missing manifest or anything that is not one. Non-regular manifests
// and manifests that cannot be read or parsed return an error and an empty version.
func ReadProjectVersion(dir string) (string, error) {
	data, err := readCapped(filepath.Join(dir, "package.json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ParseProjectVersion(data)
}
