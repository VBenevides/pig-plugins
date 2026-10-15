package hashline

import (
	"errors"
	"io"
)

// Cancellation is the request lifetime shared by context.Context and the PiG
// SDK Context. It deliberately does not introduce an SDK dependency here.
type Cancellation interface {
	Done() <-chan struct{}
	Err() error
}

const cancellationChunkBytes = 64 * 1024

type cancellationReader struct {
	request Cancellation
	reader  io.Reader
}

func (reader cancellationReader) Read(buffer []byte) (int, error) {
	if err := reader.request.Err(); err != nil {
		return 0, err
	}
	count, err := reader.reader.Read(buffer[:min(len(buffer), cancellationChunkBytes)])
	if cancelled := reader.request.Err(); cancelled != nil {
		return count, errors.Join(err, cancelled)
	}
	return count, err
}

func writePreparedText(request Cancellation, writer io.StringWriter, content string) error {
	for len(content) > 0 {
		if err := request.Err(); err != nil {
			return err
		}
		part := content[:min(len(content), cancellationChunkBytes)]
		count, err := writer.WriteString(part)
		if err != nil {
			return err
		}
		if count != len(part) {
			return io.ErrShortWrite
		}
		content = content[len(part):]
	}
	return request.Err()
}
