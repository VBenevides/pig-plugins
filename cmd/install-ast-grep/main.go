// Command install-ast-grep installs an official, checksummed native release.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releaseURL = "https://api.github.com/repos/ast-grep/ast-grep/releases/latest"
const downloadPrefix = "https://github.com/ast-grep/ast-grep/releases/download/"
const maxArchiveBytes = 64 << 20
const maxBinaryBytes = 128 << 20

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: install-ast-grep <bin-directory>")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 10 {
			return errors.New("refusing insecure or excessive release redirects")
		}
		return nil
	}}
	if err := install(ctx, client, os.Args[1], runtime.GOOS, runtime.GOARCH); err != nil {
		fmt.Fprintln(os.Stderr, "install ast-grep:", err)
		os.Exit(1)
	}
}

func assetName(goos, goarch string) (string, error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	target := map[string]string{"linux": "unknown-linux-gnu", "darwin": "apple-darwin"}[goos]
	if arch == "" || target == "" {
		return "", fmt.Errorf("no automatic ast-grep installation for %s/%s; install ast-grep on PATH manually", goos, goarch)
	}
	return "app-" + arch + "-" + target + ".zip", nil
}

func get(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pig-plugins-installer")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	return resp, nil
}

func install(ctx context.Context, client *http.Client, dir, goos, goarch string) (err error) {
	name, err := assetName(goos, goarch)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "ast-grep")
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("refusing to replace existing %s; put a working ast-grep on PATH", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	resp, err := get(ctx, client, releaseURL)
	if err != nil {
		return err
	}
	metadata, readErr := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	closeErr := resp.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return fmt.Errorf("read release metadata: %w", err)
	}
	if len(metadata) > 1<<20 {
		return errors.New("release metadata exceeds 1 MiB")
	}
	var release struct {
		Assets []asset `json:"assets"`
	}
	if err := json.Unmarshal(metadata, &release); err != nil {
		return fmt.Errorf("decode release metadata: %w", err)
	}
	var selected asset
	for _, it := range release.Assets {
		if it.Name == name {
			selected = it
			break
		}
	}
	if selected.Name == "" {
		return fmt.Errorf("latest ast-grep release has no %s; install ast-grep manually", name)
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(selected.Digest, "sha256:"))
	if err != nil || !strings.HasPrefix(selected.Digest, "sha256:") || len(digest) != sha256.Size {
		return errors.New("release asset has no valid SHA-256 digest; refusing unverified installation")
	}
	if !strings.HasPrefix(selected.URL, downloadPrefix) {
		return errors.New("release asset URL is not an official HTTPS download")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dir, ".ast-grep-install-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(stage)) }()
	archive := filepath.Join(stage, "release.zip")
	file, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	resp, err = get(ctx, client, selected.URL)
	if err != nil {
		return errors.Join(err, file.Close())
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxArchiveBytes+1))
	err = errors.Join(copyErr, resp.Body.Close(), file.Close())
	if err != nil {
		return fmt.Errorf("save release archive: %w", err)
	}
	if size > maxArchiveBytes {
		return errors.New("release archive exceeds 64 MiB")
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), hex.EncodeToString(digest)) {
		return errors.New("release archive SHA-256 mismatch")
	}
	staged, err := extract(archive, stage)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Same-filesystem hard-link publication is atomic and refuses a racing creator.
	if err := os.Link(staged, target); err != nil {
		return fmt.Errorf("publish %s: %w", target, err)
	}
	fmt.Fprintln(os.Stderr, "install: installed ast-grep at", target)
	return nil
}

func extract(archive, stage string) (path string, err error) {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return "", fmt.Errorf("open release archive: %w", err)
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	var binary *zip.File
	for _, entry := range reader.File {
		// Extract only the exact root entry, never archive paths or the sg alias.
		if entry.Name != "ast-grep" {
			continue
		}
		if binary != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 > maxBinaryBytes {
			return "", errors.New("invalid, duplicate or oversized ast-grep executable in archive")
		}
		binary = entry
	}
	if binary == nil {
		return "", errors.New("release archive contains no ast-grep executable")
	}
	source, err := binary.Open()
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	path = filepath.Join(stage, "ast-grep")
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return "", err
	}
	size, copyErr := io.Copy(output, io.LimitReader(source, maxBinaryBytes+1))
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return "", fmt.Errorf("extract ast-grep: %w", err)
	}
	if size == 0 || size > maxBinaryBytes {
		return "", errors.New("ast-grep executable is empty or oversized")
	}
	return path, nil
}
