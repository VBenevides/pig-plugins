package lancet

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DownloadTimeout bounds one setup download, as the TypeScript implementation does.
const DownloadTimeout = 900 * time.Second

// LibraryEnv names the environment variable that points at a libonnxruntime to use instead of the pinned one that
// setup installs.
const LibraryEnv = "LANCET_ORT_LIBRARY"

// ortVersion is the ONNX Runtime release that setup downloads. The Node implementation ships the same version.
const ortVersion = "1.30.0"

// ModelState is the cheap presence check of an installed model.
type ModelState struct {
	Installed bool
	// Problem says why the model is not installed: "not downloaded", "<file> is missing", "<file> is damaged"...
	Problem string
}

// ModelPins are the exact digests an install must match. The zero value is invalid; use PinnedModel.
type ModelPins struct {
	URL    string
	Bytes  int64
	SHA256 string
	// Prefix is the directory of the model files inside the archive.
	Prefix string
	Files  map[string]Artifact
}

// Artifact is the pinned size and SHA-256 of one file.
type Artifact struct {
	Bytes  int64
	SHA256 string
}

// PinnedModel returns the pins of the LANCET Nano v0.4.3 release.
func PinnedModel() ModelPins {
	files := make(map[string]Artifact, len(modelFiles))
	for name, a := range modelFiles {
		files[name] = Artifact{Bytes: a.bytes, SHA256: a.sha256}
	}
	return ModelPins{URL: Archive.URL, Bytes: Archive.Bytes, SHA256: Archive.SHA256, Prefix: Archive.Prefix, Files: files}
}

// RuntimePins pin one ONNX Runtime release archive and the shared library inside it.
type RuntimePins struct {
	URL    string
	Bytes  int64
	SHA256 string
	// Entry is the path of the library inside the tar.gz archive; Name is the file name it is installed under.
	Entry   string
	Name    string
	Library Artifact
}

// PinnedRuntime returns the ONNX Runtime pins for this platform. It fails when no release is pinned for it.
func PinnedRuntime() (RuntimePins, error) {
	base := "https://github.com/microsoft/onnxruntime/releases/download/v" + ortVersion + "/onnxruntime-"
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return RuntimePins{
			URL: base + "linux-x64-" + ortVersion + ".tgz", Bytes: 11_306_877,
			SHA256:  "a5ed5a3cac51fbb2e90da632ae43d19212faaa20e76484e62bcb7c23ddb3b3fd",
			Entry:   "onnxruntime-linux-x64-" + ortVersion + "/lib/libonnxruntime.so." + ortVersion,
			Name:    "libonnxruntime.so." + ortVersion,
			Library: Artifact{28_985_152, "245a6f8c38127551057a1cd1ffd59f0a186a227ade4f3492dea2494eb565542e"},
		}, nil
	case "linux/arm64":
		return RuntimePins{
			URL: base + "linux-aarch64-" + ortVersion + ".tgz", Bytes: 10_269_495,
			SHA256:  "e16a27a8ed330bbc698df7330b0cf56e722f354e3bcc92118682c74ef3c3e3da",
			Entry:   "onnxruntime-linux-aarch64-" + ortVersion + "/lib/libonnxruntime.so." + ortVersion,
			Name:    "libonnxruntime.so." + ortVersion,
			Library: Artifact{25_135_496, "64e903a43a041240fd6bcffe0ac6d4fea47ef87bf24b9d097801bd00a9612a4b"},
		}, nil
	case "darwin/arm64":
		return RuntimePins{
			URL: base + "osx-arm64-" + ortVersion + ".tgz", Bytes: 42_373_116,
			SHA256:  "6ebb5062a934537c352937821f9fe9718e7de1a2db1122a93dd363ffd53a7012",
			Entry:   "onnxruntime-osx-arm64-" + ortVersion + "/lib/libonnxruntime." + ortVersion + ".dylib",
			Name:    "libonnxruntime." + ortVersion + ".dylib",
			Library: Artifact{43_879_424, "bcc9110f9d638a119de2db7afb3ba9a1da8085f0cb3401e1c48ae1caf450b6fa"},
		}, nil
	}
	return RuntimePins{}, fmt.Errorf("no ONNX Runtime release is pinned for %s/%s; set %s to a libonnxruntime %s or newer",
		runtime.GOOS, runtime.GOARCH, LibraryEnv, ortVersion)
}

