package betterfooter

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var reopenRegularFile = syscall.NewLazyDLL("kernel32.dll").NewProc("ReOpenFile")

// Open metadata without read access or following the final reparse point. Only
// after validating that handle do we obtain read access to the same disk file;
// a path replacement cannot turn the subsequent open into a pipe/device open.
// Windows reparse points (including symlinks) are deliberately rejected.
func openRegularFile(path string) (*os.File, error) {
	openPath := path
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, &os.PathError{Op: "open", Path: path, Err: err}
		}
		// Match os.Open's long-path support without changing device namespaces.
		if len(absolute) >= 248 && !strings.HasPrefix(absolute, `\\?\`) && !strings.HasPrefix(absolute, `\\.\`) {
			if strings.HasPrefix(absolute, `\\`) {
				openPath = `\\?\UNC\` + absolute[2:]
			} else {
				openPath = `\\?\` + absolute
			}
		}
	}
	name, err := syscall.UTF16PtrFromString(openPath)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	const share = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE
	handle, err := syscall.CreateFile(name, 0, share, nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer syscall.CloseHandle(handle)
	kind, err := syscall.GetFileType(handle)
	if err != nil {
		return nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if kind != syscall.FILE_TYPE_DISK {
		return nil, &os.PathError{Op: "open", Path: path, Err: errNotRegularFile}
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if info.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return nil, &os.PathError{Op: "open", Path: path, Err: errNotRegularFile}
	}
	if err := reopenRegularFile.Find(); err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	readHandle, _, err := reopenRegularFile.Call(uintptr(handle), syscall.GENERIC_READ, share, 0)
	if syscall.Handle(readHandle) == syscall.InvalidHandle {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(readHandle, path), nil
}
