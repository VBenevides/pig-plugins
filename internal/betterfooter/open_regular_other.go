//go:build !unix && !windows

package betterfooter

import (
	"errors"
	"os"
)

// Do not fall back to a potentially blocking open on platforms without a safe
// regular-file opener.
func openRegularFile(path string) (*os.File, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return nil, &os.PathError{Op: "open", Path: path, Err: errors.ErrUnsupported}
}