// InstallOptions let tests replace the network and the pins; the zero value is the production setup.
type InstallOptions struct {
	// Client downloads the archives; nil means a client with the default transport. Redirects off HTTPS are refused.
	Client *http.Client
	// Model overrides the model pins; nil means PinnedModel.
	Model *ModelPins
	// Runtime overrides the runtime pins; nil means PinnedRuntime.
	Runtime *RuntimePins
	// OnProgress is told "download" and "verify"; it may be nil.
	OnProgress func(phase string)
}

// InstallResult says what an install did.
type InstallResult struct {
	// Reason is "already-current" or "downloaded".
	Reason    string
	Directory string
}

// ModelDirectory is where the pinned model lives inside the agent directory.
func ModelDirectory(agentDir string) string {
	return filepath.Join(agentDir, "smart-approve-lancet", ModelID)
}

// RuntimeDirectory is where setup installs the pinned ONNX Runtime library.
func RuntimeDirectory(agentDir string) string {
	return filepath.Join(agentDir, "smart-approve-lancet", "onnxruntime-"+ortVersion)
}

// ModelStateOf checks that every model file is present with the pinned size. It does not hash.
func ModelStateOf(dir string) ModelState {
	return modelState(dir, modelArtifacts(nil))
}

func modelArtifacts(pins *ModelPins) map[string]Artifact {
	if pins == nil {
		p := PinnedModel()
		return p.Files
	}
	return pins.Files
}

func modelState(dir string, files map[string]Artifact) ModelState {
	root, err := os.Lstat(dir)
	if err != nil {
		return ModelState{Problem: "not downloaded"}
	}
	if !root.IsDir() {
		return ModelState{Problem: "model path is not a directory"}
	}
	for name, want := range files {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			return ModelState{Problem: name + " is missing"}
		}
		if !info.Mode().IsRegular() || info.Size() != want.Bytes {
			return ModelState{Problem: name + " is damaged"}
		}
	}
	return ModelState{Installed: true}
}

// ModelVerified is true only when every installed file matches the pinned digest.
func ModelVerified(dir string) bool {
	return modelVerified(dir, modelArtifacts(nil))
}

func modelVerified(dir string, files map[string]Artifact) bool {
	if !modelState(dir, files).Installed {
		return false
	}
	for name, want := range files {
		if !fileMatches(filepath.Join(dir, name), want) {
			return false
		}
	}
	return true
}

func fileMatches(file string, want Artifact) bool {
	handle, err := os.Open(file)
	if err != nil {
		return false
	}
	defer handle.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, handle)
	return err == nil && n == want.Bytes && hex.EncodeToString(hash.Sum(nil)) == want.SHA256
}

// LibraryPath returns the libonnxruntime to load: LANCET_ORT_LIBRARY when set (used as given), else the pinned
// library that setup installed. The installed file is hashed first, so a replaced library is never loaded.
func LibraryPath(agentDir string, getenv func(string) string) (string, error) {
	if custom := getenv(LibraryEnv); custom != "" {
		return custom, nil
	}
	pins, err := PinnedRuntime()
	if err != nil {
		return "", err
	}
	return installedLibrary(agentDir, pins)
}

func installedLibrary(agentDir string, pins RuntimePins) (string, error) {
	file := filepath.Join(RuntimeDirectory(agentDir), pins.Name)
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("the ONNX Runtime library is not installed (%s); run /smart-approve-lancet lancet setup or set %s", file, LibraryEnv)
	}
	if !fileMatches(file, pins.Library) {
		return "", fmt.Errorf("the ONNX Runtime library %s does not match its pinned checksum; run /smart-approve-lancet lancet setup", file)
	}
	return file, nil
}

