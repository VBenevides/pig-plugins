package hashline

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

var errReadLimit = errors.New("read byte limit exceeded")

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes < 0 || maxBytes == math.MaxInt64 {
		return nil, fmt.Errorf("invalid read byte limit: %d", maxBytes)
	}
	content, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("%w (%d bytes)", errReadLimit, maxBytes)
	}
	return content, nil
}

// Preliminary path stats are only a fast rejection: the descriptor is checked
// again and actual bytes consumed are limited, including when a file grows.
func readBoundedFile(path string, maxBytes int64) (content []byte, err error) {
	file, err := os.OpenFile(path, os.O_RDONLY|nonBlockingReadFlag, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close read file %s: %w", path, closeErr))
		}
		if err != nil {
			content = nil
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return readBounded(file, maxBytes)
}
