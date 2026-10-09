package automodels

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

type fakeSwitcher struct {
	*fakeAccounts
	selected string
	notices  []string
	slot     quota.Slot
	answer   bool
	prompts  int
	primary  bool
}

func (f *fakeSwitcher) ModelProvider() string {
	if f.slot.Provider != "" {
		return f.slot.Provider
	}
	return "openai-codex"
}
func (f *fakeSwitcher) SelectOAuthAccount(id string) error {
	f.selected = id
	for i := range f.accounts {
		f.accounts[i].Active = f.accounts[i].ID == id
	}
	return nil
}
func (f *fakeSwitcher) Notify(text, level string)                 { f.notices = append(f.notices, text) }
func (f *fakeSwitcher) Model() string                             { return f.slot.Model }
func (f *fakeSwitcher) IsIdle() (bool, error)                     { return true, nil }
func (f *fakeSwitcher) Confirm(string, string) (bool, error)      { f.prompts++; return f.answer, nil }
func (f *fakeSwitcher) chooseModel(slot quota.Slot) (bool, error) { f.slot = slot; return true, nil }
func (f *fakeSwitcher) markSelected(primary bool)                 { f.primary = primary }

func TestRotateSameProviderAccount(t *testing.T) {
	for _, test := range []struct {
		name, body, failure string
		want                bool
	}{
		{"available", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":30},"secondary_window":{"used_percent":50}}}`, "", true},
		{"weekly low", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":30},"secondary_window":{"used_percent":96}}}`, "", false},
		{"short low", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":96}}}`, "", false},
		{"exactly five", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":95}}}`, "", true},
		{"unknown", `{}`, "", false},
		{"credential failure", `{}`, "local-first", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := &fakeSwitcher{fakeAccounts: twoAccounts()}
			source.fail = test.failure
			source.accounts = append(source.accounts, sdk.OAuthAccount{ID: "other-provider", Provider: "anthropic"})
			x := &extension{life: context.Background(), store: quota.Store{Dir: t.TempDir()}, rates: map[string]quota.RateLimitInfo{}, client: quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("chatgpt-account-id") != "chatgpt-first" {
					t.Fatal("queried wrong account")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}}}
			switched, err := x.rotateAccount(source, source.accounts[1])
			if err != nil || switched != test.want {
				t.Fatalf("switched=%v, err=%v", switched, err)
			}
			if test.want && source.selected != "local-first" {
				t.Fatalf("selected=%q", source.selected)
			}
			if !test.want && source.selected != "" {
				t.Fatal("selected unavailable account")
			}
			if strings.Join(source.reads, ",") != "local-first" {
				t.Fatalf("queried active or cross-provider account: %v", source.reads)
			}
			if strings.Contains(strings.Join(source.notices, "\n"), "SECRET") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestAccountRotationContinuesAfterFailedNeighbor(t *testing.T) {
	source := &fakeSwitcher{fakeAccounts: twoAccounts()}
	source.fail = "broken"
	source.accounts = append([]sdk.OAuthAccount{{ID: "broken", Provider: "openai-codex"}}, source.accounts...)
	x := &extension{life: context.Background(), store: quota.Store{Dir: t.TempDir()}, rates: map[string]quota.RateLimitInfo{}, client: quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":10}}}`))}, nil
	})}}}
	switched, err := x.rotateAccount(source, source.accounts[2])
	if err != nil || !switched || source.selected != "local-first" {
		t.Fatal(switched, err, source.selected)
	}
	if len(source.notices) != 2 {
		t.Fatal("failure not observable", source.notices)
	}
}

func modelSwitchFixture(t *testing.T, primaryUsed, fallbackUsed float64) (*extension, *fakeSwitcher) {
	t.Helper()
	source := &fakeSwitcher{fakeAccounts: twoAccounts(), answer: true}
	source.accounts = source.accounts[1:]
	source.accounts = append(source.accounts, sdk.OAuthAccount{ID: "claude", Provider: "anthropic", Active: true})
	source.auth["claude"] = map[string]any{"type": "oauth", "access": "CLAUDE-TEST", "expires": float64(9999999999999)}
	primary := quota.Slot{Provider: "openai-codex", Model: "main"}
	fallback := quota.Slot{Provider: "anthropic", Model: "backup"}
	source.slot = primary
	x := &extension{primary: primary, fallback: fallback, autoEnabled: true, life: context.Background(), store: quota.Store{Dir: t.TempDir()}, rates: map[string]quota.RateLimitInfo{}, client: quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":%g}}}`, primaryUsed)
		if r.Header.Get("Authorization") == "Bearer CLAUDE-TEST" {
			body = fmt.Sprintf(`{"limits":[{"kind":"session","percent":%g},{"kind":"weekly_all","percent":%g}]}`, fallbackUsed, fallbackUsed)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}}
	return x, source
}

func TestLowQuotaFallbackConsent(t *testing.T) {
	for _, test := range []struct {
		name         string
		main, backup float64
		consent      bool
		prompts      int
		switched     bool
	}{
		{"accept", 96, 20, true, 1, true},
		{"decline", 96, 20, false, 1, false},
		{"both low", 96, 97, true, 0, false},
		{"boundary", 95, 20, true, 0, false},
		{"fallback boundary", 96, 95, true, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			x, source := modelSwitchFixture(t, test.main, test.backup)
			source.answer = test.consent
			account := source.accounts[0]
			remaining, err := x.accountRemaining(x.life, source, account)
			if err != nil {
				t.Fatal(err)
			}
			if err := x.lowQuota(source, account, remaining); err != nil {
				t.Fatal(err)
			}
			if source.prompts != test.prompts || (source.slot == x.fallback) != test.switched {
				t.Fatalf("prompts=%d slot=%+v", source.prompts, source.slot)
			}
			if !test.switched {
				if err := x.lowQuota(source, account, remaining); err != nil {
					t.Fatal(err)
				}
				if source.prompts != test.prompts {
					t.Fatal("repeated declined prompt")
				}
			}
		})
	}
}

func TestQuotaFailureSwitchesBothDirectionsAndBoundsRetries(t *testing.T) {
	for _, fromPrimary := range []bool{true, false} {
		t.Run(fmt.Sprint(fromPrimary), func(t *testing.T) {
			x, source := modelSwitchFixture(t, 99, 99)
			target := x.fallback
			if !fromPrimary {
				source.slot = x.fallback
				target = x.primary
			}
			if err := x.switchOnLimit(source, source.ModelProvider()); err != nil {
				t.Fatal(err)
			}
			if source.slot != target || !x.retryPending || !x.retryUsed || source.primary == fromPrimary {
				t.Fatalf("slot=%+v pending=%v used=%v", source.slot, x.retryPending, x.retryUsed)
			}
			if source.prompts != 0 {
				t.Fatal("quota failure asked for consent")
			}
			if err := x.switchOnLimit(source, source.ModelProvider()); err != nil {
				t.Fatal(err)
			}
			if source.slot != target {
				t.Fatal("unbounded model ping-pong")
			}
		})
	}
}

func TestQuotaFailureKeepsModelWhenOtherExhausted(t *testing.T) {
	x, source := modelSwitchFixture(t, 100, 100)
	if err := x.switchOnLimit(source, source.ModelProvider()); err != nil {
		t.Fatal(err)
	}
	if source.slot != x.primary || x.retryPending {
		t.Fatal("switched to exhausted fallback")
	}
}

func TestSuccessfulMessageCancelsPendingContinuation(t *testing.T) {
	x := &extension{retryPending: true, retryUsed: true}
	_, err := x.messageEnd(sdk.Context{}, map[string]any{"message": map[string]any{"role": "assistant", "stopReason": "stop"}})
	if err != nil || x.retryPending || x.retryUsed {
		t.Fatal("successful retry left continuation pending", err)
	}
}