// RuntimeInstalled reports whether the pinned library is installed and verified, or LANCET_ORT_LIBRARY is set.
func RuntimeInstalled(agentDir string, getenv func(string) string) bool {
	_, err := LibraryPath(agentDir, getenv)
	return err == nil
}

// InstallModel downloads, verifies and atomically installs the pinned model. A verified install is left alone.
func InstallModel(ctx context.Context, agentDir string, opts InstallOptions) (InstallResult, error) {
	pins := opts.Model
	if pins == nil {
		p := PinnedModel()
		pins = &p
	}
	dir := ModelDirectory(agentDir)
	if modelVerified(dir, pins.Files) {
		return InstallResult{Reason: "already-current", Directory: dir}, nil
	}
	err := swapInstall(dir, func(staging, out string) error {
		archive := filepath.Join(staging, "model.zip")
		progress(opts, "download")
		if err := download(ctx, opts.Client, pins.URL, pins.Bytes, pins.SHA256, archive); err != nil {
			return fmt.Errorf("model download: %w", err)
		}
		progress(opts, "verify")
		if err := unpackModel(archive, out, pins); err != nil {
			return err
		}
		return os.Remove(archive)
	})
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Reason: "downloaded", Directory: dir}, nil
}

// InstallRuntime downloads, verifies and atomically installs the pinned ONNX Runtime library.
func InstallRuntime(ctx context.Context, agentDir string, opts InstallOptions) (InstallResult, error) {
	var pins RuntimePins
	if opts.Runtime != nil {
		pins = *opts.Runtime
	} else {
		var err error
		if pins, err = PinnedRuntime(); err != nil {
			return InstallResult{}, err
		}
	}
	dir := RuntimeDirectory(agentDir)
	if _, err := installedLibrary(agentDir, pins); err == nil {
		return InstallResult{Reason: "already-current", Directory: dir}, nil
	}
	err := swapInstall(dir, func(staging, out string) error {
		archive := filepath.Join(staging, "runtime.tgz")
		progress(opts, "download")
		if err := download(ctx, opts.Client, pins.URL, pins.Bytes, pins.SHA256, archive); err != nil {
			return fmt.Errorf("ONNX Runtime download: %w", err)
		}
		progress(opts, "verify")
		if err := unpackRuntime(archive, out, pins); err != nil {
			return err
		}
		return os.Remove(archive)
	})
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Reason: "downloaded", Directory: dir}, nil
}

func progress(opts InstallOptions, phase string) {
	if opts.OnProgress != nil {
		opts.OnProgress(phase)
	}
}

// swapInstall runs build in a private staging directory and, only when it succeeds, swaps the result in as target.
// The previous target is retired first and restored when the swap fails, so a failed install leaves the old files.
func swapInstall(target string, build func(staging, out string) error) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", parent, err)
	}
	if info, err := os.Lstat(parent); err != nil {
		return fmt.Errorf("inspect %s: %w", parent, err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symbolic link; refusing to write through it", parent)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(target)+"-download-")
	if err != nil {
		return fmt.Errorf("create staging directory in %s: %w", parent, err)
	}
	out := filepath.Join(staging, "out")
	retired := ""
	fail := func(err error) error {
		os.RemoveAll(staging)
		if retired != "" {
			if _, statErr := os.Lstat(target); errors.Is(statErr, os.ErrNotExist) {
				if restoreErr := os.Rename(retired, target); restoreErr != nil {
					err = errors.Join(err, fmt.Errorf("restore %s: %w", target, restoreErr))
				}
			}
		}
		return err
	}
	if err := build(staging, out); err != nil {
		return fail(err)
	}
	if _, err := os.Lstat(target); err == nil {
		retired = fmt.Sprintf("%s.replaced-%d-%d", target, os.Getpid(), time.Now().UnixNano())
		if err := os.Rename(target, retired); err != nil {
			retired = ""
			return fail(fmt.Errorf("retire %s: %w", target, err))
		}
	}
	if err := os.Rename(out, target); err != nil {
		return fail(fmt.Errorf("install %s: %w", target, err))
	}
	// The new files are live; leftover cleanup is best effort and cannot fail the install.
	os.RemoveAll(staging)
	if retired != "" {
		os.RemoveAll(retired)
	}
	return nil
}

// download fetches url into dest (created exclusively, mode 0600) and checks the exact size and SHA-256.
func download(ctx context.Context, client *http.Client, url string, size int64, sum, dest string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to download %s over a connection that is not HTTPS", url)
	}
	if client == nil {
		client = &http.Client{}
	}
	secure := *client
	secure.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("the download was redirected off HTTPS")
		}
		if len(via) >= 10 {
			return errors.New("the download was redirected too many times")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := secure.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.Request != nil && resp.Request.URL.Scheme != "https" {
		return errors.New("the download was redirected off HTTPS")
	}
	if resp.ContentLength > 0 && resp.ContentLength != size {
		return errors.New("the download is the wrong size")
	}

	handle, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	received, copyErr := io.Copy(io.MultiWriter(handle, hash), io.LimitReader(resp.Body, size+1))
	if copyErr == nil {
		copyErr = handle.Sync()
	}
	if closeErr := handle.Close(); copyErr == nil {
		copyErr = closeErr
	}
	switch {
	case copyErr != nil:
		return copyErr
	case received > size:
		return errors.New("the download is larger than the pinned archive")
	case received != size || hex.EncodeToString(hash.Sum(nil)) != sum:
		return errors.New("the download failed its checksum; nothing was installed")
	}
	return nil
}

