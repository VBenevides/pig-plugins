package contextprune

import (
	"os"
	"path/filepath"
	"testing"
)

func TestErrorPolicyDefaultsAndValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	budget, excerpt, err := ErrorPolicy(file)
	if err != nil || budget != DefaultErrorBudgetBytes || excerpt != DefaultErrorExcerptBytes {
		t.Fatalf("wrong missing-file defaults: %d %d %v", budget, excerpt, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("reading defaults created a file")
	}
	for _, text := range []string{
		`{"historicalErrorBytes":4095}`, `{"historicalErrorBytes":16777217}`, `{"historicalErrorBytes":true}`,
		`{"historicalErrorBytes":4096.5}`, `{"errorExcerptBytes":511}`, `{"errorExcerptBytes":16385}`,
		`{"errorExcerptBytes":null}`, `[]`, `{`,
	} {
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ErrorPolicy(file); err == nil {
			t.Fatalf("accepted invalid error policy %s", text)
		}
		if enabled, err := Enabled(file); err == nil || enabled {
			t.Fatalf("invalid policy did not fail closed: %s", text)
		}
	}
	text := `{"enabled":true,"historicalErrorBytes":4096,"errorExcerptBytes":512,"unknownCounter":9007199254740993}`
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	budget, excerpt, err = ErrorPolicy(file)
	if err != nil || budget != 4096 || excerpt != 512 {
		t.Fatalf("valid policy rejected: %d %d %v", budget, excerpt, err)
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != text {
		t.Fatal("policy read changed unrelated settings")
	}
	if err := SetEnabled(file, false); err != nil {
		t.Fatal(err)
	}
	budget, excerpt, err = ErrorPolicy(file)
	if err != nil || budget != 4096 || excerpt != 512 {
		t.Fatal("switch update discarded the policy")
	}
	if _, _, err := ErrorPolicy(t.TempDir()); err == nil {
		t.Fatal("unreadable policy was treated as missing")
	}
}
