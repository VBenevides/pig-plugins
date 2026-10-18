package rtk

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=[A-Za-z0-9_./:=+-]*$`)
var passedTest = regexp.MustCompile(`^\s*--- PASS: [^\r\n]+$`)
var packageProgress = regexp.MustCompile(`^[a-z][a-z0-9_./-]*$`)

// Route accepts only a simple unquoted command, optionally preceded by literal
// environment assignments. Complex shell syntax deliberately has no reducer.
// This parser neither interprets nor executes any part of the command.
func Route(command string) (route string, bypass bool) {
	if strings.ContainsAny(command, "\"'`$;&|()<>\\\n\r") {
		return "", false
	}
	args := strings.Fields(command)
	for len(args) > 0 && assignment.MatchString(args[0]) {
		if args[0] == "PI_RTK_BYPASS=1" {
			bypass = true
		}
		args = args[1:]
	}
	if bypass || len(args) < 2 || args[0] != "go" {
		return "", bypass
	}
	switch args[1] {
	case "test":
		for _, arg := range args[2:] {
			if arg == "-json" || arg == "-json=true" {
				return "go-test-json", false
			}
		}
		return "go-test", false
	case "build":
		for _, arg := range args[2:] {
			if arg == "-v" || arg == "-v=true" {
				return "go-build", false
			}
		}
	}
	return "", false
}

// Reduce removes only recognized success/progress scaffolding. Diagnostic text,
// failures, skips, and unrecognized text remain visible. Native status is not an
// input or output of this function and must remain unchanged by its caller.
func Reduce(command, raw string) (text, technique string, err error) {
	route, bypass := Route(command)
	if bypass || route == "" {
		return raw, "", nil
	}
	if len(raw) > MaxInputBytes {
		return raw, "", errors.New("captured output exceeds reducer input budget")
	}
	if route == "go-test-json" {
		return reduceJSON(raw)
	}
	var out strings.Builder
	omitted := 0
	for line := range strings.SplitAfterSeq(raw, "\n") {
		clean := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		drop := false
		if route == "go-test" {
			drop = strings.HasPrefix(clean, "=== RUN   ") || strings.HasPrefix(clean, "=== PAUSE ") || strings.HasPrefix(clean, "=== CONT  ") || passedTest.MatchString(clean) || clean == "PASS"
		} else {
			drop = packageProgress.MatchString(clean)
		}
		if drop {
			omitted++
			continue
		}
		out.WriteString(line)
	}
	if omitted == 0 {
		return raw, "", nil
	}
	fmt.Fprintf(&out, "\n[RTK: omitted %d Go success/progress lines; diagnostics retained]\n", omitted)
	return out.String(), route, nil
}

func reduceJSON(raw string) (string, string, error) {
	var out strings.Builder
	omitted := 0
	for line := range strings.SplitSeq(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct{ Action, Package, Test, Output string }
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Native Bash may append an exit/truncation notice after JSON output.
			// Preserve the entire capture when it is not a complete JSON event stream.
			return raw, "", fmt.Errorf("Go test JSON stream is incomplete or malformed; retaining host output: %w", err)
		}
		switch event.Action {
		case "output":
			out.WriteString(event.Output)
		case "start", "run", "pause", "cont", "bench":
			omitted++
		case "pass":
			if event.Test == "" {
				fmt.Fprintf(&out, "[Go package pass: %s]\n", event.Package)
			} else {
				omitted++
			}
		case "fail", "skip":
			fmt.Fprintf(&out, "[Go %s: %s %s]\n", event.Action, event.Package, event.Test)
		default:
			return raw, "", errors.New("unrecognized Go test JSON action; retaining host output")
		}
	}
	// Keep all Output payloads (including logs and panic/race diagnostics) verbatim.
	// Only event envelopes and redundant successful lifecycle records are removed.
	fmt.Fprintf(&out, "\n[RTK: decoded Go test JSON; omitted %d lifecycle events]\n", omitted)
	return out.String(), "go-test-json", nil
}
