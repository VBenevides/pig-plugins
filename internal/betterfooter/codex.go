package betterfooter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"
)

// ParseCodexUsageHeaders reads the x-codex-* response headers into the windows the app-server produces, so the
// footer stays fresh between polls without spawning a process. It returns nil when the response does not look like
// Codex (no x-codex-* header and no 429). A slot without a reset keeps the previous window's reset and capture time
// so its countdown keeps ticking; headers carry no credit information, so a previous advisory flag is kept.
func ParseCodexUsageHeaders(headers map[string]string, status int, previous []RateWindow, now time.Time) []RateWindow {
	lower := make(map[string]string, len(headers))
	for name, value := range headers {
		lower[strings.ToLower(name)] = value
	}
	looksLikeCodex := status == 429
	for name := range lower {
		if strings.HasPrefix(name, "x-codex-") {
			looksLikeCodex = true
		}
	}
	if !looksLikeCodex {
		return nil
	}
	clamp := func(n float64) float64 { return math.Max(0, math.Min(100, n)) }
	var out []RateWindow
	for _, slot := range []string{"primary", "secondary"} {
		used := parseNumber(lower["x-codex-"+slot+"-used-percent"])
		minutes := parseNumber(lower["x-codex-"+slot+"-window-minutes"])
		after := parseNumber(lower["x-codex-"+slot+"-reset-after-seconds"])
		at := parseNumber(lower["x-codex-"+slot+"-reset-at"])
		if used == nil && minutes == nil && after == nil && at == nil {
			continue
		}
		scope := "codex:" + slot
		var prev RateWindow
		hasPrev := false
		if i := slices.IndexFunc(previous, func(w RateWindow) bool { return w.Scope == scope }); i >= 0 {
			prev, hasPrev = previous[i], true
		}
		remaining := 100.0
		switch {
		case used != nil:
			remaining = clamp(100 - *used)
		case hasPrev:
			remaining = prev.Percent
		}
		var resetSec float64
		captured := now
		switch {
		case at != nil && *at > 0:
			resetSec = math.Max(0, *at-math.Floor(float64(now.UnixMilli())/1000))
		case after != nil && *after > 0:
			resetSec = *after
		case hasPrev:
			resetSec, captured = prev.ResetSec, prev.CapturedAt
		}
		window := RateWindow{Scope: scope, Percent: clamp(remaining), HasReset: resetSec > 0, ResetSec: resetSec, CapturedAt: captured, Advisory: hasPrev && prev.Advisory}
		switch {
		case minutes != nil:
			window.WindowMins = *minutes
		case hasPrev:
			window.WindowMins = prev.WindowMins
		}
		out = append(out, window)
	}
	return out
}

type codexWindowPayload struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *float64 `json:"windowDurationMins"`
	ResetsAt           *float64 `json:"resetsAt"`
}

type codexSnapshotPayload struct {
	Primary         *codexWindowPayload `json:"primary"`
	Secondary       *codexWindowPayload `json:"secondary"`
	IndividualLimit *struct {
		RemainingPercent *float64 `json:"remainingPercent"`
	} `json:"individualLimit"`
	Credits *struct {
		HasCredits bool `json:"hasCredits"`
		Unlimited  bool `json:"unlimited"`
	} `json:"credits"`
}

// ParseCodexRateLimits converts the result of the app-server `account/rateLimits/read` call. When the backend
// still allows usage (or credits pay past the plan limits) a used-up plan window is shown but advisory; the
// individual spend limit caps credit use, so it stays binding.
func ParseCodexRateLimits(result json.RawMessage, now time.Time) ([]RateWindow, error) {
	var payload struct {
		RateLimits           *codexSnapshotPayload `json:"rateLimits"`
		OrdinaryUsageAllowed *bool                 `json:"ordinaryUsageAllowed"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, errors.New("codex rate limits: invalid response")
	}
	snapshot := payload.RateLimits
	if snapshot == nil {
		return nil, nil
	}
	stillUsable := payload.OrdinaryUsageAllowed != nil && *payload.OrdinaryUsageAllowed ||
		snapshot.Credits != nil && (snapshot.Credits.Unlimited || snapshot.Credits.HasCredits)
	var windows []RateWindow
	add := func(name string, value *codexWindowPayload) {
		if value == nil || value.UsedPercent == nil {
			return
		}
		var resetSec float64
		if value.ResetsAt != nil {
			if at := *value.ResetsAt * 1000; at > float64(now.UnixMilli()) {
				resetSec = (at - float64(now.UnixMilli())) / 1000
			}
		}
		w := RateWindow{Scope: "codex:" + name, Percent: math.Max(0, math.Min(100, 100-*value.UsedPercent)), HasReset: resetSec > 0, ResetSec: resetSec, CapturedAt: now, Advisory: stillUsable}
		if value.WindowDurationMins != nil {
			w.WindowMins = *value.WindowDurationMins
		}
		windows = append(windows, w)
	}
	add("primary", snapshot.Primary)
	add("secondary", snapshot.Secondary)
	if snapshot.IndividualLimit != nil && snapshot.IndividualLimit.RemainingPercent != nil {
		windows = append(windows, RateWindow{Scope: "codex:individual", Percent: math.Max(0, math.Min(100, *snapshot.IndividualLimit.RemainingPercent)), CapturedAt: now})
	}
	SortRateWindows(windows)
	return windows, nil
}

const (
	codexTimeout  = 10 * time.Second
	codexKillWait = 2 * time.Second
	maxLineBytes  = 1 << 20
)

// ErrCodexUnavailable reports that the codex CLI is not installed.
var ErrCodexUnavailable = errors.New("codex CLI not found")

// ReadCodexRateLimits asks the locally authenticated Codex CLI for the subscription quota through its read-only
// `codex app-server --stdio` RPC; no model turn is started. The command is fixed, runs without a shell, and is
// bounded by a 10 s timeout, then SIGTERM and SIGKILL after 2 s. The CLI's stderr is discarded.
func ReadCodexRateLimits(ctx context.Context, now func() time.Time) ([]RateWindow, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return nil, ErrCodexUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, codexTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "app-server", "--stdio")
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = codexKillWait
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex rate limits: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex rate limits: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex rate limits: start failed: %w", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()
	go func() {
		const requests = `{"method":"initialize","id":1,"params":{"clientInfo":{"name":"pig-better-footer","title":"PiG Better Footer","version":"1.0.0"}}}` + "\n" +
			`{"method":"initialized","params":{}}` + "\n" +
			`{"method":"account/rateLimits/read","id":2,"params":{}}` + "\n"
		_, _ = stdin.Write([]byte(requests))
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var response struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(line, &response) != nil || response.ID == nil || *response.ID != 2 {
			continue // notifications and malformed lines
		}
		_ = stdin.Close()
		return ParseCodexRateLimits(response.Result, now())
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("codex rate limits: %w", ctx.Err())
	}
	return nil, errors.New("codex rate limits: app-server ended without an answer")
}
