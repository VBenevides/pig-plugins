package cursordirect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const BaseURL = "https://api2.cursor.sh"
const clientVersion = "cli-2026.09.02-c22c1a3"
const usablePath = "/agent.v1.AgentService/GetUsableModels"
const availablePath = "/aiserver.v1.AiService/AvailableModels"
const runPath = "/agent.v1.AgentService/Run"
const runSSEPath = "/agent.v1.AgentService/RunSSE"
const appendPath = "/aiserver.v1.BidiService/BidiAppend"

// Client does not retain credentials, account catalogs, or conversation state.
// The unexported endpoint/client are substituted only by package-local tests.
type Client struct {
	http  *http.Client
	base  string
	slots chan struct{}
	sse   bool
}

func New() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = true
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &Client{http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: BaseURL, slots: make(chan struct{}, 4)}
}

// NewWithTransport selects a transport before generation. It never retries a
// failed generation over another transport. Empty selects the HTTP/2 Run path.
func NewWithTransport(mode string) (*Client, error) {
	if mode != "" && mode != "h2" && mode != "sse" {
		return nil, errors.New("cursor: PIG_CURSOR_TRANSPORT must be h2 or sse")
	}
	c := New()
	c.sse = mode == "sse"
	return c, nil
}

type safeError struct{ cause error }

func (e *safeError) Error() string { return "cursor: authenticated HTTP request failed" }
func (e *safeError) Unwrap() error { return e.cause }
func (c *Client) request(ctx context.Context, token, path, contentType string, body io.Reader) (*http.Response, error) {
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("cursor: missing or invalid access token; run /login cursor")
	}
	if path != runPath && path != usablePath && path != availablePath && path != runSSEPath && path != appendPath {
		return nil, errors.New("cursor: unapproved RPC path")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+path, body)
	if err != nil {
		return nil, &safeError{err}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Cursor-Client-Type", "cli")
	req.Header.Set("X-Cursor-Client-Version", clientVersion)
	req.Header.Set("X-Ghost-Mode", "true")
	// Never forward credentials through redirects, even with a test/custom client.
	hc := *c.http
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return nil, &safeError{err}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("cursor: RPC rejected (HTTP %d)", resp.StatusCode)
	}
	return resp, nil
}
func (c *Client) unary(ctx context.Context, token, path string, body []byte) (message, error) {
	resp, err := c.request(ctx, token, path, "application/proto", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFrame+1))
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, &safeError{err}
	}
	if closeErr != nil {
		return nil, &safeError{closeErr}
	}
	return parse(b)
}

// Catalog uses the account's exact usable slugs. Rich metadata is optional, not
// permission to add models absent from the authoritative usable-model roster.
func (c *Client) Catalog(ctx context.Context, token string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	usable, err := c.unary(ctx, token, usablePath, nil)
	if err != nil {
		return nil, err
	}
	rich, richErr := c.unary(ctx, token, availablePath, join(integer(2, 1), integer(5, 1), integer(7, 1), integer(12, 1)))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	details := map[string]message{}
	if richErr == nil {
		for _, f := range rich {
			if f.number != 2 {
				continue
			}
			m, err := parse(f.data)
			if err != nil {
				return nil, err
			}
			details[m.text(1)] = m
			details[m.text(18)] = m
			for _, a := range m {
				if a.number == 36 || a.number == 37 {
					details[string(a.data)] = m
				}
			}
			for _, v := range m {
				if v.number == 30 {
					variant, e := parse(v.data)
					if e != nil {
						return nil, e
					}
					if id := variant.text(11); id != "" {
						details[id] = m
					}
				}
			}
		}
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, f := range usable {
		if f.number != 1 {
			continue
		}
		m, err := parse(f.data)
		if err != nil {
			return nil, err
		}
		id := m.text(1)
		if strings.TrimSpace(id) == "" || len(id) > 512 {
			return nil, errors.New("cursor: invalid model id")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		name := m.text(4)
		if name == "" {
			name = id
		}
		// OMP compatibility budgets; these are not advertised server entitlements.
		contextWindow := uint64(200000)
		maxTokens := uint64(64000)
		unknown := true
		if d := details[id]; d != nil {
			if n := d.num(15); n > 0 {
				contextWindow = n
				unknown = false
			}
			if m.num(7) != 0 && d.num(16) > 0 {
				contextWindow = d.num(16)
				unknown = false
			}
		}
		if maxTokens > contextWindow {
			maxTokens = contextWindow
		}
		model := map[string]any{"id": id, "name": name, "provider": "cursor", "api": "cursor-agent", "baseUrl": BaseURL, "reasoning": m.has(2), "input": []string{"text"}, "contextWindow": contextWindow, "maxTokens": maxTokens, "cursorContextEstimated": unknown, "cursorOutputEstimated": true, "cursorMaxMode": m.num(7) != 0}
		if richErr != nil {
			model["cursorMetadataUnavailable"] = true
		}
		out = append(out, model)
		if len(out) > 2048 {
			return nil, errors.New("cursor: excessive catalog size")
		}
	}
	return out, nil
}
