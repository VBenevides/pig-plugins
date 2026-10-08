package betterfooter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// HTTPTimeout bounds every quota request.
	HTTPTimeout = 15 * time.Second
	// MaxResponseBytes caps every quota response body.
	MaxResponseBytes = 1 << 20
)

var errRedirect = errors.New("redirect refused")

// httpGet performs one bounded GET. Redirects are never followed (the request carries credentials), the body is
// capped at 1 MiB, and errors name the source and the HTTP status only: they never carry the URL, a header, a
// token or a response body.
func httpGet(ctx context.Context, client *http.Client, source, url string, headers map[string]string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	ctx, cancel := context.WithTimeout(ctx, HTTPTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot create request", source)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	c := http.Client{}
	if client != nil {
		c = *client
	}
	if c.Timeout <= 0 || c.Timeout > HTTPTimeout {
		c.Timeout = HTTPTimeout
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return errRedirect }
	response, err := c.Do(request)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, fmt.Errorf("%s: %w", source, ctx.Err())
		case errors.Is(err, errRedirect):
			return nil, fmt.Errorf("%s: HTTP redirect refused", source)
		}
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return nil, fmt.Errorf("%s: request timed out", source)
		}
		return nil, fmt.Errorf("%s: HTTP request failed", source)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: HTTP %d", source, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: %w", source, ctx.Err())
		}
		return nil, fmt.Errorf("%s: response read failed", source)
	}
	if len(data) > MaxResponseBytes {
		return nil, fmt.Errorf("%s: response exceeds 1 MiB", source)
	}
	return data, nil
}
