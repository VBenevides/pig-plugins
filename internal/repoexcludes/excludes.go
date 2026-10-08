// Package repoexcludes adds local agent directories to Git's private exclude file.
package repoexcludes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxExcludeBytes = 1 << 20
const operationTimeout = 5 * time.Second

var patterns = [...]string{".agent-work/", ".ouro/", ".curator/"}

// Ensure adds only missing exact entries. A directory outside Git is a quiet no-op.
// Callers must check project trust before this function starts Git.
func Ensure(cwd string) (changed bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	common, exclude, err := resolve(ctx, cwd)
	if err != nil || common == "" {
		return false, err
	}
	root, err := os.OpenRoot(common)
	if err != nil {
		return false, fmt.Errorf("open Git directory: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	if filepath.Clean(exclude) != filepath.Join(filepath.Clean(common), "info", "exclude") {
		return false, fmt.Errorf("Git exclude path is outside the common info directory: %s", exclude)
	}
	if err := root.Mkdir("info", 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return false, fmt.Errorf("create Git info directory: %w", err)
	}
	before, err := root.Lstat("info")
	if err != nil || !before.IsDir() {
		return false, fmt.Errorf("Git info must be a directory, not a symbolic link: %w", errors.Join(err, fs.ErrInvalid))
	}
	info, err := root.OpenRoot("info")
	if err != nil {
		return false, fmt.Errorf("open Git info directory: %w", err)
	}
	defer func() { err = errors.Join(err, info.Close()) }()
	dir, err := info.Open(".")
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	after, err := dir.Stat()
	if err != nil || !os.SameFile(before, after) {
		return false, fmt.Errorf("Git info directory changed: %w", errors.Join(err, fs.ErrInvalid))
	}
	return appendLocked(ctx, info, dir)
}

// Git does not run hooks or aliases for this built-in command. No shell is used.
func resolve(ctx context.Context, cwd string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--path-format=absolute", "--git-common-dir", "--git-path", "info/exclude")
	cmd.Dir = cwd
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, stderr := &limitedBuffer{}, &limitedBuffer{}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 128 && (strings.HasPrefix(stderr.String(), "fatal: not a git repository (or any of the parent directories): .git") ||
			strings.HasPrefix(stderr.String(), "fatal: not a git repository (or any parent up to mount point ")) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("resolve Git exclude path: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 || lines[0] == "" || lines[1] == "" {
		return "", "", errors.New("Git returned invalid directory paths")
	}
	for i := range lines {
		if !filepath.IsAbs(lines[i]) {
			lines[i] = filepath.Join(cwd, lines[i])
		}
		absolute, err := filepath.Abs(lines[i])
		if err != nil {
			return "", "", err
		}
		lines[i] = absolute
	}
	return lines[0], lines[1], nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return 0, errors.New("Git output exceeds 64 KiB")
	}
	return b.Buffer.Write(p)
}

// The exclusive lock file remains in place until rename commits the complete file.
// This uses Git's own lock-file protocol, rather than locking the replaced inode.
func appendLocked(ctx context.Context, root *os.Root, dir *os.File) (changed bool, err error) {
	var lock *os.File
	for {
		lock, err = root.OpenFile("exclude.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return false, fmt.Errorf("lock Git exclude file: %w", err)
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("wait for Git exclude lock: %w", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	closed, committed := false, false
	defer func() {
		if !closed {
			err = errors.Join(err, lock.Close())
		}
		if !committed {
			err = errors.Join(err, root.Remove("exclude.lock"))
		}
	}()
	data, mode, original, err := readExclude(root)
	if err != nil {
		return false, err
	}
	updated := appendMissing(data)
	if len(updated) == len(data) {
		return false, nil
	}
	if len(updated) > maxExcludeBytes {
		return false, errors.New("Git exclude file would exceed 1 MiB")
	}
	if _, err := lock.Write(updated); err != nil {
		return false, fmt.Errorf("write Git exclude lock: %w", err)
	}
	if err := lock.Chmod(mode); err != nil {
		return false, fmt.Errorf("set Git exclude permissions: %w", err)
	}
	if err := lock.Sync(); err != nil {
		return false, fmt.Errorf("sync Git exclude lock: %w", err)
	}
	closed = true
	if err := lock.Close(); err != nil {
		return false, fmt.Errorf("close Git exclude lock: %w", err)
	}
	current, statErr := root.Lstat("exclude")
	if original == nil {
		if !errors.Is(statErr, fs.ErrNotExist) {
			return false, errors.New("Git exclude file appeared during update")
		}
	} else if statErr != nil || !current.Mode().IsRegular() || !os.SameFile(original, current) {
		return false, fmt.Errorf("Git exclude file changed during update: %w", errors.Join(statErr, fs.ErrInvalid))
	}
	if err := root.Rename("exclude.lock", "exclude"); err != nil {
		return false, fmt.Errorf("replace Git exclude file: %w", err)
	}
	committed = true
	if err := dir.Sync(); err != nil {
		return false, fmt.Errorf("sync Git info directory after update: %w", err)
	}
	return true, nil
}

func readExclude(root *os.Root) (data []byte, mode fs.FileMode, original fs.FileInfo, err error) {
	original, err = root.Lstat("exclude")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0o600, nil, nil
	}
	if err != nil {
		return nil, 0, nil, fmt.Errorf("inspect Git exclude file: %w", err)
	}
	if !original.Mode().IsRegular() || original.Size() > maxExcludeBytes {
		return nil, 0, nil, errors.New("Git exclude file must be a regular file of at most 1 MiB")
	}
	file, err := openExclude(root)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("open Git exclude file: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(original, opened) {
		return nil, 0, nil, fmt.Errorf("Git exclude file changed before read: %w", errors.Join(err, fs.ErrInvalid))
	}
	data, err = io.ReadAll(io.LimitReader(file, maxExcludeBytes+1))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("read Git exclude file: %w", err)
	}
	if len(data) > maxExcludeBytes {
		return nil, 0, nil, errors.New("Git exclude file exceeds 1 MiB")
	}
	return data, original.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky), original, nil
}

func appendMissing(data []byte) []byte {
	var present [len(patterns)]bool
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		for i, pattern := range patterns {
			if bytes.Equal(line, []byte(pattern)) {
				present[i] = true
			}
		}
	}
	for i, pattern := range patterns {
		if present[i] {
			continue
		}
		if len(data) > 0 && data[len(data)-1] != '\n' {
			data = append(data, '\n')
		}
		data = append(data, pattern...)
		data = append(data, '\n')
	}
	return data
}
