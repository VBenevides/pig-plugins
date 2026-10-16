package automodels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func cursorTestAuth(subject string) AuthEntry {
	claims, _ := json.Marshal(map[string]any{"sub": subject})
	entry := validAuth()
	entry.Access = "test." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	return entry
}

func TestCursorUsagePools(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
		fail        bool
	}{
		{"separate pools", `{"individualUsage":{"plan":{"limit":2000,"autoPercentUsed":11,"apiPercentUsed":100}},"billingCycleEnd":"2030-01-02T03:04:05Z"}`, []string{"Cursor Models: 11.00%", "Other Models: $20.00 used / $20.00 limit; remaining $0.00", "2030-01-02T03:04:05Z"}, false},
		{"zero", `{"individualUsage":{"overall":{"used":0,"limit":2000}}}`, []string{"$0.00 used / $20.00 limit; remaining $20.00", "Reset: unknown"}, false},
		{"missing limits", `{"individualUsage":{"overall":{"used":150}}}`, []string{"$1.50 used / unknown limit; remaining unknown"}, false},
		{"remaining only", `{"individualUsage":{"overall":{"limit":2000,"remaining":1500}}}`, []string{"$5.00 used / $20.00 limit; remaining $15.00"}, false},
		{"stale zero used", `{"individualUsage":{"overall":{"used":0,"limit":2000,"remaining":1500}}}`, []string{"$5.00 used / $20.00 limit; remaining $15.00"}, false},
		{"malformed neighbor", `{"individualUsage":{"overall":{"used":"bad"},"plan":{"enabled":false},"onDemand":{"used":100,"limit":500}}}`, []string{"On-Demand Usage: $1.00"}, false},
		{"disabled overall falls through", `{"individualUsage":{"overall":{"enabled":false,"used":100},"plan":{"autoPercentUsed":"25.5"}}}`, []string{"Cursor Models: 25.50%"}, false},
		{"overage", `{"individualUsage":{"onDemand":{"used":700,"limit":500}}}`, []string{"$7.00 used / $5.00 limit; remaining $0.00"}, false},
		{"total percent", `{"individualUsage":{"plan":{"totalPercentUsed":125}}}`, []string{"Personal Usage: 125.00%"}, false},
		{"numeric reset", `{"individualUsage":{"plan":{"autoPercentUsed":1}},"nextReset":2000000000000}`, []string{"2033-05-18T03:33:20Z"}, false},
		{"empty", `{}`, nil, true},
		{"null", `null`, nil, true},
		{"no meters", `{"individualUsage":{}}`, nil, true},
		{"negative", `{"individualUsage":{"overall":{"used":-1,"limit":10}}}`, nil, true},
		{"nonfinite", `{"individualUsage":{"plan":{"autoPercentUsed":"NaN","apiPercentUsed":"Inf"}}}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input map[string]any
			if err := json.Unmarshal([]byte(tc.input), &input); err != nil {
				t.Fatal(err)
			}
			usage, err := parseCursorUsage(input)
			if tc.fail {
				if err == nil || usage != nil {
					t.Fatalf("usage=%+v err=%v", usage, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			usage.FetchedAt = time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)
			text := strings.Join(FormatCursorUsageLines(usage), "\n")
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q: %s", want, text)
				}
			}
			for _, forbidden := range []string{"Quota available", "unlimited", "NaN", "+Inf"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("misleading display: %s", text)
				}
			}
		})
	}
}

func TestCursorUsageSelectedAccountHeaders(t *testing.T) {
	var cookies []string
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != cursorUsageURL || r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("chatgpt-account-id") != "" {
			t.Fatal("unexpected origin or headers")
		}
		cookie, err := r.Cookie("WorkosCursorSessionToken")
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := url.QueryUnescape(cookie.Value)
		if err != nil {
			t.Fatal(err)
		}
		cookies = append(cookies, decoded)
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("missing deadline")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"individualUsage":{"plan":{"autoPercentUsed":1}}}`))}, nil
	})}}
	for _, id := range []string{"user_first", "user_second", "user_first"} {
		entry := cursorTestAuth("auth0|" + id)
		usage, err := client.FetchCursor(context.Background(), entry)
		if err != nil || usage == nil || usage.FetchedAt.IsZero() {
			t.Fatalf("usage=%v err=%v", usage, err)
		}
		if got := cookies[len(cookies)-1]; got != id+"::"+entry.Access {
			t.Fatal("wrong account cookie")
		}
	}
	if len(cookies) != 3 {
		t.Fatal("unexpected shared cache")
	}
}

func TestCursorUsageFailures(t *testing.T) {
	for _, status := range []int{301, 401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://attacker.invalid/SECRET"}}, Body: io.NopCloser(strings.NewReader("SECRET"))}, nil
			})}}
			usage, err := client.FetchCursor(context.Background(), cursorTestAuth("user_test"))
			if err == nil || usage != nil || strings.Contains(err.Error(), "SECRET") || calls != 1 {
				t.Fatalf("usage=%v err=%v calls=%d", usage, err, calls)
			}
		})
	}
	for _, body := range []string{"null", "{", `{"individualUsage":{}}`, strings.Repeat("x", maxReadBytes+1)} {
		client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		if usage, err := client.FetchCursor(context.Background(), cursorTestAuth("user_test")); err == nil || usage != nil {
			t.Fatal("accepted malformed/oversized response")
		}
	}
	run, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Client{}).FetchCursor(run, cursorTestAuth("user_test")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}}
	run, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := client.FetchCursor(run, cursorTestAuth("user_test")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}

func TestCursorUsageInvalidCredentialsNeverSend(t *testing.T) {
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid credential sent"); return nil, nil })}}
	for _, sub := range []string{"", "auth0|bad;cookie", "auth0|user\r\nSECRET"} {
		if _, err := client.FetchCursor(context.Background(), cursorTestAuth(sub)); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("invalid subject err=%v", err)
		}
	}
	entry := cursorTestAuth("user_test")
	entry.Expires = 1
	if _, err := client.FetchCursor(context.Background(), entry); err == nil {
		t.Fatal("accepted expired token")
	}
	entry = validAuth()
	entry.Access = "opaque-SECRET"
	if _, err := client.FetchCursor(context.Background(), entry); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("invalid token err=%v", err)
	}
}
