package rtk

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestConservativeRoutes(t *testing.T) {
	for command, want := range map[string]string{
		"go test ./...": "go-test", "GOMAXPROCS=2 go test -json ./...": "go-test-json", "go build -v ./...": "go-build",
		"go build ./...": "", "go test ./... | tee out": "", "go test; echo done": "", "cd src && go test": "", "go test $(echo .)": "", "go test './...'": "", "echo go test": "", "PI_RTK_BYPASS=1 go test": "", "go test\ngo build": "", "env go test": "",
	} {
		got, _ := Route(command)
		if got != want {
			t.Errorf("%q = %q want %q", command, got, want)
		}
	}
}

func TestGoTestDiagnosticsRemainVisible(t *testing.T) {
	raw := strings.Repeat("=== RUN   TestOK\n--- PASS: TestOK (0.00s)\n", 30) + "--- SKIP: TestSkip (0.00s)\n--- FAIL: TestBad (0.00s)\n    bad_test.go:9: mismatch\nWARNING: DATA RACE\npanic: broken\n\x1b[31mFAIL\x1b[0m\nok\texample/good\t0.1s\nFAIL\texample/bad\t0.2s\n"
	reduced, kind, err := Reduce("go test -v ./...", raw)
	if err != nil || kind != "go-test" || len(reduced) >= len(raw) {
		t.Fatalf("%s %v", kind, err)
	}
	for _, want := range []string{"--- SKIP:", "--- FAIL:", "bad_test.go:9:", "WARNING: DATA RACE", "panic: broken", "\x1b[31mFAIL\x1b[0m", "example/good", "example/bad"} {
		if !strings.Contains(reduced, want) {
			t.Errorf("lost %q", want)
		}
	}
}

func TestGoJSONPreservesPayloadsAndFailures(t *testing.T) {
	var raw strings.Builder
	for range 50 {
		data, err := json.Marshal(map[string]any{"Action": "run", "Package": "example", "Test": "TestOK"})
		if err != nil {
			t.Fatal(err)
		}
		raw.Write(data)
		raw.WriteByte('\n')
	}
	for _, event := range []map[string]any{
		{"Action": "output", "Output": "panic: broken\nWARNING: DATA RACE\n", "Package": "bad"},
		{"Action": "fail", "Test": "TestBad", "Package": "bad"}, {"Action": "skip", "Package": "skip"}, {"Action": "pass", "Package": "good"},
	} {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		raw.Write(data)
		raw.WriteByte('\n')
	}
	reduced, kind, err := Reduce("go test -json ./...", raw.String())
	if err != nil || kind != "go-test-json" || len(reduced) >= raw.Len() {
		t.Fatalf("%s %v", kind, err)
	}
	for _, want := range []string{"panic: broken\nWARNING: DATA RACE\n", "Go fail: bad TestBad", "Go skip: skip", "Go package pass: good"} {
		if !strings.Contains(reduced, want) {
			t.Error(want)
		}
	}
	for _, bad := range []string{raw.String() + "exit code 1\n", "{\"Action\":\"future-action\"}\n", "{invalid"} {
		text, _, err := Reduce("go test -json", bad)
		if err == nil || text != bad {
			t.Fatal("malformed stream was discarded")
		}
	}
}

func TestBuildOnlyReducesProgress(t *testing.T) {
	raw := strings.Repeat("example.org/pkg\n", 100) + "# example.org/bad\nbad.go:7: undefined: missing\n"
	reduced, kind, err := Reduce("go build -v ./...", raw)
	if err != nil || kind != "go-build" || !strings.Contains(reduced, "# example.org/bad\nbad.go:7: undefined: missing\n") {
		t.Fatalf("%s %v %s", kind, err, reduced)
	}
	for _, command := range []string{"go build ./...", "go test ./... | tee output", "unknown"} {
		text, kind, err := Reduce(command, raw)
		if text != raw || kind != "" || err != nil {
			t.Fatal("ambiguous command changed")
		}
	}
}

func TestResultHookPreservesStatusAndStructuredOutput(t *testing.T) {
	r := recovery(t)
	raw := strings.Repeat("=== RUN   TestOK\n--- PASS: TestOK (0.00s)\n", 100) + "ok\texample\t0.1s\n"
	event := map[string]any{"toolName": "bash", "input": map[string]any{"command": "go test -v"}, "isError": false, "details": map[string]any{"fullOutputPath": "/host/raw", "exitCode": 0}, "structuredContent": map[string]any{"output": raw, "exit_code": 0}, "content": []any{map[string]any{"type": "text", "text": raw}}}
	before, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	result := ProcessResult(context.Background(), r, event, true)
	if result == nil {
		t.Fatal("no reduction")
	}
	if _, ok := result["isError"]; ok {
		t.Fatal("overrode failure state")
	}
	if _, ok := result["structuredContent"]; ok {
		t.Fatal("overrode native script value")
	}
	details := result["details"].(map[string]any)
	if details["fullOutputPath"] != "/host/raw" || details["exitCode"] != 0 {
		t.Fatal(details)
	}
	meta := details["rtkCompaction"].(map[string]any)
	data, err := os.ReadFile(meta["rawOutputPath"].(string))
	if err != nil || string(data) != raw {
		t.Fatal("raw capture lost", err)
	}
	after, err := json.Marshal(event)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("mutated original event")
	}
	if ProcessResult(context.Background(), r, event, false) != nil {
		t.Fatal("not opt-in")
	}
	for _, name := range []string{"read", "edit", "write"} {
		event["toolName"] = name
		if ProcessResult(context.Background(), r, event, true) != nil {
			t.Fatal("touched", name)
		}
	}
	event["toolName"] = "bash"
	event["isError"] = true
	if ProcessResult(context.Background(), r, event, true) != nil {
		t.Fatal("modified failed/permission result")
	}
	event["isError"] = false
	event["input"] = map[string]any{"command": "PI_RTK_BYPASS=1 go test -v"}
	if ProcessResult(context.Background(), r, event, true) != nil {
		t.Fatal("bypass ignored")
	}
}

func TestRecoveryFailureIsVisibleWithoutChangingExecutionStatus(t *testing.T) {
	r := recovery(t)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	raw := strings.Repeat("=== RUN   TestOK\n--- PASS: TestOK (0.00s)\n", 100)
	event := map[string]any{"toolName": "bash", "isError": false, "input": map[string]any{"command": "go test -v"}, "content": []map[string]any{{"type": "text", "text": raw}}}
	result := ProcessResult(context.Background(), r, event, true)
	text := result["content"].([]map[string]any)[0]["text"].(string)
	if !strings.HasPrefix(text, raw) || !strings.Contains(text, "recovery is closed") || result["isError"] != nil {
		t.Fatal("failure hidden or status modified")
	}
}
