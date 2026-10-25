package browseruse

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEndpointPolicy(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:9515", "http://[::1]:4444/"} {
		if err := ValidateEndpoint(endpoint); err != nil {
			t.Errorf("valid endpoint: %v", err)
		}
	}
	for _, endpoint := range []string{"http://example.com:9515", "http://localhost:9515", "https://127.0.0.1:1", "http://127.0.0.1", "http://user:secret@127.0.0.1:1", "http://127.0.0.1:1/?token=secret", "http://127.0.0.1:1/wd/hub", "file:///tmp/x"} {
		err := ValidateEndpoint(endpoint)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("unsafe endpoint admitted or leaked: %v", err)
		}
	}
}

func TestNavigationPolicy(t *testing.T) {
	for _, raw := range []string{"https://example.org/a?q=x", "http://127.0.0.1:1234/"} {
		if err := ValidateNavigation(raw); err != nil {
			t.Error(err)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "chrome://settings", "about:config", "data:text/html,x", "https://user:secret@example.org", "//example.org", strings.Repeat("x", 8193)} {
		err := ValidateNavigation(raw)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("unsafe navigation admitted or leaked: %v", err)
		}
	}
}

func TestConfirmationIsFailClosedAndNeverRetries(t *testing.T) {
	ctx := context.Background()
	for _, action := range []string{"navigate", "click", "fill", "select_tab"} {
		if ConfirmAction(ctx, action, nil) == nil {
			t.Fatal("headless action admitted")
		}
		calls := 0
		if err := ConfirmAction(ctx, action, func(title, body string) (bool, error) { calls++; return false, nil }); err == nil || calls != 1 {
			t.Fatal("denial ignored or retried")
		}
		original := errors.New("dialog unavailable")
		if err := ConfirmAction(ctx, action, func(string, string) (bool, error) { return false, original }); !errors.Is(err, original) {
			t.Fatal("lost confirmation error")
		}
		if err := ConfirmAction(ctx, action, func(string, string) (bool, error) { return true, nil }); err != nil {
			t.Error(err)
		}
	}
	for _, action := range []string{"execute_js", "raw_cdp", "upload", "download", "run_script", "unknown"} {
		if ConfirmAction(ctx, action, func(string, string) (bool, error) {
			t.Fatal("unsupported action reached confirmation")
			return true, nil
		}) == nil {
			t.Fatal("unsupported action admitted")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if !errors.Is(ConfirmAction(ctx, "click", nil), context.Canceled) {
		t.Fatal("lost cancellation")
	}
}

func TestParseMode(t *testing.T) {
	for _, mode := range []Mode{Off, Chrome, Firefox} {
		got, err := ParseMode("mode " + string(mode))
		if err != nil || got != mode {
			t.Fatalf("%s: %s %v", mode, got, err)
		}
	}
	for _, args := range []string{"", "chrome", "mode", "mode safari", "mode chrome extra"} {
		if _, err := ParseMode(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}
