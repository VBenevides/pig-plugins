package automodels

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func validAuth() AuthEntry {
	return AuthEntry{Type: "oauth", Access: "access-secret", Refresh: "refresh-secret", Expires: 9999999999999}
}
func jsonResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestNativeQuotaHTTPRequests(t *testing.T) {
	for _, provider := range []string{"Claude", "Codex"} {
		t.Run(provider, func(t *testing.T) {
			entry := validAuth()
			entry.AccountID = "explicit-account"
			calls := 0
			client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer access-secret" || request.Header.Get("Accept") != "application/json" {
					t.Fatalf("unexpected request: %s %v", request.Method, request.Header)
				}
				deadline, ok := request.Context().Deadline()
				if !ok || time.Until(deadline) > 15*time.Second || time.Until(deadline) <= 0 {
					t.Fatal("quota request must have a bounded deadline")
				}
				if provider == "Claude" {
					if request.URL.String() != claudeUsageURL || request.Header.Get("anthropic-beta") != "oauth-2025-04-20" || request.Header.Get("chatgpt-account-id") != "" {
						t.Fatal("wrong Claude endpoint or headers")
					}
					return jsonResponse(request, 200, `{"limits":[{"kind":"session","percent":12.5,"severity":"normal","resets_at":"2026-01-01T00:00:00Z"},{"kind":"weekly_scoped","percent":99,"scope":{"model":{"display_name":"Fable"}}}]}`), nil
				}
				if request.URL.String() != codexUsageURL || request.Header.Get("chatgpt-account-id") != "explicit-account" || request.Header.Get("originator") != "pi" {
					t.Fatal("wrong Codex endpoint or headers")
				}
				return jsonResponse(request, 200, `{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":5,"limit_window_seconds":18000,"reset_after_seconds":20,"reset_at":1700000020}},"additional_rate_limits":[{"limit_name":"GPT-5.3-Codex-Spark","metered_feature":"spark","rate_limit":{"secondary_window":{"used_percent":80,"limit_window_seconds":604800,"reset_after_seconds":604800,"reset_at":1700604800}}}]}`), nil
			})}}
			if provider == "Claude" {
				usage, err := client.FetchClaude(context.Background(), entry)
				if err != nil || len(usage.Limits) != 2 || *usage.Limits[0].Percent != 12.5 || *usage.Limits[1].Scope.Model.DisplayName != "Fable" {
					t.Fatalf("Claude decode: %+v %v", usage, err)
				}
			} else {
				usage, err := client.FetchCodex(context.Background(), entry)
				if err != nil || usage.PlanType != "pro" || !usage.RateLimit.Allowed || usage.RateLimit.PrimaryWindow.ResetAt != 1700000020 || usage.AdditionalRateLimits[0].RateLimit.SecondaryWindow.UsedPercent != 80 {
					t.Fatalf("Codex decode: %+v %v", usage, err)
				}
			}
			if calls != 1 {
				t.Fatalf("calls = %d", calls)
			}
		})
	}
}
func TestCodexAccountIDExtractionAndPrecedence(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"jwt-account"}}`))
	for _, test := range []struct{ token, account, want string }{
		{"header." + payload + ".signature", "", "jwt-account"},
		{"header." + payload + ".signature", "explicit-account", "explicit-account"},
		{"not-a-jwt", "", ""},
		{"header.invalid.signature", "", ""},
	} {
		entry := validAuth()
		entry.Access, entry.AccountID = test.token, test.account
		client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if got := request.Header.Get("chatgpt-account-id"); got != test.want {
				t.Fatalf("account got %q, want %q", got, test.want)
			}
			return jsonResponse(request, 200, `{"additional_rate_limits":null}`), nil
		})}}
		if _, err := client.FetchCodex(context.Background(), entry); err != nil {
			t.Fatal(err)
		}
	}
}
func TestInvalidCredentialsNeverReachOAuthEndpoints(t *testing.T) {
	entries := []AuthEntry{
		{Type: "api_key", Access: "api-key-secret", Expires: 9999999999999},
		{Type: "oauth", Expires: 9999999999999},
		{Type: "oauth", Access: "access-secret"},
		{Type: "oauth", Access: "access-secret", Expires: math.NaN()},
		{Type: "oauth", Access: "access-secret", Expires: math.Inf(1)},
		{Type: "oauth", Access: "access-secret", Expires: 1},
	}
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid credentials reached HTTP transport")
		return nil, nil
	})}}
	for _, entry := range entries {
		if _, err := client.FetchClaude(context.Background(), entry); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("Claude credentials error: %v", err)
		}
		if _, err := client.FetchCodex(context.Background(), entry); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("Codex credentials error: %v", err)
		}
	}
}
func TestQuotaResponseAndTransportErrorsAreSanitized(t *testing.T) {
	for _, test := range []struct {
		name, body   string
		status       int
		transportErr error
		want         string
	}{
		{"HTTP failure", `access-secret private body`, 401, nil, "HTTP 401"},
		{"malformed", `{"access-secret":`, 200, nil, "invalid JSON"},
		{"null", `null`, 200, nil, "invalid JSON"},
		{"array", `[]`, 200, nil, "invalid JSON"},
		{"wrong field type", `{"limits":"access-secret"}`, 200, nil, "invalid JSON"},
		{"trailing JSON", `{} {}`, 200, nil, "invalid JSON"},
		{"too large", strings.Repeat(" ", maxReadBytes+1), 200, nil, "exceeds 1 MiB"},
		{"transport", "", 0, errors.New("access-secret refresh-secret body"), "HTTP request failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if test.transportErr != nil {
					return nil, test.transportErr
				}
				return jsonResponse(request, test.status, test.body), nil
			})}}
			usage, err := client.FetchClaude(context.Background(), validAuth())
			if err == nil || usage != nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("result = %+v, error = %v", usage, err)
			}
		})
	}
}
func TestQuotaCancellationAndRedirectProtection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("pre-canceled request reached transport")
		return nil, nil
	})}}
	if _, err := client.FetchClaude(ctx, validAuth()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	client.HTTP.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		cancel()
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	if _, err := client.FetchCodex(ctx, validAuth()); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancellation: %v", err)
	}
	calls := 0
	originalRedirectCalled := false
	configured := &http.Client{Timeout: time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { originalRedirectCalled = true; return nil }, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			t.Fatal("authenticated redirect followed")
		}
		response := jsonResponse(request, 302, "access-secret")
		response.Header.Set("Location", "https://attacker.invalid/access-secret")
		return response, nil
	})}
	if _, err := (Client{HTTP: configured}).FetchClaude(context.Background(), validAuth()); err == nil || !strings.Contains(err.Error(), "redirect rejected") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("redirect error: %v", err)
	}
	if calls != 1 || originalRedirectCalled || configured.Timeout != time.Minute {
		t.Fatal("client policy was mutated or redirect escaped")
	}
	if configured.CheckRedirect == nil {
		t.Fatal("shared client callback was removed")
	}
}

type boundedBody struct{ read, closed int }

func (b *boundedBody) Read(data []byte) (int, error) {
	for i := range data {
		data[i] = ' '
	}
	b.read += len(data)
	return len(data), nil
}
func (b *boundedBody) Close() error { b.closed++; return nil }
func TestQuotaResponseReadBoundAndClose(t *testing.T) {
	body := &boundedBody{}
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := jsonResponse(request, 200, "")
		response.Body = body
		return response, nil
	})}}
	if _, err := client.FetchCodex(context.Background(), validAuth()); err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("unbounded body accepted: %v", err)
	}
	if body.read != maxReadBytes+1 || body.closed != 1 {
		t.Fatalf("read %d bytes, closed %d times", body.read, body.closed)
	}
}

type failedBody struct {
	err    error
	closed bool
}

func (b *failedBody) Read([]byte) (int, error) { return 0, b.err }
func (b *failedBody) Close() error             { b.closed = true; return nil }
func TestQuotaReadFailureAndHTTPFailureClose(t *testing.T) {
	body := &failedBody{err: errors.New("access-secret raw body error")}
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := jsonResponse(request, 200, "")
		response.Body = body
		return response, nil
	})}}
	if usage, err := client.FetchClaude(context.Background(), validAuth()); err == nil || usage != nil || !strings.Contains(err.Error(), "response read failed") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("body failure: %+v %v", usage, err)
	}
	if !body.closed {
		t.Fatal("failed response body was not closed")
	}
	bounded := &boundedBody{}
	client.HTTP.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := jsonResponse(request, 429, "")
		response.Body = bounded
		return response, nil
	})
	if _, err := client.FetchCodex(context.Background(), validAuth()); err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("HTTP failure: %v", err)
	}
	if bounded.read != 0 || bounded.closed != 1 {
		t.Fatal("HTTP error body must be closed without reading private content")
	}
}
func TestQuotaCallerDeadlineAndExactReadBoundary(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		got, ok := request.Context().Deadline()
		if !ok || !got.Before(deadline) || time.Until(got) > 15*time.Second {
			t.Fatal("long caller deadline bypassed HTTP bound")
		}
		return jsonResponse(request, 200, "{}"+strings.Repeat(" ", maxReadBytes-2)), nil
	})}}
	if usage, err := client.FetchClaude(ctx, validAuth()); err != nil || usage == nil {
		t.Fatalf("exact read boundary: %+v %v", usage, err)
	}
	// A shorter-than-15s deadline must not be extended; no timers need to fire.
	shortDeadline := time.Now().Add(10 * time.Second)
	shortCtx, shortCancel := context.WithDeadline(context.Background(), shortDeadline)
	defer shortCancel()
	client.HTTP.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		got, ok := request.Context().Deadline()
		if !ok || !got.Equal(shortDeadline) {
			t.Fatal("caller deadline was extended")
		}
		return jsonResponse(request, 200, "{}"), nil
	})
	if _, err := client.FetchCodex(shortCtx, validAuth()); err != nil {
		t.Fatal(err)
	}
	expired, expiredCancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer expiredCancel()
	if _, err := client.FetchClaude(expired, validAuth()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired context: %v", err)
	}
}

type closeFailureBody struct{ io.Reader }

func (closeFailureBody) Close() error { return errors.New("access-secret close error") }
func TestQuotaCloseFailureIsObservable(t *testing.T) {
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := jsonResponse(request, 200, "")
		response.Body = closeFailureBody{Reader: strings.NewReader("{}")}
		return response, nil
	})}}
	if usage, err := client.FetchClaude(context.Background(), validAuth()); err == nil || usage != nil || !strings.Contains(err.Error(), "response close failed") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("close failure: %+v %v", usage, err)
	}
}
