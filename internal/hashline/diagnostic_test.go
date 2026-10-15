package hashline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsNeverReflectMalformedPayloads(t *testing.T) {
	secret := "synthetic-credential-secret"
	dir := realTempDir(t)
	writeFile(t, filepath.Join(dir, "f.txt"), "synthetic-source-secret\n", 0600)
	for _, value := range []any{secret, strings.Repeat(secret, 10000), map[string]any{secret: secret}, []any{secret}} {
		_, err := Read(dir, map[string]any{"path": "f.txt", "offset": value})
		if err == nil || strings.Contains(err.Error(), "secret") || len(err.Error()) > 512 {
			t.Fatalf("read diagnostic leaked: %v", err)
		}
		_, err = Edit(dir, map[string]any{"path": "f.txt", "version": ContentVersion([]byte("synthetic-source-secret\n")), "edits": []any{map[string]any{"op": "replace", "anchor": value, "lines": []any{secret}}}})
		if err == nil || strings.Contains(err.Error(), "secret") || len(err.Error()) > 512 {
			t.Fatalf("edit diagnostic leaked: %v", err)
		}
	}
	engine := NewEngine()
	_, err := engine.WriteContext(context.Background(), dir, map[string]any{"path": "f.txt", "content": secret + "\x00"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("write diagnostic leaked: %v", err)
	}
}
