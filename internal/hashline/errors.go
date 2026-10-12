package hashline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

type ErrorCategory string

const (
	CategoryInvalid    ErrorCategory = "invalid_request"
	CategoryStale      ErrorCategory = "stale_version"
	CategoryConflict   ErrorCategory = "conflicting_operations"
	CategoryPermission ErrorCategory = "permission_denied"
	CategoryConcurrent ErrorCategory = "concurrent_write"
	CategoryFilesystem ErrorCategory = "filesystem_failure"
	CategoryCancelled  ErrorCategory = "cancelled"
	CategoryCommitted  ErrorCategory = "committed_failure"
)

func categorizedEditError(category ErrorCategory, format string, args ...any) *EditError {
	return &EditError{Message: fmt.Sprintf(format, args...), Category: category}
}

// ErrorCategoryOf classifies wrapped errors without parsing their human text.
// A committed failure takes precedence over cancellation or filesystem causes.
func ErrorCategoryOf(err error) ErrorCategory {
	if err == nil {
		return ""
	}
	var committed *CommittedError
	if errors.As(err, &committed) {
		return CategoryCommitted
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return CategoryCancelled
	}
	if errors.Is(err, fs.ErrPermission) {
		return CategoryPermission
	}
	var rejected *EditError
	if errors.As(err, &rejected) {
		if rejected.Category != "" {
			return rejected.Category
		}
		return CategoryInvalid
	}
	return CategoryFilesystem
}
