package browseruse

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDriverFetchErrorsHideSignedURLsAndPreserveCause(t *testing.T) {
	cause := errors.New("fixture transport failure")
	client := &http.Client{Transport: driverTransport(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: "https://release-assets.githubusercontent.com/fixture?token=must-not-leak", Err: cause}
	})}
	_, err := fetchDriverData(context.Background(), client, "https://github.com/fixture", 100)
	if err == nil || !errors.Is(err, cause) || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("unsafe fetch diagnostic: %v", err)
	}
}

func TestManagedCacheLockPreventsSecondSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture browser is a POSIX shell script")
	}
	root := t.TempDir()
	browserDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(browserDir, "firefox"), []byte("#!/bin/sh\necho Mozilla Firefox 140.0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", browserDir)
	cache := filepath.Join(root, "drivers", driverCacheName(Firefox))
	lock := filepath.Join(cache, ".lock")
	if err := os.MkdirAll(lock, 0700); err != nil {
		t.Fatal(err)
	}
	client, err := NewManagedClient(Firefox, filepath.Join(root, "firefox"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), "snapshot", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "cache is in use") {
		t.Fatalf("cache contention: %v", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("second setup removed another owner's lock")
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("closing second client removed another owner's lock")
	}
}

func TestManagedDriverVisibilityEnvironment(t *testing.T) {
	got := driverEnvironment([]string{"PATH=/fixture", "MOZ_HEADLESS=1", "DISPLAY=:1", "Moz_Headless=1"})
	if strings.Join(got, "|") != "PATH=/fixture|DISPLAY=:1" {
		t.Fatalf("visibility environment: %v", got)
	}
}
