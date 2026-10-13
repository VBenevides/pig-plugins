package imageview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var pruneNow = time.Unix(1_800_000_000, 0)

func blobFile(t *testing.T, root, seed string, size int, age time.Duration) string {
	t.Helper()
	name := strings.Repeat(seed, 64)[:64] + ".png"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	when := pruneNow.Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func TestPruneRemovesUnusedBlobsAfterTwoDaysAndKeepsRecentOnes(t *testing.T) {
	root := t.TempDir()
	old := blobFile(t, root, "a", 10, 49*time.Hour)
	edge := blobFile(t, root, "b", 10, 48*time.Hour) // Exactly two days is not yet older than two days.
	recent := blobFile(t, root, "c", 10, 47*time.Hour)
	result, err := Prune(root, pruneNow, MaxBlobAge, MaxBlobBytes)
	if err != nil {
		t.Fatal(err)
	}
	if exists(old) || !exists(edge) || !exists(recent) {
		t.Fatalf("old=%v edge=%v recent=%v", exists(old), exists(edge), exists(recent))
	}
	if result.Expired != 1 || result.Kept != 2 || result.KeptBytes != 20 {
		t.Fatalf("%+v", result)
	}
}

func TestPruneEnforcesTheSizeCapLeastRecentlyUsedFirst(t *testing.T) {
	root := t.TempDir()
	oldest := blobFile(t, root, "a", 40, 30*time.Hour)
	middle := blobFile(t, root, "b", 40, 20*time.Hour)
	newest := blobFile(t, root, "c", 40, time.Hour)
	result, err := Prune(root, pruneNow, MaxBlobAge, 100)
	if err != nil {
		t.Fatal(err)
	}
	if exists(oldest) || !exists(middle) || !exists(newest) || result.OverCap != 1 || result.KeptBytes != 80 {
		t.Fatalf("oldest=%v middle=%v newest=%v %+v", exists(oldest), exists(middle), exists(newest), result)
	}
	// A cap that fits exactly removes nothing; a smaller one removes more, still oldest first.
	if result, _ := Prune(root, pruneNow, MaxBlobAge, 80); result.OverCap != 0 {
		t.Fatalf("exact fit must keep everything: %+v", result)
	}
	if _, err := Prune(root, pruneNow, MaxBlobAge, 50); err != nil || exists(middle) || !exists(newest) {
		t.Fatalf("middle=%v newest=%v %v", exists(middle), exists(newest), err)
	}
}

func TestPruneTouchesOnlyBlobsAndStaleTempFiles(t *testing.T) {
	root := t.TempDir()
	write := func(name string, age time.Duration) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		when := pruneNow.Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
		return path
	}
	note := write("notes.txt", 100*time.Hour)
	shortName := write(strings.Repeat("a", 63)+".png", 100*time.Hour)
	upper := write(strings.Repeat("A", 64)+".png", 100*time.Hour)
	staleTemp := write(".image-123", 2*time.Hour)
	freshTemp := write(".image-456", 5*time.Minute)
	dir := filepath.Join(root, strings.Repeat("d", 64)+".png")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, strings.Repeat("e", 64)+".png")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	result, err := Prune(root, pruneNow, MaxBlobAge, 0)
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"notes": note, "short name": shortName, "upper-case name": upper, "fresh temp": freshTemp, "directory": dir, "symlink": link, "symlink target": outside} {
		if !exists(path) {
			t.Errorf("%s must be left alone", name)
		}
	}
	if exists(staleTemp) || result.Temp != 1 {
		t.Fatalf("an abandoned temporary file must go: %+v", result)
	}
}

func TestPruneIsIdempotentAndAMissingDirectoryIsNotAnError(t *testing.T) {
	root := t.TempDir()
	blobFile(t, root, "a", 10, 100*time.Hour)
	blobFile(t, root, "b", 10, time.Hour)
	if _, err := Prune(root, pruneNow, MaxBlobAge, MaxBlobBytes); err != nil {
		t.Fatal(err)
	}
	second, err := Prune(root, pruneNow, MaxBlobAge, MaxBlobBytes)
	if err != nil || second.Expired != 0 || second.OverCap != 0 || second.Kept != 1 {
		t.Fatalf("second run: %+v %v", second, err)
	}
	if result, err := Prune(filepath.Join(root, "absent"), pruneNow, MaxBlobAge, MaxBlobBytes); err != nil || result != (PruneResult{}) {
		t.Fatalf("missing directory: %+v %v", result, err)
	}
}

func TestPruneReportsEveryFailureAndFinishesOnTheNextRun(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("a read-only directory does not stop root")
	}
	root := t.TempDir()
	first := blobFile(t, root, "a", 10, 100*time.Hour)
	second := blobFile(t, root, "b", 10, 90*time.Hour)
	fresh := blobFile(t, root, "c", 10, time.Hour)
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o700)
	result, err := Prune(root, pruneNow, MaxBlobAge, MaxBlobBytes)
	if err == nil || !strings.Contains(err.Error(), filepath.Base(first)) || !strings.Contains(err.Error(), filepath.Base(second)) {
		t.Fatalf("both failures must be reported, got %v", err)
	}
	if result.Expired != 0 || !exists(first) || !exists(second) || !exists(fresh) {
		t.Fatalf("nothing may be counted as removed when removal failed: %+v", result)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err = Prune(root, pruneNow, MaxBlobAge, MaxBlobBytes)
	if err != nil || result.Expired != 2 || exists(first) || exists(second) || !exists(fresh) {
		t.Fatalf("the retry must finish the job: %+v %v", result, err)
	}
}

func TestTouchRefreshesAnOldBlobOnlyAndNeverFollowsLinks(t *testing.T) {
	root := t.TempDir()
	old := blobFile(t, root, "a", 10, 47*time.Hour)
	if err := Touch(root, filepath.Base(old), pruneNow); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(old); !info.ModTime().Equal(pruneNow) {
		t.Fatalf("old blob not refreshed: %v", info.ModTime())
	}
	recent := blobFile(t, root, "b", 10, 10*time.Minute)
	before, _ := os.Stat(recent)
	if err := Touch(root, filepath.Base(recent), pruneNow); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(recent); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("a recently used blob must not be rewritten on every redraw")
	}
	if err := Touch(root, strings.Repeat("f", 64)+".png", pruneNow); err != nil {
		t.Fatalf("a pruned or unknown blob is not an error: %v", err)
	}
	for _, bad := range []string{"../x.png", "notes.txt", strings.Repeat("g", 64) + ".png"} {
		if err := Touch(root, bad, pruneNow); err == nil {
			t.Errorf("Touch accepted %q", bad)
		}
	}
	outside := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := pruneNow.Add(-100 * time.Hour)
	if err := os.Chtimes(outside, when, when); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, strings.Repeat("e", 64)+".png")); err != nil {
		t.Fatal(err)
	}
	if err := Touch(root, strings.Repeat("e", 64)+".png", pruneNow); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(outside); !info.ModTime().Equal(when) {
		t.Fatal("Touch followed a symlink")
	}
}
