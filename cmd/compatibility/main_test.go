package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPrintVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "COMPATIBILITY.json")
	if err := os.WriteFile(path, []byte(`{"dependencies":{"pig":{"path":"github.com/MichaelKinsy/PiG","version":"0.4.2"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := printVersion(path, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "0.4.2\n" {
		t.Fatalf("output = %q", output.String())
	}
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := printVersion(path, &output); err == nil {
		t.Fatal("accepted invalid manifest")
	}
	if output.Len() != 0 {
		t.Fatal("invalid manifest produced a version")
	}
	if err := printVersion(path+".missing", &output); err == nil {
		t.Fatal("accepted missing manifest")
	}
}
