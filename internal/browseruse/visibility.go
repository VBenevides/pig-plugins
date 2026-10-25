package browseruse

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Visibility string

const (
	Hidden Visibility = "hidden"
	Login  Visibility = "login"
	Always Visibility = "always"
)

func ParseVisibility(args string) (Visibility, error) {
	fields := strings.Fields(args)
	if len(fields) == 2 && fields[0] == "visibility" {
		value := Visibility(fields[1])
		if value == Hidden || value == Login || value == Always {
			return value, nil
		}
	}
	return "", errors.New("usage: /browser-use visibility hidden|login|always")
}

// SetVisible restarts only the owned browser, using the same profile. It restores
// the current HTTP(S) URL, not tabs, form state, or previous write operations.
// A failed shutdown blocks further use rather than risking concurrent profile access.
func (c *Client) SetVisible(ctx context.Context, visible bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("browser session is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.visible == visible {
		return nil
	}
	if c.session == "" {
		c.visible = visible
		return nil
	}
	var currentURL string
	if err := c.request(ctx, "GET", "/session/"+c.session+"/url", nil, &currentURL); err != nil {
		return fmt.Errorf("read URL before visibility change: %w", err)
	}
	if currentURL != "about:blank" {
		if err := ValidateNavigation(currentURL); err != nil {
			return fmt.Errorf("cannot restore browser URL: %w", err)
		}
	}
	if err := c.request(ctx, "DELETE", "/session/"+c.session, nil, nil); err != nil {
		c.closed = true
		return fmt.Errorf("close browser before visibility change failed. The extension did not start a replacement: %w", err)
	}
	c.session = ""
	c.invalidate()
	c.visible = visible
	c.headless = false
	if err := c.start(ctx); err != nil {
		c.closed = true
		return fmt.Errorf("restart browser with selected visibility failed. Stop the driver session before another attempt: %w", err)
	}
	if currentURL == "about:blank" {
		return nil
	}
	if err := c.request(ctx, "POST", "/session/"+c.session+"/url", map[string]any{"url": currentURL}, nil); err != nil {
		return fmt.Errorf("restore page after visibility change failed. Do not repeat previous writes: %w", err)
	}
	return nil
}

// Hide closes a login browser without revisiting an unconfirmed secured page.
// The next operation starts headless with the same profile.
func (c *Client) Hide(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("browser session is closed")
	}
	if c.session != "" {
		if err := c.request(ctx, "DELETE", "/session/"+c.session, nil, nil); err != nil {
			c.closed = true
			return fmt.Errorf("close login browser: %w", err)
		}
	}
	c.session = ""
	c.visible = false
	c.headless = false
	c.invalidate()
	return nil
}
