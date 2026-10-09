package automodels

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

func TestStartupRevalidatesCachedCooldown(t *testing.T) {
	for _, test := range []struct {
		name, body                          string
		status                              int
		available, wantError, changeAccount bool
	}{
		{name: "available", body: `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":30},"secondary_window":{"used_percent":50}}}`, available: true},
		{name: "weekly exhausted", body: `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":30},"secondary_window":{"used_percent":100}}}`},
		{name: "denied", body: `{"rate_limit":{"allowed":false,"primary_window":{"used_percent":30}}}`},
		{name: "unknown", body: `{}`},
		{name: "request failed", status: 429, body: `{}`, wantError: true},
		{name: "account changed", body: `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":30}}}`, changeAccount: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			source := twoAccounts()
			store := quota.Store{Dir: t.TempDir()}
			if err := store.SetRateLimit("openai-codex", now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			x := &extension{store: store, primary: quota.Slot{Provider: "openai-codex", Model: "main"}, rates: map[string]quota.RateLimitInfo{"openai-codex": {Status: "rate_limited"}}}
			if err := store.SaveRateLimits(x.rates); err != nil {
				t.Fatal(err)
			}
			x.life = t.Context()
			x.client = quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("chatgpt-account-id") != "chatgpt-second" {
					t.Fatal("queried wrong account")
				}
				if test.changeAccount {
					source.accounts[0].Active, source.accounts[1].Active = true, false
				}
				status := test.status
				if status == 0 {
					status = 200
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}}
			left, err := x.startupCooldown(source, x.primary, now)
			if (err != nil) != test.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
			if (left == 0) != test.available {
				t.Fatalf("cooldown=%v, available=%v", left, test.available)
			}
			persisted, err := store.RateLimitLeft("openai-codex", now)
			if err != nil || (persisted == 0) != test.available {
				t.Fatalf("persisted=%v, err=%v", persisted, err)
			}
			if (x.rate("openai-codex") == nil) != test.available {
				t.Fatal("incorrect cached snapshot")
			}
			rates, err := store.LoadRateLimits()
			if err != nil {
				t.Fatal(err)
			}
			_, cached := rates["openai-codex"]
			if cached == test.available {
				t.Fatal("incorrect durable snapshot")
			}
		})
	}
}

func TestStartupWithoutCooldownDoesNotQueryQuota(t *testing.T) {
	x, source := modelSwitchFixture(t, 30, 30)
	left, err := x.startupCooldown(source, x.primary, time.Now())
	if err != nil || left != 0 || len(source.reads) != 0 {
		t.Fatal(left, err, source.reads)
	}
}
