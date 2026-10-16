package automodels

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

func TestCursorUsageDashboardIsolatesAccountFailures(t *testing.T) {
	token := func(id string) string {
		return "test." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|`+id+`"}`)) + ".signature"
	}
	source := &fakeAccounts{
		accounts: []sdk.OAuthAccount{{ID: "first", Provider: "cursor", Label: "Personal"}, {ID: "second", Provider: "cursor", Label: "Work", Active: true}},
		auth: map[string]map[string]any{
			"first":  {"type": "oauth", "access": token("user_first"), "expires": float64(9999999999999)},
			"second": {"type": "oauth", "access": token("user_second"), "expires": float64(9999999999999)},
		},
	}
	calls := 0
	x := &extension{store: quota.Store{Dir: t.TempDir()}}
	x.client = quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		cookie, err := r.Cookie("WorkosCursorSessionToken")
		if err != nil {
			t.Fatal(err)
		}
		value, err := url.QueryUnescape(cookie.Value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(value, "user_first::") {
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("SECRET"))}, nil
		}
		if value != "user_second::"+token("user_second") {
			t.Fatal("wrong account")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"individualUsage":{"plan":{"limit":2000,"autoPercentUsed":11,"apiPercentUsed":100}}}`))}, nil
	})}}
	lines, err := x.usageLines(context.Background(), source, "cursor", "composer")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{"Personal", "Failed to fetch quota: Cursor usage HTTP 401", "Work [active]", "Cursor Models: 11.00%", "Other Models: $20.00", "Reset: unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "SECRET") || strings.Contains(text, token("user_second")) || strings.Contains(text, "Quota available") || strings.Contains(text, "📊 ❌ Rate-limited") || calls != 2 {
		t.Fatalf("misleading dashboard or bad fetch count: %s calls=%d", text, calls)
	}
	if source.accounts[1].Active != true {
		t.Fatal("usage mutated selection")
	}
}
