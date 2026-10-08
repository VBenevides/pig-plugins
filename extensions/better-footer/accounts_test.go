package betterfooter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
	bf "github.com/VBenevides/pig-plugins/internal/betterfooter"
)

type quotaAccounts struct {
	selected                string
	reads                   []string
	expired                 bool
	enumerationErr, authErr error
}

func (s *quotaAccounts) OAuthAccounts() ([]sdk.OAuthAccount, error) {
	if s.enumerationErr != nil {
		return nil, s.enumerationErr
	}
	if s.selected == "none" {
		return nil, nil
	}
	return []sdk.OAuthAccount{
		{ID: "first", Provider: bf.CodexProvider, Active: s.selected == "first"},
		{ID: "second", Provider: bf.CodexProvider, Active: s.selected == "second"},
	}, nil
}
func (s *quotaAccounts) OAuthAccountAuth(id string) (map[string]any, error) {
	s.reads = append(s.reads, id)
	if s.authErr != nil {
		return nil, s.authErr
	}
	expires := time.Now().Add(time.Hour)
	if s.expired {
		expires = time.Now().Add(-time.Hour)
	}
	return map[string]any{"type": "oauth", "access": "secret-" + id, "accountId": "tenant-" + id, "expires": float64(expires.UnixMilli())}, nil
}

type quotaTransport func(*http.Request) (*http.Response, error)

