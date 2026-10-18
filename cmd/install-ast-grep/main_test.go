package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func archiveFixture(t *testing.T, names ...string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("fixture executable")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func clientFixture(t *testing.T, archive []byte, digest, url string, status int) (*http.Client, *int) {
	t.Helper()
	hash := sha256.Sum256(archive)
	if digest == "valid" {
		digest = "sha256:" + hex.EncodeToString(hash[:])
	}
	metadata, err := json.Marshal(map[string]any{"assets": []asset{{Name: "app-x86_64-unknown-linux-gnu.zip", URL: url, Digest: digest}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		*calls++
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		data := archive
		if req.URL.String() == releaseURL {
			data = metadata
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header), Request: req}, nil
	})}
	return client, calls
}

func TestInstallVerifiedReleaseAndPreserveExisting(t *testing.T) {
	archive := archiveFixture(t, "ast-grep", "sg", "../escape")
	client, calls := clientFixture(t, archive, "valid", downloadPrefix+"99.0.0/app.zip", 200)
	dir := t.TempDir()
	if err := install(context.Background(), client, dir, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "ast-grep")
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "fixture executable" {
		t.Fatalf("installed: %s %v", data, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm()&0100 == 0 {
		t.Fatalf("not executable: %v %v", info, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("unexpected files: %v %v", entries, err)
	}
	if err := install(context.Background(), client, dir, "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("replaced existing executable: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("existing executable caused network calls: %d", *calls)
	}
}

func TestInstallFailuresLeaveNoOutput(t *testing.T) {
	valid := archiveFixture(t, "ast-grep")
	for _, tc := range []struct {
		name        string
		archive     []byte
		digest, url string
		status      int
	}{
		{"checksum", valid, "sha256:" + strings.Repeat("0", 64), downloadPrefix + "v/app.zip", 200},
		{"no-digest", valid, "", downloadPrefix + "v/app.zip", 200},
		{"untrusted-url", valid, "valid", "http://example.invalid/app.zip", 200},
		{"bad-zip", []byte("not a zip"), "valid", downloadPrefix + "v/app.zip", 200},
		{"no-binary", archiveFixture(t, "sg"), "valid", downloadPrefix + "v/app.zip", 200},
		{"traversal", archiveFixture(t, "../ast-grep"), "valid", downloadPrefix + "v/app.zip", 200},
		{"duplicate", archiveFixture(t, "ast-grep", "ast-grep"), "valid", downloadPrefix + "v/app.zip", 200},
		{"http-error", valid, "valid", downloadPrefix + "v/app.zip", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := clientFixture(t, tc.archive, tc.digest, tc.url, tc.status)
			dir := t.TempDir()
			if err := install(context.Background(), client, dir, "linux", "amd64"); err == nil {
				t.Fatal("failure hidden")
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
				t.Fatalf("left partial installation: %v %v", entries, err)
			}
		})
	}
}

func TestPlatformSelectionAndCancellation(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			if name, err := assetName(goos, arch); err != nil || !strings.HasSuffix(name, ".zip") {
				t.Fatalf("%s/%s: %s %v", goos, arch, name, err)
			}
		}
	}
	if _, err := assetName("linux", "riscv64"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
	client, _ := clientFixture(t, archiveFixture(t, "ast-grep"), "valid", downloadPrefix+"v/app.zip", 200)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := install(ctx, client, t.TempDir(), "linux", "amd64"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
