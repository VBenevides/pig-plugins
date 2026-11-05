package cursorbridge

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const releaseBase = "https://github.com/cursor/sdk-bridge/releases/download/v" + Version + "/"
const maxArchive = 160 << 20
const maxExecutable = 256 << 20

var artifactHashes = map[string]string{
	"darwin-arm64":  "fa032dbfa7c65be7d6b4b9c828a52116aca0be7c2f06625e833e888844254792",
	"darwin-amd64":  "570e78346a41784926f6436fe8fb6e18377557bfc44f6f814bc2cbb2f5020a78",
	"linux-arm64":   "d479dbd50fb100bb0022d18c0eca75b27e8f600d168f1116613782321b6099c9",
	"linux-amd64":   "ce3dd35e963c68a79ba6db5879c6ece048aa80cff16312faf9c6b43b9409763a",
	"windows-amd64": "6ac53405c4b915f196b536f599a84301150d2dcac0b442e915cdb1d483a4ed8b",
}

func BinaryPath(agentDir, goos string) string {
	name := "cursor-sdk-bridge"
	if goos == "windows" {
		name += ".exe"
	}
	return filepath.Join(agentDir, "bin", "cursor-sdk-bridge", Version, name)
}

// Installed checks local integrity without network access or process execution.
func Installed(agentDir, goos string) (string, error) {
	path := BinaryPath(agentDir, goos)
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("Cursor bridge is not installed; run pig-plugins --install-cursor: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxExecutable {
		return "", errors.New("invalid installed Cursor bridge executable")
	}
	digest, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return "", fmt.Errorf("read Cursor bridge integrity record: %w", err)
	}
	if len(digest) != 64 {
		return "", errors.New("invalid Cursor bridge integrity record")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(file, maxExecutable+1)); err != nil {
		return "", err
	}
	if hex.EncodeToString(hash.Sum(nil)) != string(digest) {
		return "", errors.New("installed Cursor bridge failed its integrity check")
	}
	return path, nil
}

func Install(ctx context.Context, agentDir, goos, goarch string) (string, error) {
	arch := goarch
	if arch == "amd64" {
		arch = "x64"
	}
	platform := goos
	if platform == "windows" {
		platform = "win32"
	}
	hash, ok := artifactHashes[goos+"-"+goarch]
	if !ok {
		return "", fmt.Errorf("Cursor bridge does not support %s/%s", goos, goarch)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "https" {
			return errors.New("unsafe Cursor release redirect")
		}
		return nil
	}}
	url := releaseBase + "cursor-sdk-bridge-standalone-" + platform + "-" + arch + ".tar.gz"
	path, err := install(ctx, agentDir, goos, hash, func(ctx context.Context) (io.ReadCloser, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("download Cursor bridge: %w", err)
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return nil, fmt.Errorf("Cursor bridge download returned HTTP %d", response.StatusCode)
		}
		return response.Body, nil
	})
	if err != nil {
		return "", err
	}
	packagePlatform := runtimePlatform(goos, goarch)
	runtimeHash, ok := runtimeHashes[packagePlatform]
	if !ok {
		return "", errors.New("Cursor SDK runtime does not support this platform")
	}
	runtimeURL := "https://registry.npmjs.org/@cursor/sdk-" + packagePlatform + "/-/sdk-" + packagePlatform + "-" + Version + ".tgz"
	err = installRuntime(ctx, filepath.Dir(path), packagePlatform, runtimeHash, func(ctx context.Context) (io.ReadCloser, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, runtimeURL, nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("download Cursor SDK runtime: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("Cursor SDK runtime download returned HTTP %d", response.StatusCode)
		}
		return response.Body, nil
	})
	if err != nil {
		return "", fmt.Errorf("install Cursor SDK runtime: %w", err)
	}
	return path, nil
}

