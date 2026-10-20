package guard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestApprovalAnswersAndInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		answer  string
		ok      bool
		err     error
		block   bool
		persist bool
	}{
		{"Allow", true, nil, false, false}, {"Deny", true, nil, true, false},
		{"Always allow this command and item", true, nil, false, true},
		{"Allow once", true, nil, true, false}, {"", false, nil, true, false},
		{"Allow", false, nil, true, false}, {"Allow", true, errors.New("UI disconnected"), true, false},
	} {
		t.Run(tc.answer, func(t *testing.T) {
			cwd := t.TempDir()
			list := NewAllowlist(filepath.Join(cwd, "allow.json"))
			gate := NewGate(Settings{Mode: Interactive}, nil)
			gate.UseAllowlist(list)
			call := Call{Tool: "write", Input: map[string]any{"path": filepath.Join(cwd, ".env")}, Cwd: cwd, HasUI: true,
				Select: func(_ string, options []string) (string, bool, error) {
					if !slices.Equal(options, []string{"Allow", "Deny", "Always allow this command and item"}) {
						t.Fatal(options)
					}
					return tc.answer, tc.ok, tc.err
				}}
			got := gate.CheckAccess(context.Background(), call, ModeLax)
			if got.Block != tc.block {
				t.Fatal(got)
			}
			_, err := os.Stat(list.file)
			if (err == nil) != tc.persist {
				t.Fatalf("persist=%v stat=%v", tc.persist, err)
			}
		})
	}
}

func TestUnadvertisedAlwaysAllowAndBrokenGrantStore(t *testing.T) {
	cwd := t.TempDir()
	gate := NewGate(Settings{}, nil)
	call := Call{Tool: "write", Input: map[string]any{"path": filepath.Join(cwd, ".env")}, Cwd: cwd, HasUI: true,
		Select: func(_ string, options []string) (string, bool, error) {
			if !slices.Equal(options, []string{"Allow", "Deny"}) {
				t.Fatal(options)
			}
			return choiceAlways, true, nil
		}}
	if got := gate.CheckAccess(context.Background(), call, ModeLax); !got.Block {
		t.Fatal("unadvertised response allowed")
	}
	bad := filepath.Join(cwd, "bad.json")
	if err := os.WriteFile(bad, []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	gate.UseAllowlist(NewAllowlist(bad))
	call.Select = func(string, []string) (string, bool, error) { return choiceAlways, true, nil }
	if got := gate.CheckAccess(context.Background(), call, ModeLax); got.Block {
		t.Fatal("explicit approval should still allow once", got)
	}
	data, err := os.ReadFile(bad)
	if err != nil || string(data) != "not JSON" {
		t.Fatalf("damaged grant file overwritten: %q %v", data, err)
	}
}
