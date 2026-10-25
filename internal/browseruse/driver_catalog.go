package browseruse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

type driverRelease struct{ version, url, digest, format, binary string }
type driverDownload struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
}
type chromeRelease struct {
	Version   string                      `json:"version"`
	Downloads map[string][]driverDownload `json:"downloads"`
}

func trustedDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return fmt.Errorf("driver downloads require an official HTTPS source")
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "googlechromelabs.github.io", "storage.googleapis.com":
		return nil
	}
	return fmt.Errorf("driver download host is not allowed")
}

func driverHTTPClient() *http.Client {
	return &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many driver download redirects")
		}
		return trustedDownloadURL(request.URL.String())
	}}
}

type driverFetchError struct{ cause error }

func (e driverFetchError) Error() string {
	return "driver resource request failed. Check network access to the official release servers"
}
func (e driverFetchError) Unwrap() error { return e.cause }

func fetchDriverData(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	if err := trustedDownloadURL(rawURL); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "pig-plugins-browser-use")
	response, err := client.Do(request)
	if err != nil {
		return nil, driverFetchError{cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("driver resource returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("driver resource exceeds its size limit")
	}
	return data, nil
}

func latestDriver(ctx context.Context, client *http.Client, mode Mode, browserVersion, goos, arch string) (driverRelease, error) {
	binary := "geckodriver"
	if mode == Chrome {
		binary = "chromedriver"
	}
	if goos == "windows" {
		binary += ".exe"
	}
	if mode == Chrome {
		platform := map[string]string{"linux/amd64": "linux64", "linux/arm64": "linux-arm64", "darwin/amd64": "mac-x64", "darwin/arm64": "mac-arm64", "windows/amd64": "win64", "windows/386": "win32"}[goos+"/"+arch]
		if platform == "" {
			return driverRelease{}, fmt.Errorf("ChromeDriver has no supported download for %s/%s", goos, arch)
		}
		parts := strings.Split(browserVersion, ".")
		if len(parts) < 3 {
			return driverRelease{}, fmt.Errorf("Chrome version must include its build number")
		}
		data, err := fetchDriverData(ctx, client, "https://googlechromelabs.github.io/chrome-for-testing/latest-patch-versions-per-build-with-downloads.json", 8<<20)
		if err != nil {
			return driverRelease{}, err
		}
		var catalog struct {
			Builds map[string]chromeRelease `json:"builds"`
		}
		if err := json.Unmarshal(data, &catalog); err != nil {
			return driverRelease{}, fmt.Errorf("parse ChromeDriver metadata: %w", err)
		}
		release, ok := catalog.Builds[strings.Join(parts[:3], ".")]
		if !ok {
			return driverRelease{}, fmt.Errorf("no current ChromeDriver matches browser build %s. Update Chrome or configure an external driver", strings.Join(parts[:3], "."))
		}
		for _, download := range release.Downloads["chromedriver"] {
			if download.Platform == platform && releasePattern.MatchString(release.Version) {
				if err := trustedDownloadURL(download.URL); err != nil {
					return driverRelease{}, err
				}
				return driverRelease{release.Version, download.URL, "", "zip", binary}, nil
			}
		}
		return driverRelease{}, fmt.Errorf("no compatible ChromeDriver download for %s", platform)
	}
	platform := map[string]string{"linux/amd64": "linux64", "linux/arm64": "linux-aarch64", "darwin/amd64": "macos", "darwin/arm64": "macos-aarch64", "windows/amd64": "win64", "windows/386": "win32", "windows/arm64": "win-aarch64"}[goos+"/"+arch]
	if platform == "" {
		return driverRelease{}, fmt.Errorf("geckodriver has no supported download for %s/%s", goos, arch)
	}
	data, err := fetchDriverData(ctx, client, "https://api.github.com/repos/mozilla/geckodriver/releases/latest", 2<<20)
	if err != nil {
		return driverRelease{}, err
	}
	var release struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return driverRelease{}, fmt.Errorf("parse geckodriver metadata: %w", err)
	}
	version := strings.TrimPrefix(release.Tag, "v")
	if !releasePattern.MatchString(version) {
		return driverRelease{}, fmt.Errorf("invalid geckodriver release version")
	}
	format := "tar.gz"
	if goos == "windows" {
		format = "zip"
	}
	name := "geckodriver-" + release.Tag + "-" + platform + "." + format
	for _, asset := range release.Assets {
		if asset.Name == name {
			if err := trustedDownloadURL(asset.URL); err != nil {
				return driverRelease{}, err
			}
			if !strings.HasPrefix(asset.Digest, "sha256:") || len(asset.Digest) != 71 {
				return driverRelease{}, fmt.Errorf("geckodriver release has no SHA-256 digest")
			}
			return driverRelease{version, asset.URL, strings.TrimPrefix(asset.Digest, "sha256:"), format, binary}, nil
		}
	}
	return driverRelease{}, fmt.Errorf("no geckodriver asset for %s/%s", goos, arch)
}

func driverCacheName(mode Mode) string {
	return string(mode) + "-" + runtime.GOOS + "-" + runtime.GOARCH
}
