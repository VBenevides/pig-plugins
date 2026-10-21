// Package distribution owns the fused executable's environment and self-update.
package distribution

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	pigplugins "github.com/VBenevides/pig-plugins"
	"github.com/VBenevides/pig-plugins/internal/updates"
)

// Prepare replaces the environment setup formerly performed by the launcher.
func Prepare(version string) error {
	home := os.Getenv("PIG_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve PiG home: %w", err)
		}
		home = filepath.Join(userHome, ".pig")
	}
	if err := os.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Join(home, "bin")); err != nil {
		return err
	}
	return os.Setenv("PIG_PLUGINS_HOST_VERSION", version)
}

// Update installs only a strictly newer plugins version, pinned to the checked commit.
func Update() error { return update(pigplugins.Version()) }

func update(current string) (err error) {
	if _, err := updates.Newer(current, current); err != nil {
		return fmt.Errorf("cannot determine local plugins version: %w", err)
	}
	dir, err := os.MkdirTemp("", "pig-plugins-update.*")
	if err != nil {
		return fmt.Errorf("create update workspace: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	metadata := filepath.Join(dir, "metadata")
	if err := download("https://api.github.com/repos/VBenevides/pig-plugins/git/ref/heads/main", metadata, "4096"); err != nil {
		return fmt.Errorf("resolve remote plugins commit: %w", err)
	}
	body, err := readMetadata(metadata)
	if err != nil {
		return err
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &ref); err != nil {
		return fmt.Errorf("decode remote plugins commit: %w", err)
	}
	commit := ref.Object.SHA
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commit) {
		return fmt.Errorf("invalid remote plugins commit %q", commit)
	}
	base := "https://raw.githubusercontent.com/VBenevides/pig-plugins/" + commit
	if err := download(base+"/VERSION", metadata, "4096"); err != nil {
		return fmt.Errorf("read remote plugins version: %w", err)
	}
	body, err = readMetadata(metadata)
	if err != nil {
		return err
	}
	remote := strings.TrimSpace(string(body))
	newer, err := updates.Newer(remote, current)
	if err != nil {
		return fmt.Errorf("compare plugins versions: %w", err)
	}
	if !newer {
		fmt.Fprintf(os.Stdout, "pig-plugins: no update needed (local %s, remote %s)\n", current, remote)
		return nil
	}
	path := filepath.Join(dir, "install.sh")
	if err := download(base+"/scripts/install.sh", path, "1048576"); err != nil {
		return fmt.Errorf("download pig-plugins installer: %w", err)
	}
	install := exec.Command("sh", path)
	// The installer must build the exact checkout whose VERSION was compared.
	install.Env = append(os.Environ(), "PIG_PLUGINS_UPDATE=1", "PIG_PLUGINS_COMMIT="+commit,
		"PIG_PLUGINS_REPO=https://github.com/VBenevides/pig-plugins")
	install.Stdin, install.Stdout, install.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := install.Run(); err != nil {
		return fmt.Errorf("update pig-plugins: %w", err)
	}
	return nil
}

func download(url, path, maxSize string) error {
	cmd := exec.Command("curl", "--proto", "=https", "--proto-redir", "=https", "--tlsv1.2", "-fsSL",
		"--connect-timeout", "10", "--max-time", "60", "--max-filesize", maxSize, url, "-o", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func readMetadata(path string) (body []byte, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open update metadata: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	body, err = io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, fmt.Errorf("read update metadata: %w", err)
	}
	if len(body) > 4096 {
		return nil, fmt.Errorf("update metadata exceeds 4096 bytes")
	}
	return body, nil
}
