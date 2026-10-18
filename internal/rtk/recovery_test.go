package rtk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func recovery(t *testing.T) *Recovery {
	t.Helper()
	r := NewRecovery()
	r.parent = t.TempDir()
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestRecoveryFidelityAndPermissions(t *testing.T) {
	r := recovery(t)
	raw := strings.Repeat("diagnostic\x1b[31m\r\n", 100)
	visible, meta, err := r.Present(context.Background(), raw, "FAIL: diagnostic\n")
	if err != nil || meta == nil {
		t.Fatalf("%v %v", meta, err)
	}
	data, err := os.ReadFile(meta.RawOutputPath)
	if err != nil || string(data) != raw {
		t.Fatalf("capture: %q %v", data, err)
	}
	for path, want := range map[string]os.FileMode{meta.RawOutputPath: 0600, r.dir: 0700} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != want {
			t.Fatalf("permissions %s: %v %v", path, st, err)
		}
	}
	if !strings.Contains(visible, "may already be host-truncated") || !strings.Contains(visible, "FAIL: diagnostic") || meta.OutputBytes != len(visible) {
		t.Fatal(visible)
	}
}

func TestRecoveryFailuresKeepOriginal(t *testing.T) {
	for _, kind := range []string{"cancelled", "closed", "too-large", "unwritable", "visible-budget"} {
		t.Run(kind, func(t *testing.T) {
			r := recovery(t)
			raw := strings.Repeat("FAIL\n", 200)
			reduced := "FAIL\n"
			ctx := context.Background()
			switch kind {
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "closed":
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			case "too-large":
				raw = strings.Repeat("x", MaxInputBytes+1)
			case "unwritable":
				r.parent = filepath.Join(r.parent, "missing", "parent")
			case "visible-budget":
				raw = strings.Repeat("x", MaxVisibleBytes+2000)
				reduced = strings.Repeat("x", MaxVisibleBytes+1)
			}
			visible, meta, err := r.Present(ctx, raw, reduced)
			if err == nil || visible != raw || meta != nil {
				t.Fatalf("lost original: %v %v", meta, err)
			}
			if kind == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestRecoveryRetentionAndCleanupIsolation(t *testing.T) {
	r := recovery(t)
	first, err := r.Save(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	for range maxSnapshots {
		if _, err := r.Save(context.Background(), "next"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest retained: %v", err)
	}
	if len(r.files) != maxSnapshots {
		t.Fatal(len(r.files))
	}
	for range 10 {
		if _, err := r.Save(context.Background(), strings.Repeat("x", MaxInputBytes)); err != nil {
			t.Fatal(err)
		}
	}
	if r.bytes > maxSnapshotBytes || len(r.files) > maxSnapshots {
		t.Fatal("unbounded retention")
	}
	unrelated := filepath.Join(r.parent, "unrelated")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := r.dir
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot directory survived: %v", err)
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatal("unrelated file removed")
	}
}

func TestRecoveryConcurrentSaveAndClose(t *testing.T) {
	r := recovery(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := r.Save(context.Background(), "output")
			if err != nil && !strings.Contains(err.Error(), "closed") {
				t.Error(err)
			}
		})
	}
	wg.Go(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
}

func TestNoReductionDoesNotCreateArtifacts(t *testing.T) {
	r := recovery(t)
	text, meta, err := r.Present(context.Background(), "raw", "longer")
	if err != nil || meta != nil || text != "raw" || r.dir != "" {
		t.Fatalf("%s %v %v", text, meta, err)
	}
}
