package cursorbridge

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const maxRuntimeArchive = 64 << 20
const maxRuntimeContent = 96 << 20
const runtimeRecord = ".pig-runtime-integrity.json"

// Published npm integrity values for @cursor/sdk-<platform>@1.2.0. This
// companion package supplies cursorsandbox, ripgrep, and the SDK's parser assets.
var runtimeHashes = map[string]string{
	"linux-x64":    "ainsGAFtkWTBZrLABp82Hvfv7VB/FxmZHE42qMVotFTRDwOY2iduvhSig910bVxv+kw3S00Srmu/9CxklLwhbw==",
	"linux-arm64":  "Rs+cd5snuhxMAHkpqRfEGPB1qAsfqSOYJh5SOHZTOWy2LkRVO62WHwd4x80C2emj+dosTV5WAcuF2kMKKcrDJg==",
	"darwin-x64":   "mJTFdX+qv9dHAm59tT7uq7ci7+BDvFoNk4gdF2zGp9SLULicY+FdFD64p1qLen1bX5Y2soZaX5mWBy7BfObjcg==",
	"darwin-arm64": "UkZ/RdxKCpOQ85nRgzSq+g0h+gIw36/lCEvQvomhs5EkSFZW3OwbeEzkAU3UYglZ7t3RNHj3mc5C5cdDtHguvw==",
	"win32-x64":    "p3rkyta6R/nXIukd3/ifJq32otkkGoUi41T2jxnHbPHMhzE2F8OuJQGBVmORuQLMr+bIGGVZmCxertgs4WgzcQ==",
}

type runtimeIntegrity struct {
	Archive string            `json:"archive"`
	Files   map[string]string `json:"files"`
}

func runtimePlatform(goos, goarch string) string {
	if goos == "windows" {
		goos = "win32"
	}
	if goarch == "amd64" {
		goarch = "x64"
	}
	return goos + "-" + goarch
}

func runtimeFile(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name && !strings.Contains(name, "\\") && !strings.HasPrefix(name, "/") && filepath.IsLocal(filepath.FromSlash(name)) && name != runtimeRecord
}

func checkRuntime(target, expected string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Cursor runtime directory is not a regular directory")
	}
	recordPath := filepath.Join(target, runtimeRecord)
	recordInfo, err := os.Lstat(recordPath)
	if err != nil {
		return err
	}
	if !recordInfo.Mode().IsRegular() || recordInfo.Size() > 1<<20 {
		return errors.New("invalid Cursor runtime integrity record")
	}
	data, err := os.ReadFile(recordPath)
	if err != nil {
		return err
	}
	var record runtimeIntegrity
	if err = json.Unmarshal(data, &record); err != nil {
		return err
	}
	if record.Archive != expected || len(record.Files) == 0 || len(record.Files) > 128 {
		return errors.New("invalid Cursor runtime integrity record")
	}
	var total int64
	for name, digest := range record.Files {
		if !runtimeFile(name) {
			return errors.New("invalid Cursor runtime integrity path")
		}
		filePath := filepath.Join(target, filepath.FromSlash(name))
		// Reject symlinks anywhere below the private runtime directory.
		curr := target
		for _, part := range strings.Split(name, "/") {
			curr = filepath.Join(curr, part)
			info, err = os.Lstat(curr)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("Cursor runtime contains a symbolic link")
			}
		}
		if !info.Mode().IsRegular() || info.Size() > maxRuntimeContent-total {
			return errors.New("invalid Cursor runtime file")
		}
		total += info.Size()
		if runtime.GOOS != "windows" && strings.HasPrefix(name, "bin/") && info.Mode().Perm()&0111 == 0 {
			return errors.New("Cursor runtime binary is not executable")
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, io.LimitReader(file, maxRuntimeContent+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		if hex.EncodeToString(hash.Sum(nil)) != digest {
			return errors.New("Cursor runtime file failed its integrity check")
		}
	}
	return nil
}

// installRuntime is called only by the explicit installer. A standalone bridge
// does not include this optional SDK package. Put it where the SDK's platform
// package locator searches relative to process.execPath, never in the workspace.
func installRuntime(ctx context.Context, bridgeDir, platform, expected string, fetch func(context.Context) (io.ReadCloser, error)) (err error) {
	parent := bridgeDir
	for _, component := range []string{"node_modules", "@cursor"} {
		parent = filepath.Join(parent, component)
		if err = os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, statErr := os.Lstat(parent)
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Cursor runtime parent is not a regular directory")
		}
	}
	target := filepath.Join(parent, "sdk-"+platform)
	if err = checkRuntime(target, expected); err == nil {
		return nil
	}
	lock := target + ".lock"
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("Cursor runtime installation is locked: %w", err)
	}
	defer func() { err = errors.Join(err, os.Remove(lock)) }()
	if err = checkRuntime(target, expected); err == nil {
		return nil
	}
	if _, statErr := os.Lstat(target); statErr == nil {
		return errors.New("Cursor runtime directory exists but failed verification; preserve it and resolve the integrity failure manually")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	stage, err := os.MkdirTemp(parent, ".cursor-runtime-*")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(stage)) }()
	archive, err := os.CreateTemp(stage, "archive-*")
	if err != nil {
		return err
	}
	response, err := fetch(ctx)
	if err != nil {
		return errors.Join(err, archive.Close())
	}
	hash := sha512.New()
	n, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(response, maxRuntimeArchive+1))
	closeErr := errors.Join(response.Close(), archive.Close())
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if n > maxRuntimeArchive || base64.StdEncoding.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("Cursor runtime archive failed its pinned integrity check")
	}
	root := filepath.Join(stage, "package")
	if err = os.Mkdir(root, 0700); err != nil {
		return err
	}
	files, err := extractRuntime(archive.Name(), root)
	if err != nil {
		return err
	}
	required := []string{"bin/rg", "bin/cursorsandbox"}
	if strings.HasPrefix(platform, "win32-") {
		required = []string{"bin/rg.exe", "bin/cursorsandbox.exe"}
	}
	for _, name := range required {
		if files[name] == "" {
			return fmt.Errorf("Cursor runtime archive lacks %s", name)
		}
	}
	record, err := json.Marshal(runtimeIntegrity{Archive: expected, Files: files})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, runtimeRecord), record, 0600); err != nil {
		return err
	}
	if err = checkRuntime(root, expected); err != nil {
		return err
	}
	return os.Rename(root, target)
}

func extractRuntime(archive, root string) (map[string]string, error) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	reader := tar.NewReader(io.LimitReader(gz, maxRuntimeContent+(1<<20)))
	files := map[string]string{}
	var total int64
	for count := 0; count < 128; count++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		name := strings.TrimPrefix(header.Name, "package/")
		if name == header.Name || !runtimeFile(name) || header.Typeflag != tar.TypeReg || files[name] != "" {
			return nil, errors.New("unsafe Cursor runtime archive entry")
		}
		if header.Size < 0 || header.Size > maxRuntimeContent-total {
			return nil, errors.New("Cursor runtime archive is too large")
		}
		total += header.Size
		target := filepath.Join(root, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, err
		}
		mode := os.FileMode(0600)
		if strings.HasPrefix(name, "bin/") {
			mode = 0700
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(output, hash), reader)
		closeErr := output.Close()
		if copyErr != nil || closeErr != nil {
			return nil, errors.Join(copyErr, closeErr)
		}
		if n != header.Size {
			return nil, errors.New("truncated Cursor runtime file")
		}
		files[name] = hex.EncodeToString(hash.Sum(nil))
	}
	return nil, errors.New("Cursor runtime archive has too many entries")
}
