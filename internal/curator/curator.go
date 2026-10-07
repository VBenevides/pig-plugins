// Package curator is the Go side of the pi-curator extension. The journal, redaction, locking and search live in
// the separate `curator` program, whose packages are internal to its own module, so this package runs that program
// and keeps the same wire format. Existing .curator data stays readable because nothing here writes it directly.
package curator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultTimeout bounds one curator call.
	DefaultTimeout = 15 * time.Second
	// MaxOutput bounds the bytes read from stdout and stderr together.
	MaxOutput = 1 << 20
)

// Options say how to run curator.
type Options struct {
	// Bin is the program; empty means PI_CURATOR_BIN from Getenv, then `curator` on PATH.
	Bin string
	// Getenv reads PI_CURATOR_BIN; nil means no variable.
	Getenv func(string) string
	// Cwd is passed as --cwd.
	Cwd string
	// BoundRoot requires Cwd to remain the canonical repository root, not an ancestor.
	BoundRoot string
	// Timeout defaults to DefaultTimeout.
	Timeout time.Duration
}

func (o Options) bin() string {
	if o.Bin != "" {
		return o.Bin
	}
	if o.Getenv != nil {
		if bin := o.Getenv("PI_CURATOR_BIN"); bin != "" {
			return bin
		}
	}
	return "curator"
}

// Run is the outcome of one curator call that ran to completion.
type Run struct {
	Code   int
	Stdout string
	Stderr string
}

// limited collects output and refuses to hold more than MaxOutput bytes in total across the writers that share it.
type limited struct {
	shared *outputBudget
	buf    bytes.Buffer
}

type outputBudget struct {
	mu       sync.Mutex
	used     int
	exceeded bool
	cancel   context.CancelFunc
}

func (l *limited) Write(p []byte) (int, error) {
	l.shared.mu.Lock()
	defer l.shared.mu.Unlock()
	if l.shared.exceeded {
		return 0, errors.New("output limit exceeded")
	}
	if len(p) > MaxOutput-l.shared.used {
		l.shared.exceeded = true
		l.shared.cancel()
		return 0, errors.New("output limit exceeded")
	}
	l.shared.used += len(p)
	return l.buf.Write(p)
}

