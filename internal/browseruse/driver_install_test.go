package browseruse

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type driverTransport func(*http.Request) (*http.Response, error)

func (f driverTransport) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func fixtureResponse(data string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data)), Header: make(http.Header)}
}

func TestBrowserDetectionMissingAndAvailable(t *testing.T) {
	if _, err := findBrowser(Chrome, func(string) (string, error) { return "", os.ErrNotExist }); err == nil || !strings.Contains(err.Error(), "Install the browser") {
		t.Fatalf("missing browser diagnostic: %v", err)
	}
	result, err := findBrowser(Firefox, func(name string) (string, error) {
		if name == "firefox-esr" {
			return "/fixture/firefox", nil
		}
		return "", os.ErrNotExist
	})
	if err != nil || result != "/fixture/firefox" {
		t.Fatalf("detected browser %s: %v", result, err)
	}
}

func TestOfficialDriverCatalogs(t *testing.T) {
	client := &http.Client{Transport: driverTransport(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "chrome-for-testing") {
			return fixtureResponse(`{"builds":{"145.0.1":{"version":"145.0.1.9","downloads":{"chromedriver":[{"platform":"linux64","url":"https://storage.googleapis.com/chrome-for-testing-public/driver.zip"}]}}}}`), nil
		}
		return fixtureResponse(`{"tag_name":"v0.37.1","assets":[{"name":"geckodriver-v0.37.1-linux64.tar.gz","browser_download_url":"https://github.com/mozilla/geckodriver/releases/download/v0.37.1/driver.tar.gz","digest":"sha256:` + strings.Repeat("a", 64) + `"}]}`), nil
	})}
	chrome, err := latestDriver(context.Background(), client, Chrome, "145.0.1.2", "linux", "amd64")
	if err != nil || chrome.version != "145.0.1.9" {
		t.Fatalf("Chrome selection: %v %v", chrome, err)
	}
	gecko, err := latestDriver(context.Background(), client, Firefox, "140.0", "linux", "amd64")
	if err != nil || gecko.version != "0.37.1" || gecko.digest != strings.Repeat("a", 64) {
		t.Fatalf("Firefox selection: %v %v", gecko, err)
	}
	if _, err := latestDriver(context.Background(), client, Chrome, "144.0.1.2", "linux", "amd64"); err == nil {
		t.Fatal("selected an incompatible ChromeDriver")
	}
	for _, address := range []string{"http://github.com/archive", "https://evil.example/driver", "https://user:secret@github.com/archive", "https://github.com:444/archive"} {
		if trustedDownloadURL(address) == nil {
			t.Fatalf("unsafe download URL accepted: %s", address)
		}
	}
}

func driverZip(t *testing.T, name, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: name}
	header.SetMode(0700)
	entry, err := archive.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, content); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestDriverInstallReuseUpdateAndFailedDownload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executable is a POSIX shell script")
	}
	cache := t.TempDir()
	downloads := 0
	content := driverZip(t, "geckodriver", "#!/bin/sh\necho geckodriver 1.2.3\n")
	sum := sha256.Sum256(content)
	release := driverRelease{"1.2.3", "https://github.com/mozilla/geckodriver/fixture.zip", hex.EncodeToString(sum[:]), "zip", "geckodriver"}
	client := &http.Client{Transport: driverTransport(func(*http.Request) (*http.Response, error) { downloads++; return fixtureResponse(string(content)), nil })}
	old := filepath.Join(cache, "1.2.2")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := installDriver(context.Background(), client, cache, release)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("deleted old driver before the replacement was ready")
	}
	if _, err := installDriver(context.Background(), client, cache, release); err != nil || downloads != 1 {
		t.Fatalf("cache reuse downloads=%d: %v", downloads, err)
	}
	if err := pruneDrivers(cache, release.version); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old driver remains after successful replacement")
	}
	release.version = "1.2.4"
	release.digest = strings.Repeat("0", 64)
	if _, err := installDriver(context.Background(), client, cache, release); err == nil {
		t.Fatal("accepted bad checksum")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatal("failed update removed the working driver")
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".install-") {
			t.Fatal("installation stage leaked")
		}
	}
}

func TestDriverArchiveRejectsUnsafeExecutable(t *testing.T) {
	for _, name := range []string{"../geckodriver", "/geckodriver", "other/../geckodriver"} {
		if _, err := extractDriver(driverZip(t, name, "fixture"), driverRelease{binary: "geckodriver", format: "zip"}); err == nil {
			t.Fatalf("unsafe member %s", name)
		}
	}
}

func TestManagedClientChecksBrowserBeforeDownloading(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("system application paths can contain a browser")
	}
	profile := filepath.Join(t.TempDir(), "firefox")
	client, err := NewManagedClient(Firefox, profile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), "snapshot", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "browser is unavailable") {
		t.Fatalf("missing browser: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(profile), "drivers")); !os.IsNotExist(err) {
		t.Fatal("created a driver cache before checking the browser")
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDriverDownloadCancellationAndBounds(t *testing.T) {
	request, cancel := context.WithCancel(context.Background())
	cancel()
	client := driverHTTPClient()
	if _, err := fetchDriverData(request, client, "https://api.github.com/repos/mozilla/geckodriver/releases/latest", 10); err == nil {
		t.Fatal("ignored cancellation")
	}
	client = &http.Client{Transport: driverTransport(func(*http.Request) (*http.Response, error) { return fixtureResponse("too much data"), nil })}
	if _, err := fetchDriverData(context.Background(), client, "https://github.com/fixture", 2); err == nil {
		t.Fatal("ignored size limit")
	}
	if err := driverHTTPClient().CheckRedirect(&http.Request{URL: mustDriverURL(t, "http://github.com/fixture")}, nil); err == nil {
		t.Fatal("allowed redirect to HTTP")
	}
}

func TestWrongDriverVersionPreservesOldCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executable is a POSIX shell script")
	}
	cache := t.TempDir()
	old := filepath.Join(cache, "1.2.2")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	archive := driverZip(t, "geckodriver", "#!/bin/sh\necho geckodriver 9.9.9\n")
	client := &http.Client{Transport: driverTransport(func(*http.Request) (*http.Response, error) { return fixtureResponse(string(archive)), nil })}
	_, err := installDriver(context.Background(), client, cache, driverRelease{"1.2.3", "https://github.com/fixture", "", "zip", "geckodriver"})
	if err == nil || !strings.Contains(err.Error(), "unexpected version") {
		t.Fatalf("wrong version accepted: %v", err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal(fmt.Errorf("old cache lost: %w", err))
	}
}
