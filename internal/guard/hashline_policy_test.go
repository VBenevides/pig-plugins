package guard

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/hashline"
)

func TestGuardAndMutationAgreeOnProtectedAliases(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	protected := filepath.Join(cwd, ".ssh")
	if err := os.Mkdir(protected, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(protected, "existing")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(cwd, "file-alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(protected, filepath.Join(cwd, "directory-alias")); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"file-alias", "@file-alias", "directory-alias/existing", "@directory-alias/new/ordinary"} {
		input := map[string]any{"path": raw, "content": "approved replacement"}
		call := Call{Tool: "write", Input: input, Cwd: cwd}
		if decision := newGate(Strict, false, nil).Check(context.Background(), call); !decision.Block {
			t.Fatalf("protected alias allowed: %s", raw)
		}
		approval := &dialog{answer: true}
		call.HasUI = true
		call.Confirm = approval.confirm
		if decision := newGate(Interactive, false, nil).Check(context.Background(), call); decision.Block || len(approval.asked) != 1 {
			t.Fatalf("approval missing: %s %+v", raw, decision)
		}
		expected := RealTarget(hashline.ResolveToolPath(raw, cwd))
		if _, err := hashline.Write(cwd, input); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(expected)
		if err != nil || string(content) != "approved replacement" {
			t.Fatalf("guard/mutation target mismatch: %s %q %v", expected, content, err)
		}
	}
}