func install(ctx context.Context, agentDir, goos, expected string, fetch func(context.Context) (io.ReadCloser, error)) (path string, err error) {
	if path, err = Installed(agentDir, goos); err == nil {
		return path, nil
	}
	target := filepath.Dir(BinaryPath(agentDir, goos))
	parent := filepath.Dir(target)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", fmt.Errorf("create Cursor install directory: %w", err)
	}
	// Atomic mkdir isolates concurrent installers. Do not remove another owner's lock.
	lock := target + ".lock"
	if err = os.Mkdir(lock, 0700); err != nil {
		return "", fmt.Errorf("Cursor installation is locked; remove the lock only after its owner exits: %w", err)
	}
	defer func() { err = errors.Join(err, os.Remove(lock)) }()
	if path, checkErr := Installed(agentDir, goos); checkErr == nil {
		return path, nil
	}
	// Never replace an occupied installation. A failed download cannot damage it.
	if _, checkErr := os.Lstat(target); checkErr == nil {
		return "", errors.New("Cursor bridge directory exists but failed verification; preserve it and resolve the integrity failure manually")
	} else if !errors.Is(checkErr, os.ErrNotExist) {
		return "", checkErr
	}
	stage, err := os.MkdirTemp(parent, ".cursor-install-*")
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(stage)) }()
	archive, err := os.CreateTemp(stage, "archive-*")
	if err != nil {
		return "", err
	}
	response, err := fetch(ctx)
	if err != nil {
		archive.Close()
		return "", err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(response, maxArchive+1))
	closeErr := errors.Join(response.Close(), archive.Close())
	if copyErr != nil || closeErr != nil {
		return "", errors.Join(copyErr, closeErr)
	}
	if size > maxArchive || hex.EncodeToString(hash.Sum(nil)) != expected {
		return "", errors.New("Cursor archive failed its pinned integrity check")
	}
	input, err := os.Open(archive.Name())
	if err != nil {
		return "", err
	}
	gz, err := gzip.NewReader(input)
	if err != nil {
		input.Close()
		return "", fmt.Errorf("open Cursor archive: %w", err)
	}
	executable := filepath.Base(BinaryPath(agentDir, goos))
	output := filepath.Join(stage, executable)
	found := false
	reader := tar.NewReader(io.LimitReader(gz, maxExecutable+(8<<20)))
	for entries := 0; entries < 128; entries++ {
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			err = nextErr
			break
		}
		// Extract only the known executable. Archive paths never become filesystem paths.
		if header.Name != "bin/"+executable {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxExecutable {
			err = errors.New("invalid Cursor executable in archive")
			break
		}
		file, openErr := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if openErr != nil {
			err = openErr
			break
		}
		h := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(file, h), reader)
		err = errors.Join(copyErr, file.Close())
		if err != nil {
			break
		}
		err = os.WriteFile(output+".sha256", []byte(hex.EncodeToString(h.Sum(nil))), 0600)
		if err != nil {
			break
		}
		found = true
	}
	err = errors.Join(err, gz.Close(), input.Close())
	if err != nil {
		return "", fmt.Errorf("extract Cursor bridge: %w", err)
	}
	if !found {
		return "", errors.New("Cursor archive has no supported bridge executable")
	}
	if err = os.Remove(archive.Name()); err != nil {
		return "", err
	}
	if err = os.Rename(stage, target); err != nil {
		return "", fmt.Errorf("publish Cursor bridge: %w", err)
	}
	return BinaryPath(agentDir, goos), nil
}

// Flag accepts only a standalone install invocation. It never consumes normal prompts.
func InstallFlag(args []string) (bool, error) {
	if len(args) > 0 && args[0] == "--install-cursor" {
		if len(args) != 1 {
			return false, errors.New("usage: pig-plugins --install-cursor")
		}
		return true, nil
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--install-cursor") {
			return false, errors.New("use pig-plugins --install-cursor as a standalone command")
		}
	}
	return false, nil
}