// run runs curator with a hard timeout and bounded output. A non-zero exit is a result, not an error. Errors never
// reflect the child's output.
func run(ctx context.Context, o Options, args []string, stdin string) (Run, error) {
	if len(args) == 0 {
		return Run{}, errors.New("curator command is required")
	}
	if o.BoundRoot != "" {
		root, err := filepath.EvalSymlinks(o.Cwd)
		if err != nil || root != o.BoundRoot {
			return Run{}, errors.New("repository changed; memory access and capture denied")
		}
		if _, err := os.Lstat(filepath.Join(root, ".git")); err != nil {
			return Run{}, errors.New("repository changed; memory access and capture denied")
		}
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	budget := &outputBudget{cancel: cancel}
	stdout, stderr := &limited{shared: budget}, &limited{shared: budget}

	cmd := exec.CommandContext(callCtx, o.bin(), slices.Concat([]string{args[0], "--cwd", o.Cwd}, args[1:])...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()

	budget.mu.Lock()
	exceeded := budget.exceeded
	budget.mu.Unlock()
	switch {
	case exceeded:
		return Run{}, errors.New("curator output limit exceeded")
	case ctx.Err() != nil:
		return Run{}, fmt.Errorf("curator %s cancelled: %w", args[0], ctx.Err())
	case errors.Is(callCtx.Err(), context.DeadlineExceeded):
		return Run{}, fmt.Errorf("curator %s timed out after %s", args[0], timeout)
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
		return Run{Stdout: stdout.buf.String(), Stderr: stderr.buf.String()}, nil
	case errors.As(err, &exit):
		return Run{Code: exit.ExitCode(), Stdout: stdout.buf.String(), Stderr: stderr.buf.String()}, nil
	default:
		return Run{}, fmt.Errorf("cannot run curator (%s): %w", o.bin(), err)
	}
}

// Status is the repository onboarding state reported by `curator status`.
type Status struct {
	State   string `json:"state"`
	Root    string `json:"root"`
	Store   string `json:"store"`
	RepoID  string `json:"repo_id"`
	Message string `json:"message"`
}

// Repository states.
const (
	StatePendingConsent = "pending_consent"
	StateReady          = "ready"
)

func decodeStatus(out Run, what string) (Status, error) {
	if out.Code != 0 {
		return Status{}, fmt.Errorf("curator %s failed (exit %d)", what, out.Code)
	}
	var status Status
	if err := json.Unmarshal([]byte(out.Stdout), &status); err != nil {
		return Status{}, errors.New("curator produced an invalid JSON response")
	}
	return status, nil
}

// RepoStatus reports the onboarding state of the repository at o.Cwd.
func RepoStatus(ctx context.Context, o Options) (Status, error) {
	out, err := run(ctx, o, []string{"status"}, "")
	if err != nil {
		return Status{}, err
	}
	return decodeStatus(out, "status")
}

// InitRepo creates the repository memory. The caller must have the user's explicit consent first.
func InitRepo(ctx context.Context, o Options) (Status, error) {
	out, err := run(ctx, o, []string{"init", "--consent"}, "")
	if err != nil {
		return Status{}, err
	}
	return decodeStatus(out, "init")
}

// RecentDecisions returns the newest user decisions in the journal, oldest first, as `YYYY-MM-DD: snippet` lines.
// It is only used when the operator opts in; the text is stored history and must be presented as untrusted data.
func RecentDecisions(ctx context.Context, o Options, count int) ([]string, error) {
	out, err := run(ctx, o, []string{"search", "--decisions", "--limit", fmt.Sprint(count), "--snippet", "300"}, "")
	if err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("curator search failed (exit %d)", out.Code)
	}
	var parsed struct {
		Sessions []struct {
			Hits []struct {
				CreatedAt string `json:"created_at_utc"`
				Snippet   string `json:"snippet"`
			} `json:"hits"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(out.Stdout), &parsed); err != nil {
		return nil, errors.New("curator produced an invalid JSON response")
	}
	type hit struct{ at, snippet string }
	var hits []hit
	for _, session := range parsed.Sessions {
		for _, h := range session.Hits {
			hits = append(hits, hit{h.CreatedAt, h.Snippet})
		}
	}
	// Curator returns newest first; reversing before the stable sort keeps that order when timestamps tie.
	slices.Reverse(hits)
	slices.SortStableFunc(hits, func(a, b hit) int { return strings.Compare(a.at, b.at) })
	lines := make([]string, len(hits))
	for i, h := range hits {
		day := h.at
		if len(day) > 10 {
			day = day[:10]
		}
		lines[i] = day + ": " + h.snippet
	}
	return lines, nil
}

// MemoryCommand runs a read-only memory command and returns its trimmed stdout. A non-zero exit is an error the
// agent can see.
func MemoryCommand(ctx context.Context, o Options, args []string) (string, error) {
	out, err := run(ctx, o, args, "")
	if err != nil {
		return "", err
	}
	if out.Code != 0 {
		return "", fmt.Errorf("curator %s failed (exit %d)", args[0], out.Code)
	}
	return strings.TrimSpace(out.Stdout), nil
}

// IngestTransport sends each request to `curator ingest`.
func IngestTransport(o Options) Transport {
	return func(ctx context.Context, request IngestRequest) (IngestResponse, error) {
		payload, err := json.Marshal(request)
		if err != nil {
			return IngestResponse{}, fmt.Errorf("encode the ingest request: %w", err)
		}
		out, err := run(ctx, o, []string{"ingest"}, string(payload))
		if err != nil {
			return IngestResponse{}, err
		}
		var parsed struct {
			Results  *[]ItemResult `json:"results"`
			Gaps     []ItemResult  `json:"gaps"`
			Warnings []string      `json:"journal_warnings"`
		}
		if err := json.Unmarshal([]byte(out.Stdout), &parsed); err != nil {
			return IngestResponse{}, fmt.Errorf("curator ingest produced no JSON response (exit %d)", out.Code)
		}
		if parsed.Results == nil {
			return IngestResponse{}, fmt.Errorf("curator ingest returned an unexpected response (exit %d)", out.Code)
		}
		return IngestResponse{Results: *parsed.Results, Gaps: parsed.Gaps, Warnings: parsed.Warnings}, nil
	}
}

// Status text for `/pi-curator status`: the raw status line, or why curator is unavailable.
func StatusLine(ctx context.Context, o Options) string {
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}
	out, err := run(ctx, o, []string{"status"}, "")
	if err != nil {
		return "repository memory: unavailable (" + err.Error() + ")"
	}
	if out.Code != 0 {
		message := fmt.Sprintf("curator status failed (exit %d)", out.Code)
		if text := strings.TrimSpace(out.Stderr); text != "" {
			message += "; " + truncate(text, 200)
		}
		return "repository memory: unavailable (" + message + ")"
	}
	return "repository memory: " + strings.Join(strings.Fields(out.Stdout), " ")
}

func truncate(text string, limit int) string {
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}
