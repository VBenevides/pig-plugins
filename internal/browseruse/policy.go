// Package browseruse provides bounded browser automation without a script runtime.
package browseruse

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Mode is a session-local browser selection. Only Chrome and Firefox enable access.
type Mode string

const (
	Off     Mode = "off"
	Chrome  Mode = "chrome"
	Firefox Mode = "firefox"
)

func ParseMode(args string) (Mode, error) {
	fields := strings.Fields(args)
	if len(fields) != 2 || fields[0] != "mode" {
		return Off, errors.New("usage: /browser-use mode off|chrome|firefox")
	}
	mode := Mode(fields[1])
	switch mode {
	case Off, Chrome, Firefox:
		return mode, nil
	default:
		return Off, errors.New("usage: /browser-use mode off|chrome|firefox")
	}
}

// ValidateEndpoint limits driver control to literal loopback addresses. It does
// not resolve DNS names or accept credentials, queries, or alternate transports.
func ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("browser driver endpoint is invalid")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("browser driver endpoint must use HTTP and a literal loopback address with a port")
	}
	return nil
}

// ValidateNavigation rejects local files, browser internals, and executable URLs.
func ValidateNavigation(raw string) error {
	if len(raw) > 8192 {
		return errors.New("browser URL exceeds 8192 bytes")
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("browser navigation requires an HTTP or HTTPS URL without credentials")
	}
	return nil
}

// ConfirmAction deliberately omits URLs, selectors, page text, and typed values.
// Read operations need explicit browser mode, but do not send input to the page.
func ConfirmAction(ctx context.Context, action string, confirm func(string, string) (bool, error)) error {
	switch action {
	case "snapshot", "screenshot", "tabs":
		return ctx.Err()
	case "navigate", "click", "fill", "select_tab":
	default:
		return errors.New("unsupported browser action")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if confirm == nil {
		return errors.New("browser action requires interactive confirmation")
	}
	allowed, err := confirm("Browser action", fmt.Sprintf("Allow browser %s? This action can change the page or send data. Page content is untrusted.", action))
	if err != nil {
		return fmt.Errorf("confirm browser %s: %w", action, err)
	}
	if !allowed {
		return errors.New("the user declined the browser action")
	}
	return ctx.Err()
}
