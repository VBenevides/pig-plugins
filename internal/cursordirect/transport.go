package cursordirect

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"time"
)

// openRun owns bounded, ordered client writes and closes them with the response.
// RunSSE/BidiAppend is an explicit alternative, never a generation retry.
func (c *Client) openRun(parent context.Context, token string, run []byte) (*http.Response, func([]byte) error, func(), error) {
	ctx, cancel := context.WithCancelCause(parent)
	reader, writer := io.Pipe()
	outgoing := make(chan []byte, 32)
	done := make(chan struct{})
	requestID := str(1, uuidID(rand.Text()))
	go func() {
		defer close(done)
		defer writer.Close()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		seq := uint64(0)
		for {
			var b []byte
			select {
			case <-ctx.Done():
				return
			case b = <-outgoing:
			case <-ticker.C:
				b = bytesField(7, nil)
			}
			if !c.sse {
				if _, err := writer.Write(frame(b)); err != nil {
					cancel(&safeError{err})
					return
				}
				continue
			}
			appendCtx, appendCancel := context.WithTimeout(ctx, 30*time.Second)
			body := join(bytesField(2, requestID), integer(3, seq), bytesField(4, b))
			seq++
			resp, err := c.request(appendCtx, token, appendPath, "application/proto", bytes.NewReader(body))
			if err == nil {
				_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, maxFrame+1))
				closeErr := resp.Body.Close()
				if err == nil {
					err = closeErr
				}
			}
			appendCancel()
			if err != nil {
				cancel(&safeError{err})
				return
			}
		}
	}()
	closeRun := func() { cancel(context.Canceled); reader.Close(); writer.Close(); <-done }
	send := func(b []byte) error {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		select {
		case outgoing <- b:
			return nil
		case <-done:
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return errors.New("cursor: request writer closed")
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	if err := send(run); err != nil {
		closeRun()
		return nil, nil, nil, err
	}
	path := runPath
	var body io.Reader = reader
	if c.sse {
		path = runSSEPath
		body = bytes.NewReader(frame(requestID))
	}
	resp, err := c.request(ctx, token, path, "application/connect+proto", body)
	if err == nil && !c.sse && resp.ProtoMajor != 2 {
		resp.Body.Close()
		err = errors.New("cursor: direct Run requires HTTP/2; select PIG_CURSOR_TRANSPORT=sse before retrying manually")
	}
	if err != nil {
		closeRun()
		return nil, nil, nil, err
	}
	return resp, send, closeRun, nil
}
