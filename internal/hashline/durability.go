package hashline

import (
	"errors"
	"fmt"
	"os"
)

// CommittedError means publication succeeded, but a required subsequent
// operation failed. Callers must reread instead of retrying blindly.
type CommittedError struct {
	Path string
	Err  error
}

func (err *CommittedError) Error() string {
	return fmt.Sprintf("%s was committed, but post-commit processing failed; durability may be uncertain. Read it again before retrying: %v", err.Path, err.Err)
}
func (err *CommittedError) Unwrap() error { return err.Err }

func syncDirectory(path string) (err error) {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer func() {
		if closeErr := directory.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close synced directory: %w", closeErr))
		}
	}()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
