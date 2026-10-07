// Package lsp implements bounded, read-only stdio language-server access.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxMessage = 32 << 20

// RPC owns framing and request lifetimes. Its reader must close to stop the receive loop.
type RPC struct {
	reader    io.ReadCloser
	writer    io.WriteCloser
	writeMu   sync.Mutex
	mu        sync.Mutex
	pending   map[int64]chan response
	next      atomic.Int64
	done      chan struct{}
	closeOnce sync.Once
	failure   error
	Handle    func(string, json.RawMessage) (any, error)
	Notify    func(string, json.RawMessage)
}
type response struct {
	value json.RawMessage
	err   error
}

func NewRPC(reader io.ReadCloser, writer io.WriteCloser) *RPC {
	r := &RPC{reader: reader, writer: writer, pending: map[int64]chan response{}, done: make(chan struct{})}
	return r
}
func (r *RPC) Start() { go r.readLoop() }
func (r *RPC) Close(err error) {
	r.closeOnce.Do(func() {
		if err == nil {
			err = errors.New("LSP connection closed")
		}
		r.mu.Lock()
		r.failure = err
		close(r.done)
		r.mu.Unlock()
		r.reader.Close()
		r.writer.Close()
	})
}
func (r *RPC) Alive() bool {
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}
func (r *RPC) write(ctx context.Context, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxMessage {
		return errors.New("LSP message exceeds 32 MiB")
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if !r.Alive() {
		return r.failure
	}
	// Closing the pipe interrupts a blocked write. A cancelled send cannot leave a partial frame usable.
	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			select {
			case <-finished:
				return
			default:
				r.Close(ctx.Err())
			}
		case <-finished:
		}
	}()
	defer close(finished)
	if _, err = fmt.Fprintf(r.writer, "Content-Length: %d\r\n\r\n", len(data)); err == nil {
		_, err = r.writer.Write(data)
	}
	if err != nil {
		r.Close(err)
	}
	return err
}
func (r *RPC) Send(ctx context.Context, method string, params any) error {
	return r.write(ctx, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (r *RPC) Request(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	id := r.next.Add(1)
	ch := make(chan response, 1)
	r.mu.Lock()
	if !r.Alive() {
		err := r.failure
		r.mu.Unlock()
		return nil, err
	}
	r.pending[id] = ch
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }()
	if err := r.write(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case result := <-ch:
		return result.value, result.err
	case <-r.done:
		return nil, r.failure
	case <-ctx.Done():
		cancelCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_ = r.Send(cancelCtx, "$/cancelRequest", map[string]any{"id": id})
		return nil, fmt.Errorf("LSP %s: %w", method, ctx.Err())
	}
}
func (r *RPC) readLoop() {
	reader := bufio.NewReader(r.reader)
	for {
		length := -1
		header := 0
		for {
			rawLine, err := reader.ReadSlice('\n')
			if err != nil {
				r.Close(fmt.Errorf("LSP framing: %w", err))
				return
			}
			header += len(rawLine)
			if header > 4096 {
				r.Close(errors.New("LSP header exceeds 4 KiB"))
				return
			}
			line := strings.TrimSpace(string(rawLine))
			if line == "" {
				break
			}
			key, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(key, "Content-Length") {
				n, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil || n < 0 || n > maxMessage || length >= 0 {
					r.Close(errors.New("invalid LSP Content-Length"))
					return
				}
				length = n
			}
		}
		if length < 0 {
			r.Close(errors.New("missing LSP Content-Length"))
			return
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			r.Close(err)
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int
				Message string
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			r.Close(fmt.Errorf("invalid LSP JSON: %w", err))
			return
		}
		if msg.Method != "" {
			if len(msg.ID) == 0 {
				if r.Notify != nil {
					r.Notify(msg.Method, msg.Params)
				}
				continue
			}
			var value any
			var err error
			if r.Handle != nil {
				value, err = r.Handle(msg.Method, msg.Params)
			} else {
				err = errors.New("unsupported server request")
			}
			reply := map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": value}
			if err != nil {
				delete(reply, "result")
				reply["error"] = map[string]any{"code": -32601, "message": err.Error()}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			err = r.write(ctx, reply)
			cancel()
			if err != nil {
				return
			}
			continue
		}
		var id int64
		if json.Unmarshal(msg.ID, &id) != nil {
			continue
		}
		r.mu.Lock()
		ch := r.pending[id]
		r.mu.Unlock()
		if ch != nil {
			res := response{value: msg.Result}
			if msg.Error != nil {
				res.err = fmt.Errorf("LSP error %d: %s", msg.Error.Code, msg.Error.Message)
			}
			select {
			case ch <- res:
			default:
			}
		}
	}
}
