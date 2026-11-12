package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryHashesUnicodeAndToolChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stdout.jsonl")
	data := `{"type":"message_start","message":{"role":"system","sections":{"a":"ignored"}}}
{"type":"message_end","message":{"role":"system","sections":{"a":"é文"},"toolsAdded":[{"name":"read"},{"name":"edit"}]}}
{"type":"message_end","message":{"role":"system","sections":{"a":"é文"},"toolsRemoved":["edit"],"toolsAdded":[{"name":"todo"}]}}
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := inventory(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != digest([]byte(data)) || len(got.Systems) != 2 || got.Systems[0].Characters != 2 {
		t.Fatalf("inventory=%+v", got)
	}
	if strings.Join(got.Systems[1].ActiveTools, ",") != "read,todo" {
		t.Fatalf("tools=%v", got.Systems[1].ActiveTools)
	}
	var first, second bytes.Buffer
	if err := run(root, &first); err != nil {
		t.Fatal(err)
	}
	if err := run(root, &second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("manifest is nondeterministic")
	}
}

func TestInventoryReportsMalformedTrace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stdout.jsonl")
	if err := os.WriteFile(path, []byte("{}\n{\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := inventory(path, root)
	if err == nil || !strings.Contains(err.Error(), "stdout.jsonl:2") {
		t.Fatalf("error=%v", err)
	}
}

func TestRunRejectsMissingTraces(t *testing.T) {
	var out bytes.Buffer
	if err := run(t.TempDir(), &out); err == nil {
		t.Fatal("missing traces accepted")
	}
}
