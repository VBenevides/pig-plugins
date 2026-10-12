package hashline

import "strings"

// Unknown keys are rejected without reflecting arbitrary keys or values in
// diagnostics. The operation/request label and allowed names identify the fix.
func rejectUnknownFields(object map[string]any, label string, allowed ...string) error {
	for field := range object {
		known := false
		for _, name := range allowed {
			if field == name {
				known = true
				break
			}
		}
		if !known {
			return editErrorf("%s: unknown fields; only %s are allowed.", label, strings.Join(allowed, ", "))
		}
	}
	return nil
}
