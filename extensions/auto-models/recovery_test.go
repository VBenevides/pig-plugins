package automodels

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

func TestPrimaryQuotaChecksSelectedCodexAccount(t *testing.T) {
	for _, scenario := range []struct {
		name, body string
		status     int
		want       *bool
		fail       bool
	}{
		{"available", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":20},"secondary_window":{"used_percent":50}}}`, 200, boolPtr(true), false},
		{"weekly exhausted", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":20},"secondary_window":{"used_percent":100}}}`, 200, boolPtr(false), false},
		{"unknown", `{}`, 200, nil, false},
		{"http failure", `private response`, 503, nil, true},
		{"account changed", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":20}}}`, 200, nil, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			source := twoAccounts()
			x := &extension{client: quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("chatgpt-account-id") != "chatgpt-second" {
					t.Fatal("did not check selected primary account")
				}
				if scenario.name == "account changed" {
					source.accounts[0].Active = true
					source.accounts[1].Active = false
				}
				return &http.Response{StatusCode: scenario.status, Body: io.NopCloser(strings.NewReader(scenario.body))}, nil
			})}}}
			got, err := x.primaryAvailable(context.Background(), source, "openai-codex")
			if (err != nil) != scenario.fail {
				t.Fatalf("error = %v", err)
			}
			if (got == nil) != (scenario.want == nil) || got != nil && *got != *scenario.want {
				t.Fatalf("availability = %v, want %v", got, scenario.want)
			}
			if strings.Join(source.reads, ",") != "local-second" {
				t.Fatal("read another account's credentials", source.reads)
			}
		})
	}
}

func boolPtr(value bool) *bool { return &value }

func TestPrimaryQuotaUnknownAndCancellation(t *testing.T) {
	x := &extension{client: quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatal("unexpected request for unknown or canceled quota")
		return nil, errors.New("unexpected request")
	})}}}
	for _, provider := range []string{"openai", "unsupported"} {
		got, err := x.primaryAvailable(context.Background(), twoAccounts(), provider)
		if got != nil || err != nil {
			t.Fatal(got, err)
		}
	}
	got, err := x.primaryAvailable(context.Background(), &fakeAccounts{}, "openai-codex")
	if got != nil || err != nil {
		t.Fatal(got, err)
	}
	run, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = x.primaryAvailable(run, twoAccounts(), "openai-codex")
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
}

func TestRecoveryClearsPersistedCooldownAndSnapshot(t *testing.T) {
	store := quota.Store{Dir: t.TempDir()}
	if err := store.SetRateLimit("openai-codex", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	x := &extension{store: store, rates: map[string]quota.RateLimitInfo{
		"openai-codex": {TokensRemaining: "0", TokensReset: "1h"},
		"anthropic":    {Status: "allowed"},
	}}
	if err := x.clearPrimaryCooldown("openai-codex"); err != nil {
		t.Fatal(err)
	}
	left, err := store.RateLimitLeft("openai-codex", time.Now())
	if err != nil || left != 0 {
		t.Fatal(left, err)
	}
	rates, err := store.LoadRateLimits()
	if err != nil || len(rates) != 1 || rates["anthropic"].Status != "allowed" {
		t.Fatal(rates, err)
	}
	if err := x.clearPrimaryCooldown("openai-codex"); err != nil {
		t.Fatal("repeated recovery must be idempotent", err)
	}
}
