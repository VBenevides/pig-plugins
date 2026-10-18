// Package astsearch adapts the optional ast-grep CLI for read-only discovery.
package astsearch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const maxCaptureBytes = 1 << 20

type capture struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *capture) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		remaining := b.limit - b.buffer.Len()
		if remaining > 0 {
			_, _ = b.buffer.Write(p[:remaining])
		}
		b.exceeded = true
		b.cancel()
		return 0, errors.New("ast-grep output budget exceeded")
	}
	return b.buffer.Write(p)
}

type execution struct {
	out, stderr string
	exit        int
	limited     bool
}

func execute(ctx context.Context, binary, dir string, args []string) (execution, error) {
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(request, binary, args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	stdout := &capture{limit: maxCaptureBytes, cancel: cancel}
	stderr := &capture{limit: 16 << 10, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	result := execution{out: stdout.buffer.String(), stderr: stderr.buffer.String(), limited: stdout.exceeded || stderr.exceeded}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if result.limited {
		return result, nil
	}
	if err == nil {
		return result, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.exit = exit.ExitCode()
		return result, nil
	}
	return result, fmt.Errorf("execute ast-grep: %w", err)
}
