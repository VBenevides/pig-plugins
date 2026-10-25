package browseruse

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxDriverArchive = 50 << 20
const maxDriverBinary = 100 << 20

func extractDriver(data []byte, release driverRelease) ([]byte, error) {
	read := func(reader io.Reader) ([]byte, error) {
		data, err := io.ReadAll(io.LimitReader(reader, maxDriverBinary+1))
		if err != nil {
			return nil, err
		}
		if len(data) == 0 || len(data) > maxDriverBinary {
			return nil, fmt.Errorf("driver executable has an invalid size")
		}
		return data, nil
	}
	safeName := func(name string) bool {
		return !strings.Contains(name, "\\") && !strings.HasPrefix(name, "/") && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../")
	}
	if release.format == "zip" {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		if len(archive.File) > 1000 {
			return nil, fmt.Errorf("driver archive has too many entries")
		}
		for _, file := range archive.File {
			if path.Base(file.Name) != release.binary {
				continue
			}
			if !safeName(file.Name) || !file.Mode().IsRegular() {
				return nil, fmt.Errorf("unsafe driver archive entry")
			}
			reader, err := file.Open()
			if err != nil {
				return nil, err
			}
			binary, readErr := read(reader)
			return binary, errors.Join(readErr, reader.Close())
		}
	} else if release.format == "tar.gz" {
		compressed, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer compressed.Close()
		archive := tar.NewReader(io.LimitReader(compressed, maxDriverBinary+1))
		for count := 0; count < 1000; count++ {
			header, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if path.Base(header.Name) != release.binary {
				continue
			}
			if !safeName(header.Name) || header.Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("unsafe driver archive entry")
			}
			return read(archive)
		}
	}
	return nil, fmt.Errorf("driver executable is missing from its archive")
}

// installDriver publishes a complete verified version before any old cache cleanup.
func installDriver(ctx context.Context, client *http.Client, cache string, release driverRelease) (result string, err error) {
	if !releasePattern.MatchString(release.version) || filepath.Base(release.binary) != release.binary {
		return "", fmt.Errorf("invalid driver release path")
	}
	destination := filepath.Join(cache, release.version)
	result = filepath.Join(destination, release.binary)
	if info, statErr := os.Lstat(destination); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("driver version directory is unsafe")
		}
		info, statErr = os.Lstat(result)
		if statErr != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("cached driver is not a regular executable")
		}
		version, versionErr := executableVersion(ctx, result)
		if versionErr != nil {
			return "", fmt.Errorf("check cached driver: %w", versionErr)
		}
		if version != release.version {
			return "", fmt.Errorf("cached driver version does not match its directory")
		}
		return result, nil
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	archive, err := fetchDriverData(ctx, client, release.url, maxDriverArchive)
	if err != nil {
		return "", err
	}
	if release.digest != "" {
		sum := sha256.Sum256(archive)
		if hex.EncodeToString(sum[:]) != release.digest {
			return "", fmt.Errorf("driver archive failed SHA-256 verification")
		}
	}
	binary, err := extractDriver(archive, release)
	if err != nil {
		return "", fmt.Errorf("extract driver executable: %w", err)
	}
	stage, err := os.MkdirTemp(cache, ".install-")
	if err != nil {
		return "", err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(stage); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove driver installation stage: %w", cleanupErr))
		}
	}()
	file, err := os.OpenFile(filepath.Join(stage, release.binary), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return "", err
	}
	_, writeErr := file.Write(binary)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err = errors.Join(writeErr, file.Close()); err != nil {
		return "", err
	}
	version, err := executableVersion(ctx, filepath.Join(stage, release.binary))
	if err != nil {
		return "", fmt.Errorf("check downloaded driver: %w", err)
	}
	if version != release.version {
		return "", fmt.Errorf("downloaded driver reported an unexpected version")
	}
	if err := os.Rename(stage, destination); err != nil {
		return "", fmt.Errorf("publish driver version: %w", err)
	}
	return result, nil
}

func pruneDrivers(cache, current string) error {
	entries, err := os.ReadDir(cache)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == current || !releasePattern.MatchString(entry.Name()) {
			continue
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to remove unsafe driver version entry")
		}
		if err := os.RemoveAll(filepath.Join(cache, entry.Name())); err != nil {
			return fmt.Errorf("remove old driver %s: %w", entry.Name(), err)
		}
	}
	return nil
}
