package hashline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	if ImageMIME(head) != "" || isBMP(head) || bytes.IndexByte(content, 0) >= 0 {
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
	// Mode holds ordinary permission bits; privileged modes are rejected.
	Mode          fs.FileMode
	info          fs.FileInfo // identity observed when the snapshot was taken
	securityLabel []byte      // exact inherited label; never printed
}

// TakeSnapshot opens the file for editing: refuses directories and special files, symlinks that leave cwd,
// files larger than maxBytes and binary content.
func TakeSnapshot(absolute, cwd string, maxBytes int64) (Snapshot, error) {
	return TakeSnapshotContext(context.Background(), absolute, cwd, maxBytes)
}

func TakeSnapshotContext(request Cancellation, absolute, cwd string, maxBytes int64) (Snapshot, error) {
	if err := request.Err(); err != nil {
		return Snapshot{}, err
	}
	link, err := os.Lstat(absolute)
	if err != nil {
		return Snapshot{}, editErrorf("Cannot edit %s: %s. The file must exist.", absolute, errnoName(err))
	}
	target, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if link.Mode()&fs.ModeSymlink != 0 {
			return Snapshot{}, editErrorf("Cannot edit %s: broken symlink (%s).", absolute, errnoName(err))
		}
		return Snapshot{}, fmt.Errorf("resolve target %s: %w", absolute, err)
	}
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve working directory %s: %w", cwd, err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return Snapshot{}, fmt.Errorf("compare target %s with working directory %s: %w", target, root, err)
	}
	outside := relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
	if target != absolute && outside {
		return Snapshot{}, editErrorf("Refusing to edit %s: its path traverses a symlink to %s, outside the working directory %s.", absolute, target, root)
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
	if stat.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
		return Snapshot{}, categorizedEditError(CategoryPermission, "Refusing to edit %s: it has privileged permission bits that atomic replacement must not restore.", absolute)
	}
	if stat.Mode().Perm()&0222 == 0 {
		return Snapshot{}, categorizedEditError(CategoryPermission, "Refusing to edit %s: it is read-only; replacement must not bypass its write permissions.", absolute)
	}
	links, err := linkCount(stat)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect edit target %s: %w", absolute, err)
	}
	if links != 1 {
		return Snapshot{}, editErrorf("Refusing to edit %s: it has %d hard links; atomic replacement would break the link relationship.", absolute, links)
	}
	if stat.Size() > maxBytes {
		return Snapshot{}, editErrorf("%s is larger than %d bytes.", absolute, maxBytes)
	}
	label, err := checkSupportedMetadata(target)
	if err != nil {
		return Snapshot{}, err
	}
	content, err := readBoundedFileContext(request, target, maxBytes)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read %s: %w", target, err)
	}
	if IsBinary(content) {
		return Snapshot{}, editErrorf("Refusing to edit %s: it is binary or not valid UTF-8.", absolute)
	}
	if err := request.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Target: target, Bytes: content, Mode: stat.Mode().Perm(), info: stat, securityLabel: label}, nil
}

// ReplaceAtomically replaces the file with content through a temporary file in the same directory and a rename.
// Before the rename the file is read again; if its bytes differ from the snapshot, nothing is replaced and the
// edit is rejected. beforeCommit runs after the temporary file is complete (tests use it to race the edit); a
// change made by another process between the final comparison and the rename cannot be detected.
func ReplaceAtomically(snapshot Snapshot, content string, beforeCommit func()) (err error) {
	return ReplaceAtomicallyContext(context.Background(), snapshot, content, beforeCommit)
}

func ReplaceAtomicallyContext(request Cancellation, snapshot Snapshot, content string, beforeCommit func()) (err error) {
	return replaceAtomicallyWithSync(request, snapshot, content, beforeCommit, syncDirectory)
}

func replaceAtomicallyWithSync(request Cancellation, snapshot Snapshot, content string, beforeCommit func(), syncParent func(string) error) (err error) {
	if err := request.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(snapshot.Target)
	file, err := os.CreateTemp(directory, ".pig-plugins-edit-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", snapshot.Target, err)
	}
	temporary := file.Name()
	committed := false
	defer func() {
		if file != nil {
			if closeErr := file.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close temporary file for %s: %w", snapshot.Target, closeErr))
			}
		}
		if !committed {
			if removeErr := os.Remove(temporary); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temporary file %s: %w", temporary, removeErr))
			}
		}
	}()
	replacementInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect temporary file for %s: %w", snapshot.Target, err)
	}
	if snapshot.info == nil || !sameOwnership(snapshot.info, replacementInfo) {
		return editErrorf("Refusing to replace %s: original ownership cannot be safely preserved; nothing was written.", snapshot.Target)
	}
	label, err := checkSupportedMetadata(temporary)
	if err != nil {
		return err
	}
	if !bytes.Equal(label, snapshot.securityLabel) {
		return editErrorf("Refusing to replace %s: inherited security label does not match the original; nothing was written.", snapshot.Target)
	}
	if err := writePreparedText(request, file, content); err != nil {
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
	if err := request.Err(); err != nil {
		return err
	}
	currentInfo, err := os.Lstat(snapshot.Target)
	if err != nil {
		return categorizedEditError(CategoryConcurrent, "%s changed while it was being edited (%s); nothing was written.", snapshot.Target, errnoName(err))
	}
	if snapshot.info == nil || !currentInfo.Mode().IsRegular() || !os.SameFile(snapshot.info, currentInfo) {
		return categorizedEditError(CategoryConcurrent, "%s changed file identity while the edit was prepared; nothing was written. Read it again.", snapshot.Target)
	}
	links, err := linkCount(currentInfo)
	if err != nil {
		return fmt.Errorf("revalidate link count of %s: %w", snapshot.Target, err)
	}
	if links != 1 || currentInfo.Mode() != snapshot.info.Mode() || !sameOwnership(snapshot.info, currentInfo) {
		return categorizedEditError(CategoryConcurrent, "%s changed security-relevant metadata while the edit was prepared; nothing was written. Read it again.", snapshot.Target)
	}
	currentLabel, err := checkSupportedMetadata(snapshot.Target)
	if err != nil {
		return fmt.Errorf("revalidate metadata of %s: %w", snapshot.Target, err)
	}
	if !bytes.Equal(currentLabel, snapshot.securityLabel) {
		return categorizedEditError(CategoryConcurrent, "%s changed security label while the edit was prepared; nothing was written. Read it again.", snapshot.Target)
	}
	current, err := readBoundedFileContext(request, snapshot.Target, MaxEditBytes)
	if err != nil {
		if cancelled := request.Err(); cancelled != nil {
			return fmt.Errorf("cancelled final read of %s: %w", snapshot.Target, errors.Join(err, cancelled))
		}
		return categorizedEditError(CategoryConcurrent, "%s changed while it was being edited (%s); nothing was written.", snapshot.Target, errnoName(err))
	}
	if !bytes.Equal(current, snapshot.Bytes) {
		return categorizedEditError(CategoryConcurrent, "%s was modified by someone else while the edit was prepared; nothing was written. Read it again.", snapshot.Target)
	}
	if err := request.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporary, snapshot.Target); err != nil {
		return fmt.Errorf("replace %s: %w", snapshot.Target, err)
	}
	committed = true
	if err := syncParent(directory); err != nil {
		return &CommittedError{Path: snapshot.Target, Err: err}
	}
	return nil
}
