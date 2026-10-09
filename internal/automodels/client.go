package automodels

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	claudeUsageURL   = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL    = "https://chatgpt.com/backend-api/wham/usage"
	quotaHTTPTimeout = 15 * time.Second
)

// TransientError marks a usage failure that heals by itself: a timeout, a transport failure, or HTTP 429/5xx.
type TransientError struct{ msg string }

func (e *TransientError) Error() string { return e.msg }

// IsTransient reports whether err is a temporary usage failure. Callers log such errors instead of notifying
// the user, because the next refresh retries.
func IsTransient(err error) bool {
	var transient *TransientError
	return errors.As(err, &transient) || errors.Is(err, context.DeadlineExceeded)
}

// Client only calls the two fixed production OAuth usage endpoints. HTTP allows
// deterministic transports; timeout and redirect protection cannot be disabled.
type Client struct{ HTTP *http.Client }

func (c Client) FetchClaude(ctx context.Context, entry AuthEntry) (*ClaudeUsage, error) {
	var usage ClaudeUsage
	if err := c.fetch(ctx, entry, "Claude", claudeUsageURL, &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}
func (c Client) FetchCodex(ctx context.Context, entry AuthEntry) (*CodexUsage, error) {
	var usage CodexUsage
	if err := c.fetch(ctx, entry, "Codex", codexUsageURL, &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}

func (c Client) fetch(ctx context.Context, entry AuthEntry, provider, url string, usage any) (fetchErr error) {
	if ctx == nil {
		return fmt.Errorf("%s usage: missing request context", provider)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s usage: %w", provider, err)
	}
	if entry.Type != "oauth" || strings.TrimSpace(entry.Access) == "" || len(entry.Access) > maxReadBytes || !finite(entry.Expires) || entry.Expires <= 0 {
		return fmt.Errorf("%s usage: invalid OAuth credentials", provider)
	}
	if float64(time.Now().UnixMilli()) > entry.Expires {
		return fmt.Errorf("%s usage: OAuth token expired", provider)
	}
	ctx, cancel := context.WithTimeout(ctx, quotaHTTPTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("%s usage: cannot create request", provider)
	}
	request.Header.Set("Authorization", "Bearer "+entry.Access)
	request.Header.Set("Accept", "application/json")
	if provider == "Claude" {
		request.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else {
		accountID := entry.AccountID
		if accountID == "" {
			accountID = extractCodexAccountID(entry.Access)
		}
		if accountID != "" {
			request.Header.Set("chatgpt-account-id", accountID)
		}
		request.Header.Set("originator", "pi")
	}
	client := http.Client{}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	if client.Timeout <= 0 || client.Timeout > quotaHTTPTimeout {
		client.Timeout = quotaHTTPTimeout
	}
	redirectErr := errors.New("usage redirects are not allowed")
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return redirectErr }
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s usage: %w", provider, ctx.Err())
		}
		if errors.Is(err, redirectErr) {
			return fmt.Errorf("%s usage: HTTP redirect rejected", provider)
		}
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return &TransientError{fmt.Sprintf("%s usage: request timed out", provider)}
		}
		// Transport errors may contain request headers, URLs, or credentials.
		return &TransientError{fmt.Sprintf("%s usage: HTTP request failed", provider)}
	}
	if response.Body == nil {
		return fmt.Errorf("%s usage: missing response body", provider)
	}
	defer func() {
		if err := response.Body.Close(); err != nil && fetchErr == nil {
			fetchErr = fmt.Errorf("%s usage: response close failed", provider)
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := fmt.Sprintf("%s usage HTTP %d", provider, response.StatusCode)
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return &TransientError{status}
		}
		return errors.New(status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxReadBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s usage: %w", provider, ctx.Err())
		}
		return fmt.Errorf("%s usage: response read failed", provider)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s usage: %w", provider, ctx.Err())
	}
	if len(data) > maxReadBytes {
		return fmt.Errorf("%s usage: response exceeds 1 MiB", provider)
	}
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("%s usage: invalid JSON response", provider)
	}
	if err := json.Unmarshal(data, usage); err != nil {
		return fmt.Errorf("%s usage: invalid JSON response", provider)
	}
	return nil
}

// CodexQuotaSupported reports whether the token can address the ChatGPT usage endpoint.
// The "openai" provider may hold an API-audience token that the endpoint rejects with 401.
func CodexQuotaSupported(entry AuthEntry) bool {
	return entry.AccountID != "" || extractCodexAccountID(entry.Access) != ""
}

func extractCodexAccountID(token string) string {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var payload struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	return payload.Auth.AccountID
}
