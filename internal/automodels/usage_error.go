package automodels

// UsageError identifies an optional quota lookup failure, not a generation or
// model-switching failure. Callers can keep it in diagnostics without a popup.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// UsageFailure preserves the cause and text while marking quota diagnostics.
func UsageFailure(err error) error {
	if err == nil {
		return nil
	}
	return &UsageError{Err: err}
}
