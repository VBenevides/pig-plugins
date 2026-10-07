package hashline

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// ResolveToolPath resolves a tool path like Pi's file tools: strip a leading `@`, expand `~`, resolve against cwd.
func ResolveToolPath(raw, cwd string) string {
	value := strings.TrimPrefix(raw, "@")
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	switch {
	case value == "~":
		value = home
	case strings.HasPrefix(value, "~/"):
		value = filepath.Join(home, value[2:])
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(cwd, value)
}

// ImageMIME returns the MIME type of PNG, JPEG, GIF and WebP content, or "".
func ImageMIME(head []byte) string {
	switch {
	case bytes.HasPrefix(head, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case len(head) >= 3 && head[0] == 0xff && head[1] == 0xd8 && head[2] == 0xff:
		return "image/jpeg"
	case bytes.HasPrefix(head, []byte("GIF8")):
		return "image/gif"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func isBMP(head []byte) bool {
	if len(head) <= 14 || head[0] != 0x42 || head[1] != 0x4d {
		return false
	}
	pixelOffset := uint32(head[10]) | uint32(head[11])<<8 | uint32(head[12])<<16 | uint32(head[13])<<24
	return int64(pixelOffset) < int64(len(head))+4096
}

// IsBinary is true for images, content with NUL bytes and anything that is not valid UTF-8.
func IsBinary(content []byte) bool {
	head := content[:min(len(content), 8192)]
	if ImageMIME(head) != "" || isBMP(head) || bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	return !utf8.Valid(content)
}

// errnoName is the symbolic name Node prints for an error, e.g. ENOENT.
func errnoName(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		for _, known := range []struct {
			errno syscall.Errno
			name  string
		}{
			{syscall.ENOENT, "ENOENT"}, {syscall.EACCES, "EACCES"}, {syscall.EPERM, "EPERM"}, {syscall.ENOTDIR, "ENOTDIR"},
			{syscall.EISDIR, "EISDIR"}, {syscall.ELOOP, "ELOOP"}, {syscall.ENAMETOOLONG, "ENAMETOOLONG"}, {syscall.EIO, "EIO"},
		} {
			if errno == known.errno {
				return known.name
			}
		}
	}
	return err.Error()
}

// Snapshot is the exact content an edit is validated against.
type Snapshot struct {
	// Target is the real path of the file that will be replaced (symlinks resolved).
	Target string
	Bytes  []byte
	// Mode holds the permission, setuid, setgid and sticky bits.
	Mode fs.FileMode
}

// TakeSnapshot opens the file for editing: refuses directories and special files, symlinks that leave cwd,
// files larger than maxBytes and binary content.
func TakeSnapshot(absolute, cwd string, maxBytes int64) (Snapshot, error) {
	link, err := os.Lstat(absolute)
	if err != nil {
		return Snapshot{}, editErrorf("Cannot edit %s: %s. The file must exist.", absolute, errnoName(err))
	}
	target := absolute
	if link.Mode()&fs.ModeSymlink != 0 {
		target, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return Snapshot{}, editErrorf("Cannot edit %s: broken symlink (%s).", absolute, errnoName(err))
		}
		root, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			return Snapshot{}, fmt.Errorf("resolve working directory %s: %w", cwd, err)
		}
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return Snapshot{}, editErrorf("Refusing to edit %s: it is a symlink to %s, outside the working directory %s.", absolute, target, root)
		}
	}
	stat, err := os.Stat(target)
	if err != nil {
		return Snapshot{}, fmt.Errorf("stat %s: %w", target, err)
	}
	if stat.IsDir() {
		return Snapshot{}, editErrorf("Refusing to edit %s: it is a directory.", absolute)
	}
	if !stat.Mode().IsRegular() {
		return Snapshot{}, editErrorf("Refusing to edit %s: it is not a regular file.", absolute)
	}
	if stat.Size() > maxBytes {
		return Snapshot{}, editErrorf("%s is larger than %d bytes.", absolute, maxBytes)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read %s: %w", target, err)
	}
	if IsBinary(content) {
		return Snapshot{}, editErrorf("Refusing to edit %s: it is binary or not valid UTF-8.", absolute)
	}
	return Snapshot{Target: target, Bytes: content, Mode: stat.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)}, nil
}

// ReplaceAtomically replaces the file with content through a temporary file in the same directory and a rename.
// Before the rename the file is read again; if its bytes differ from the snapshot, nothing is replaced and the
// edit is rejected. beforeCommit runs after the temporary file is complete (tests use it to race the edit); a
// change made by another process between the final comparison and the rename cannot be detected.
func ReplaceAtomically(snapshot Snapshot, content string, beforeCommit func()) (err error) {
	directory, base := filepath.Split(snapshot.Target)
	temporary := filepath.Join(directory, fmt.Sprintf(".%s.pig-plugins-%d-%s.tmp", base, os.Getpid(), strconv.FormatInt(time.Now().UnixMilli(), 36)))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, snapshot.Mode&fs.ModePerm)
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", snapshot.Target, err)
	}
	committed := false
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if !committed {
			if removeErr := os.Remove(temporary); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temporary file %s: %w", temporary, removeErr))
			}
		}
	}()
	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("write temporary file for %s: %w", snapshot.Target, err)
	}
	if err := file.Chmod(snapshot.Mode); err != nil {
		return fmt.Errorf("set mode of temporary file for %s: %w", snapshot.Target, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %s: %w", snapshot.Target, err)
	}
	closing := file
	file = nil
	if err := closing.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", snapshot.Target, err)
	}
	if beforeCommit != nil {
		beforeCommit()
	}
	current, err := os.ReadFile(snapshot.Target)
	if err != nil {
		return editErrorf("%s changed while it was being edited (%s); nothing was written.", snapshot.Target, errnoName(err))
	}
	if !bytes.Equal(current, snapshot.Bytes) {
		return editErrorf("%s was modified by someone else while the edit was prepared; nothing was written. Read it again.", snapshot.Target)
	}
	if err := os.Rename(temporary, snapshot.Target); err != nil {
		return fmt.Errorf("replace %s: %w", snapshot.Target, err)
	}
	committed = true
	return nil
}
