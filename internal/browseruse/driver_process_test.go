package browseruse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustDriverURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestMain(m *testing.M) {
	if mode := os.Getenv("PIG_BROWSER_DRIVER_TEST_HELPER"); mode != "" {
		if len(os.Args) > 1 && os.Args[1] == "--version" {
			fmt.Println("geckodriver 1.2.3")
			os.Exit(0)
		}
		port := ""
		for i, arg := range os.Args[1:] {
			if strings.HasPrefix(arg, "--port=") {
				port = strings.TrimPrefix(arg, "--port=")
			}
			if arg == "--port" {
				port = os.Args[i+2]
			}
		}
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			var value any
			switch {
			case r.URL.Path == "/status":
				value = map[string]any{"ready": mode == "ready"}
			case r.URL.Path == "/session":
				value = map[string]any{"sessionId": "fixture"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
		})
		if err := http.ListenAndServe("127.0.0.1:"+port, nil); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestOwnedDriverStartupAndExternalOwnership(t *testing.T) {
	t.Setenv("PIG_BROWSER_DRIVER_TEST_HELPER", "ready")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(t.TempDir(), ".lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	process, endpoint, err := launchDriver(context.Background(), binary, Chrome, lock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := process.stop(); err != nil {
			t.Error(err)
		}
	}()
	external, err := NewClient(Chrome, endpoint, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := external.Run(context.Background(), "tabs", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := external.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.done:
		t.Fatal("closing external client stopped its driver")
	default:
	}
	owned, err := NewClient(Chrome, endpoint, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owned.driver = process
	if err := owned.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.done:
	default:
		t.Fatal("owned driver remains running")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("driver cache lock leaked")
	}
}

func TestDriverStartupCancellationCleansProcessAndLock(t *testing.T) {
	t.Setenv("PIG_BROWSER_DRIVER_TEST_HELPER", "not-ready")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(t.TempDir(), ".lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if process, _, err := launchDriver(request, binary, Firefox, lock); err == nil || process != nil {
		t.Fatalf("startup cancellation: %v %v", process, err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("canceled startup left its lock")
	}
}

func TestDriverExecutableMissingReleasesLock(t *testing.T) {
	lock := filepath.Join(t.TempDir(), ".lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := launchDriver(context.Background(), filepath.Join(t.TempDir(), "missing"), Firefox, lock); err == nil {
		t.Fatal("missing driver started")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("failed startup left its lock")
	}
}

func TestLiveManagedFirefoxSetupAndCacheReuse(t *testing.T) {
	if os.Getenv("PIG_BROWSER_LIVE_MANAGED") != "1" {
		t.Skip("set PIG_BROWSER_LIVE_MANAGED=1 for real downloads and Firefox startup")
	}
	profile := filepath.Join(t.TempDir(), "firefox")
	var originalModTime time.Time
	for iteration := 0; iteration < 2; iteration++ {
		client, err := NewManagedClient(Firefox, profile)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := client.Close(context.Background()); err != nil {
				t.Error(err)
			}
		}()
		request, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		_, err = client.Run(request, "snapshot", nil, nil)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if client.driver == nil || !client.headless {
			t.Fatal("managed headless Firefox was not started")
		}
		cache := filepath.Join(filepath.Dir(profile), "drivers", driverCacheName(Firefox))
		entries, err := os.ReadDir(cache)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries {
			if !releasePattern.MatchString(entry.Name()) {
				continue
			}
			count++
			info, err := os.Stat(filepath.Join(cache, entry.Name(), "geckodriver"))
			if err != nil {
				t.Fatal(err)
			}
			if iteration == 0 {
				originalModTime = info.ModTime()
			} else if !info.ModTime().Equal(originalModTime) {
				t.Fatal("unchanged driver was downloaded again")
			}
		}
		if count != 1 {
			t.Fatalf("cached versions %d", count)
		}
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