// unpackModel extracts exactly the pinned model files from the archive into a new directory out.
func unpackModel(archive, out string, pins *ModelPins) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open the model archive: %w", err)
	}
	defer reader.Close()
	entries := make(map[string]*zip.File, len(pins.Files))
	for _, entry := range reader.File {
		name, ok := strings.CutPrefix(entry.Name, pins.Prefix)
		if !ok {
			continue
		}
		if _, wanted := pins.Files[name]; !wanted {
			continue
		}
		if _, dup := entries[name]; dup {
			return fmt.Errorf("the model archive lists %s twice; nothing was installed", name)
		}
		entries[name] = entry
	}
	if err := os.Mkdir(out, 0o700); err != nil {
		return err
	}
	for name, want := range pins.Files {
		entry, ok := entries[name]
		if !ok {
			return fmt.Errorf("missing %s in the model archive; nothing was installed", name)
		}
		if int64(entry.UncompressedSize64) != want.Bytes {
			return fmt.Errorf("%s in the model archive has the wrong size; nothing was installed", name)
		}
		src, err := entry.Open()
		if err != nil {
			return fmt.Errorf("read %s from the model archive: %w", name, err)
		}
		err = writeVerified(filepath.Join(out, name), src, want, name+" in the model archive")
		src.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// unpackRuntime extracts the one pinned library from the tar.gz archive into a new directory out.
func unpackRuntime(archive, out string, pins RuntimePins) error {
	handle, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer handle.Close()
	gz, err := gzip.NewReader(handle)
	if err != nil {
		return fmt.Errorf("open the ONNX Runtime archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s is missing from the ONNX Runtime archive; nothing was installed", pins.Entry)
		}
		if err != nil {
			return fmt.Errorf("read the ONNX Runtime archive: %w", err)
		}
		if path.Clean(header.Name) != pins.Entry || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size != pins.Library.Bytes {
			return errors.New("the ONNX Runtime library in the archive has the wrong size; nothing was installed")
		}
		if err := os.Mkdir(out, 0o700); err != nil {
			return err
		}
		return writeVerified(filepath.Join(out, pins.Name), tr, pins.Library, "the ONNX Runtime library in the archive")
	}
}

// writeVerified copies exactly want.Bytes from src into a new file (mode 0600, executable bit for libraries is not
// needed by dlopen) and checks the digest. A mismatch removes the file.
func writeVerified(dest string, src io.Reader, want Artifact, what string) (err error) {
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(dest)
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(src, want.Bytes+1))
	if err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	if n != want.Bytes || hex.EncodeToString(hash.Sum(nil)) != want.SHA256 {
		return fmt.Errorf("%s failed its checksum; nothing was installed", what)
	}
	return nil
}
