package betterfooter

// CycleDirection infers the host's cycle direction from neighboring models. Two-model
// scopes are intrinsically ambiguous without the host's configured-keybinding matcher.
func CycleDirection(models []ModelRef, previous, selected ModelRef) int {
	before, after := -1, -1
	for i, m := range models {
		if m == previous {
			before = i
		}
		if m == selected {
			after = i
		}
	}
	if before < 0 || after < 0 || len(models) < 3 {
		return 1
	}
	if (after-before+len(models))%len(models) == 1 {
		return 1
	}
	if (before-after+len(models))%len(models) == 1 {
		return -1
	}
	return 1
}
