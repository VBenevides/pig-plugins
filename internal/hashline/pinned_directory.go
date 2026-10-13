package hashline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// A verified procfs descriptor path keeps every child operation relative to the
// opened directory even if its original pathname is renamed or replaced.
// Mutation already fails closed outside Linux for unsupported metadata.
type pinnedDirectory struct {
	file               *os.File
	physical, original string
	info               fs.FileInfo
}

func pinDirectory(path string) (directory *pinnedDirectory, err error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("directory-relative mutation is unsupported on %s", runtime.GOOS)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|nonBlockingReadFlag|nonFollowingReadFlag, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, file.Close())
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("mutation parent is not a directory")
	}
	physical := fmt.Sprintf("/proc/self/fd/%d", file.Fd())
	actual, err := os.Stat(physical)
	if err != nil {
		return nil, fmt.Errorf("directory-relative mutation requires accessible procfs descriptors: %w", err)
	}
	if !os.SameFile(info, actual) {
		return nil, fmt.Errorf("procfs descriptor identity mismatch")
	}
	directory = &pinnedDirectory{file: file, physical: physical, original: path, info: info}
	if err = directory.validate(); err != nil {
		return nil, err
	}
	return directory, nil
}
func (directory *pinnedDirectory) validate() error {
	real, err := filepath.EvalSymlinks(directory.original)
	if err != nil {
		return fmt.Errorf("revalidate mutation directory: %w", err)
	}
	info, err := os.Lstat(directory.original)
	if err != nil {
		return fmt.Errorf("revalidate mutation directory: %w", err)
	}
	if real != directory.original || !info.IsDir() || !os.SameFile(directory.info, info) {
		return categorizedEditError(CategoryConcurrent, "Mutation directory %s changed identity or symlink resolution; nothing was published.", directory.original)
	}
	return nil
}
func (directory *pinnedDirectory) close(primary *error, target string, published *bool) {
	if err := directory.file.Close(); err != nil {
		err = fmt.Errorf("close mutation directory for %s: %w", target, err)
		if published != nil && *published {
			err = &CommittedError{Path: target, Err: err}
		}
		*primary = errors.Join(*primary, err)
	}
}
func (directory *pinnedDirectory) child(target string) string {
	return filepath.Join(directory.physical, filepath.Base(target))
}
