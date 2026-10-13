package hashline

import (
	"errors"
	"fmt"
	"strings"
)

// RejectionContext adds bounded anchors only, never source or replacement text.
// These hints neither refresh read authority nor trigger relocation/retry.
type RejectionContext struct {
	Err     error
	Anchors []string
}

func (rejection *RejectionContext) Error() string {
	return fmt.Sprintf("%v Nearby current anchors (no source displayed): %s. Explicitly read this range again before editing; these hints do not authorize edits.", rejection.Err, strings.Join(rejection.Anchors, ", "))
}
func (rejection *RejectionContext) Unwrap() error { return rejection.Err }

func staleRejectionContext(request Cancellation, content []byte, edits any, primary error) error {
	if ErrorCategoryOf(primary) != CategoryStale {
		return primary
	}
	operations, ok := edits.([]any)
	if !ok || len(operations) == 0 {
		return primary
	}
	operation, ok := operations[0].(map[string]any)
	if !ok {
		return primary
	}
	anchor, err := parseAnchor(operation["anchor"], "anchor")
	if err != nil {
		return primary
	}
	file, err := ParseFileContext(request, string(content))
	if err != nil {
		return errors.Join(primary, err)
	}
	if len(file.Lines) == 0 {
		return primary
	}
	center := min(anchor.line, len(file.Lines))
	hints := make([]string, 0, 5)
	for line := max(1, center-2); line <= min(len(file.Lines), center+2); line++ {
		hints = append(hints, FormatAnchor(line, file.Lines[line-1].Text))
	}
	return &RejectionContext{Err: primary, Anchors: hints}
}
