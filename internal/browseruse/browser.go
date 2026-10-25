package browseruse

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type browserInstallation struct{ path, version string }

var versionPattern = regexp.MustCompile(`\b[0-9]+(?:\.[0-9]+){1,3}\b`)
var releasePattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){2,3}$`)

func findBrowser(mode Mode, lookup func(string) (string, error)) (string, error) {
	names := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
	if mode == Firefox {
		names = []string{"firefox", "firefox-esr"}
	}
	if runtime.GOOS == "darwin" {
		if mode == Firefox {
			names = append(names, "/Applications/Firefox.app/Contents/MacOS/firefox")
		} else {
			names = append(names, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium")
		}
	}
	if runtime.GOOS == "windows" {
		for _, root := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			if mode == Firefox {
				names = append(names, filepath.Join(root, "Mozilla Firefox", "firefox.exe"))
			} else {
				names = append(names, filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"))
			}
		}
	}
	for _, name := range names {
		if executable, err := lookup(name); err == nil {
			return executable, nil
		}
	}
	return "", fmt.Errorf("%s browser is unavailable. Install the browser or add its executable to PATH. Browser-use downloads drivers, not browsers", mode)
}

func executableVersion(ctx context.Context, binary string) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return commandVersion(exec.CommandContext(bounded, binary, "--version"))
}

func commandVersion(command *exec.Cmd) (string, error) {
	command.WaitDelay = time.Second
	output := &boundedOutput{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("read executable version: %w", err)
	}
	version := versionPattern.FindString(output.text.String())
	if version == "" {
		return "", fmt.Errorf("executable did not report a recognizable version")
	}
	return version, nil
}

type boundedOutput struct{ text strings.Builder }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.text.Len()+len(data) > 16384 {
		return 0, fmt.Errorf("executable version output exceeds its limit")
	}
	return b.text.Write(data)
}

func detectBrowser(ctx context.Context, mode Mode) (browserInstallation, error) {
	binary, err := findBrowser(mode, exec.LookPath)
	if err != nil {
		return browserInstallation{}, err
	}
	var version string
	if runtime.GOOS != "windows" {
		version, err = executableVersion(ctx, binary)
	}
	if runtime.GOOS == "windows" {
		// Read file metadata. Chrome's Windows --version can open a window.
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		command := exec.CommandContext(bounded, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-Item -LiteralPath $env:PIG_BROWSER_VERSION_PATH).VersionInfo.ProductVersion")
		command.Env = append(os.Environ(), "PIG_BROWSER_VERSION_PATH="+binary)
		version, err = commandVersion(command)
	}
	if err != nil {
		return browserInstallation{}, fmt.Errorf("check %s browser: %w", mode, err)
	}
	return browserInstallation{binary, version}, nil
}