func (f quotaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func quotaResponse(used string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{"allowed":false,"limit_reached":true,"primary_window":{"used_percent":` + used + `,"limit_window_seconds":18000,"reset_after_seconds":3600}}}`))}
}

func TestNativeCodexSelectedCredentialsAndCacheIsolation(t *testing.T) {
	source := &quotaAccounts{selected: "first"}
	requests, cliCalls := 0, 0
	client := quota.Client{HTTP: &http.Client{Transport: quotaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != "https://chatgpt.com/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer secret-"+source.selected || r.Header.Get("chatgpt-account-id") != "tenant-"+source.selected {
			t.Fatalf("request did not use explicitly selected native credentials")
		}
		if source.selected == "first" {
			return quotaResponse("100"), nil
		}
		return quotaResponse("25"), nil
	})}}
	var store bf.QuotaStore
	reader := bf.Reader{Store: &store, Codex: func(context.Context) ([]bf.RateWindow, error) {
		cliCalls++
		return nil, errors.New("unrelated CLI account")
	}, Fetch: func(ctx context.Context, key string) ([]bf.RateWindow, string, bool, error) {
		windows, err := fetchNativeCodex(ctx, source, client, key, time.Now())
		return windows, "", true, err
	}}
	first, err := nativeQuotaKey(source, bf.CodexProvider, bf.CodexProvider)
	if err != nil {
		t.Fatal(err)
	}
	q, known, err := reader.Read(context.Background(), first, false, bf.QuotaCacheAge)
	if err != nil || !bf.IsQuotaExhausted(q, known, time.Now()) {
		t.Fatalf("first quota: %+v %v %v", q, known, err)
	}
	source.selected = "second"
	second, err := nativeQuotaKey(source, bf.CodexProvider, bf.CodexProvider)
	if err != nil || first == second {
		t.Fatal("native cache keys collided", err)
	}
	if q, known := store.Get(second); bf.IsQuotaExhausted(q, known, time.Now()) {
		t.Fatal("first cache exhausted second account")
	}
	q, known, err = reader.Read(context.Background(), second, false, bf.QuotaCacheAge)
	if err != nil || !known || len(q.Windows) != 1 || q.Windows[0].Percent != 75 || bf.IsQuotaExhausted(q, known, time.Now()) {
		t.Fatalf("second quota: %+v %v %v", q, known, err)
	}
	if requests != 2 || cliCalls != 0 || strings.Join(source.reads, ",") != "first,second" {
		t.Fatal("wrong source or credential reads", requests, cliCalls, source.reads)
	}
}

func TestNativeCodexSwitchDuringFetchRejectsPublication(t *testing.T) {
	source := &quotaAccounts{selected: "first"}
	client := quota.Client{HTTP: &http.Client{Transport: quotaTransport(func(*http.Request) (*http.Response, error) {
		source.selected = "second"
		return quotaResponse("100"), nil
	})}}
	key := bf.NativeQuotaKey(bf.CodexProvider, "first")
	var store bf.QuotaStore
	reader := bf.Reader{Store: &store, Fetch: func(ctx context.Context, key string) ([]bf.RateWindow, string, bool, error) {
		w, err := fetchNativeCodex(ctx, source, client, key, time.Now())
		return w, "", true, err
	}, Codex: func(context.Context) ([]bf.RateWindow, error) { t.Fatal("native read invoked CLI"); return nil, nil }}
	q, known, err := reader.Read(context.Background(), key, true, 0)
	if err == nil || known || bf.IsQuotaExhausted(q, known, time.Now()) {
		t.Fatal("changed account was published", q, known, err)
	}
	if _, known := store.Get(key); known {
		t.Fatal("old account read entered cache")
	}
}

func TestNativeCodexFailuresNeverInvokeCLIOrExposeSecrets(t *testing.T) {
	for _, failure := range []string{"enumeration", "auth", "http", "empty", "unauthorized", "expired"} {
		t.Run(failure, func(t *testing.T) {
			source := &quotaAccounts{selected: "first"}
			if failure == "enumeration" {
				source.enumerationErr = errors.New("host secret-first")
			}
			if failure == "auth" {
				source.authErr = errors.New("host secret-first")
			}
			source.expired = failure == "expired"
			client := quota.Client{HTTP: &http.Client{Transport: quotaTransport(func(*http.Request) (*http.Response, error) {
				if source.expired {
					t.Fatal("expired native credential made HTTP request")
				}
				if failure == "http" {
					return nil, errors.New("transport secret-first tenant-first")
				}
				if failure == "empty" {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				}
				return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("secret-first tenant-first"))}, nil
			})}}
			var store bf.QuotaStore
			key := bf.NativeQuotaKey(bf.CodexProvider, "first")
			store.Update(key, time.Now(), func(q *bf.ProviderQuota) { q.Windows = []bf.RateWindow{{Percent: 0, CapturedAt: time.Now()}} })
			reader := bf.Reader{Store: &store, Fetch: func(ctx context.Context, key string) ([]bf.RateWindow, string, bool, error) {
				w, err := fetchNativeCodex(ctx, source, client, key, time.Now())
				return w, "", true, err
			}, Codex: func(context.Context) ([]bf.RateWindow, error) { t.Fatal("native failure invoked CLI"); return nil, nil }}
			q, known, err := reader.Read(context.Background(), key, true, 0)
			if err == nil || known || bf.IsQuotaExhausted(q, known, time.Now()) || strings.Contains(err.Error(), "secret-first") || strings.Contains(err.Error(), "tenant-first") {
				t.Fatal("unsafe native failure", known, err)
			}
		})
	}
}

func TestNativeAccountMetadataFailureDoesNotEnableCLIFallback(t *testing.T) {
	for _, selection := range []string{"", "first", "second", "none"} {
		source := &quotaAccounts{selected: selection}
		key, err := nativeQuotaKey(source, bf.CodexProvider, bf.CodexProvider)
		if selection == "" && (err == nil || key != "") {
			t.Fatal("inactive native accounts enabled CLI fallback")
		}
		if selection == "none" && (err != nil || key != bf.CodexProvider) {
			t.Fatal("no native source should retain CLI fallback")
		}
		if selection == "first" || selection == "second" {
			if err != nil || key != bf.NativeQuotaKey(bf.CodexProvider, selection) {
				t.Fatal("wrong native selector", key, err)
			}
		}
	}
	source := &quotaAccounts{enumerationErr: errors.New("secret")}
	key, err := nativeQuotaKey(source, bf.CodexProvider, bf.CodexProvider)
	if key != "" || err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("unsafe enumeration fallback", key, err)
	}
}

func TestNativeHeaderAttributionAndSwitchInvalidation(t *testing.T) {
	var store bf.QuotaStore
	x := &extension{store: &store, generation: 1}
	first, second := bf.NativeQuotaKey(bf.CodexProvider, "first"), bf.NativeQuotaKey(bf.CodexProvider, "second")
	x.setQuotaKeyLocked(first)
	x.requestKey, x.requestGeneration, x.requestQuotaGeneration = first, x.generation, x.quotaGeneration
	headers := map[string]string{"x-codex-primary-used-percent": "100", "x-codex-primary-reset-after-seconds": "3600"}
	if !x.publishHeaders(headers, 200, time.Now()) {
		t.Fatal("valid native request headers rejected")
	}
	if q, known := store.Get(first); !bf.IsQuotaExhausted(q, known, time.Now()) {
		t.Fatal("namespaced Codex headers used generic rate parser")
	}
	x.setQuotaKeyLocked(second)
	if _, known := store.Get(first); known {
		t.Fatal("old cache survived account switch")
	}
	if x.publishHeaders(headers, 429, time.Now()) {
		t.Fatal("first request was attributed to second account")
	}
	x.setQuotaKeyLocked(first)
	if x.publishHeaders(headers, 429, time.Now()) {
		t.Fatal("old request published after switching back")
	}
	if _, known := store.Get(first); known {
		t.Fatal("old headers exhausted account after switch-back")
	}
}

func TestNativeChatGPTAndGenericHeaderRendering(t *testing.T) {
	var store bf.QuotaStore
	x := &extension{store: &store, generation: 1}
	key := bf.NativeQuotaKey(bf.ChatGPTQuotaKey, "native")
	x.setQuotaKeyLocked(key)
	x.requestKey, x.requestGeneration, x.requestQuotaGeneration = key, 1, x.quotaGeneration
	x.publishHeaders(map[string]string{"x-ratelimit-limit-requests": "10", "x-ratelimit-remaining-requests": "0"}, 200, time.Now())
	q, _ := store.Get(key)
	if len(q.Windows) != 0 {
		t.Fatal("ChatGPT native key accepted API-key rate exhaustion")
	}
	state := bf.RenderState{Provider: "openai", Model: "test", QuotaKey: key, Quota: bf.ProviderQuota{ChatGPTLimitAt: time.Now()}}
	output := strings.Join(bf.RenderFooter(state, 240, bf.Theme{}, time.Now()), "\n")
	if !strings.Contains(output, "ChatGPT limit") || !strings.Contains(output, bf.ChatGPTUsageURL) || strings.Contains(output, "oauth:") {
		t.Fatal("native ChatGPT quota badge changed", output)
	}
	key = bf.NativeQuotaKey("anthropic", "native")
	x.setQuotaKeyLocked(key)
	x.requestKey, x.requestQuotaGeneration = key, x.quotaGeneration
	x.publishHeaders(map[string]string{"anthropic-ratelimit-tokens-limit": "100", "anthropic-ratelimit-tokens-remaining": "50"}, 200, time.Now())
	q, _ = store.Get(key)
	if len(q.Windows) != 1 || q.Windows[0].Percent != 50 {
		t.Fatal("native generic rate parser changed", q)
	}
}
