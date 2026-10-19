// Package distribution owns the fused executable's environment and self-update.
package distribution

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// Update downloads the complete installer before running it. A failed transfer
// never executes its partial output. The installer rebuilds the fused binary.
func Update() (err error) {
	installer, err := os.CreateTemp("", "pig-plugins-update.*")
	if err != nil {
		return fmt.Errorf("create update installer: %w", err)
	}
	path := installer.Name()
	defer func() { err = errors.Join(err, os.Remove(path)) }()
	if err := installer.Close(); err != nil {
		return fmt.Errorf("close update installer: %w", err)
	}
	download := exec.Command("curl", "--proto", "=https", "--tlsv1.2", "-fsSL", "--connect-timeout", "10", "--max-time", "60", "https://raw.githubusercontent.com/VBenevides/pig-plugins/main/scripts/install.sh", "-o", path)
	download.Stdout, download.Stderr = os.Stdout, os.Stderr
	if err := download.Run(); err != nil {
		return fmt.Errorf("download pig-plugins installer: %w", err)
	}
	install := exec.Command("sh", path)
	install.Env = append(os.Environ(), "PIG_PLUGINS_UPDATE=1")
	install.Stdin, install.Stdout, install.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := install.Run(); err != nil {
		return fmt.Errorf("update pig-plugins: %w", err)
	}
	return nil
}
