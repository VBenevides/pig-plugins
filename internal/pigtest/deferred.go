package pigtest

import (
	"path/filepath"
	"runtime"
	"testing"
)

// ActivateDeferredForTest supplies explicit tool activation for execution-only tests.
// Do not load this fixture in discovery or initial-declaration tests.
func ActivateDeferredForTest(t testing.TB) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate deferred-tool test fixture")
	}
	return filepath.Join(filepath.Dir(source), "..", "..", "testfixtures", "activate-deferred")
}
