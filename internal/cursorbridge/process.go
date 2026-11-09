package cursorbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Process owns an isolated bridge and its temporary agent state. No account or
// conversation may share a Process. Close must be called after a successful Start.
type Process struct {
	Client   *Client
	cmd      *exec.Cmd
	done     chan struct{}
	root     string
	turn     sync.Mutex
	parkedMu sync.Mutex
	parked   *Session // run waiting on tool results, if any
	once     sync.Once
	closeErr error
}

type Ready struct {
	SchemaVersion int    `json:"schemaVersion"`
	Transport     string `json:"transport"`
	Protocol      string `json:"protocol"`
	URL           string `json:"url"`
	AuthTokenFile string `json:"authTokenFile"`
}

func Start(ctx context.Context, binary, key string) (*Process, error) {
	return start(ctx, binary, key, 30*time.Second)
}

func start(ctx context.Context, binary, key string, timeout time.Duration) (*Process, error) {
	if binary == "" {
		return nil, errors.New("Cursor bridge is missing; run pig-plugins --install-cursor")
	}
	root, err := os.MkdirTemp("", "pig-cursor-bridge-*")
	if err != nil {
		return nil, fmt.Errorf("create Cursor state directory: %w", err)
	}
	cmd := exec.Command(binary, "--host", "127.0.0.1", "--port", "0", "--workspace", root, "--state-root", filepath.Join(root, "state"), "--max-concurrent-agents", "1", "--max-message-bytes", "8388608")
	cmd.Dir = root
	// Do not inherit Cursor, Node, proxy, or bridge option overrides. In particular,
	// NODE_TLS_REJECT_UNAUTHORIZED must never disable upstream certificate checks.
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "SYSTEMROOT", "WINDIR", "SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS"} {
		if value, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "CURSOR_API_KEY="+key, "CURSOR_SDK_CLIENT_LANGUAGE=go")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		os.RemoveAll(root)
		return nil, fmt.Errorf("open Cursor handshake pipe: %w", err)
	}
	if err = cmd.Start(); err != nil {
		stderr.Close()
		os.RemoveAll(root)
		return nil, fmt.Errorf("start Cursor bridge: %w", err)
	}
	p := &Process{cmd: cmd, done: make(chan struct{}), root: root}
	go func() { _ = cmd.Wait(); close(p.done) }()
	ready := make(chan Ready, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			const prefix = "cursor-sdk-bridge ready "
			line := scanner.Text()
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			var message Ready
			if json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &message) != nil {
				continue
			}
			select {
			case ready <- message:
			default:
			}
		}
		// Continue draining diagnostics if a malformed line exceeded the scan limit.
		_, _ = io.Copy(io.Discard, stderr)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	fail := func(cause error) (*Process, error) { return nil, errors.Join(cause, p.Close()) }
	select {
	case message := <-ready:
		if message.SchemaVersion != 1 || message.Transport != "tcp" || message.Protocol != "connect" {
			return fail(errors.New("unsupported Cursor bridge discovery schema"))
		}
		info, err := os.Lstat(message.AuthTokenFile)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 || info.Mode().Perm()&0077 != 0 {
			return fail(errors.New("Cursor bridge token file is missing or not private"))
		}
		data, err := os.ReadFile(message.AuthTokenFile)
		if err != nil {
			return fail(errors.New("cannot read Cursor bridge token file"))
		}
		p.Client, err = NewClient(message.URL, strings.TrimSpace(string(data)))
		if err != nil {
			return fail(err)
		}
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if err = p.Client.Check(checkCtx); err != nil {
			return fail(err)
		}
		return p, nil
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-timer.C:
		return fail(errors.New("Cursor bridge startup timed out"))
	case <-p.done:
		return fail(errors.New("Cursor bridge exited before its handshake"))
	}
}

// releaseParked stops a parked run's listener and request. The bridge shutdown
// that follows ends the run itself, so no agent calls are made here.
func (p *Process) releaseParked() {
	if parked := p.takeParked(nil); parked != nil {
		parked.mu.Lock()
		if !parked.closed {
			parked.closed = true
			close(parked.gone)
		}
		parked.mu.Unlock()
		parked.cancelRun()
		if err := parked.server.Close(); err != nil {
			p.closeErr = errors.Join(p.closeErr, fmt.Errorf("stop Cursor tool callback server: %w", err))
		}
	}
}

func (p *Process) Close() error {
	p.once.Do(func() {
		p.releaseParked()
		if p.Client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			var output struct{}
			_ = p.Client.Unary(ctx, control+"Shutdown", map[string]int{"graceSeconds": 0}, &output)
			cancel()
			p.Client.Close()
		}
		select {
		case <-p.done:
		default:
			if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				p.closeErr = fmt.Errorf("stop Cursor bridge: %w", err)
			}
			<-p.done
		}
		p.closeErr = errors.Join(p.closeErr, os.RemoveAll(p.root))
	})
	return p.closeErr
}
