package automodels

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

type limitSwitcher struct {
	*fakeSwitcher
	busy           bool
	selectionError bool
}

func (f *limitSwitcher) IsIdle() (bool, error) { return !f.busy, nil }
func (f *limitSwitcher) SelectOAuthAccount(id string) error {
	if f.selectionError {
		return errors.New("credential selection failed")
	}
	return f.fakeSwitcher.SelectOAuthAccount(id)
}

func TestQuotaFailureRotatesCodexBeforeClaude(t *testing.T) {
	for _, busy := range []bool{false, true} {
		x, base := modelSwitchFixture(t, 100, 20)
		base.accounts = append([]sdk.OAuthAccount{{ID: "local-first", Provider: "openai-codex"}}, base.accounts...)
		x.client = quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") == "Bearer CLAUDE-TEST" {
				t.Fatal("queried Claude before eligible Codex account")
			}
			if r.Header.Get("chatgpt-account-id") != "chatgpt-first" {
				t.Fatal("queried wrong Codex account")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":0}}}`))}, nil
		})}}
		source := &limitSwitcher{fakeSwitcher: base, busy: busy}
		if err := x.switchOnLimit(source, "openai-codex"); err != nil {
			t.Fatal(err)
		}
		if source.selected != "local-first" || source.slot != x.primary || !x.retryPending || !x.retryUsed || x.retryTarget != x.primary {
			t.Fatalf("busy=%v selected=%s slot=%+v pending=%v used=%v", busy, source.selected, source.slot, x.retryPending, x.retryUsed)
		}
		if err := x.switchOnLimit(source, "openai-codex"); err != nil {
			t.Fatal(err)
		}
		if source.slot != x.primary {
			t.Fatal("repeated failure bypassed retry budget")
		}
	}
}

func TestQuotaFailureRotationRespectsPolicyAndErrors(t *testing.T) {
	for _, scenario := range []string{"explicit model", "different model", "selection failure", "unknown quota", "exhausted quota"} {
		t.Run(scenario, func(t *testing.T) {
			x, base := modelSwitchFixture(t, 100, 20)
			base.accounts = append([]sdk.OAuthAccount{{ID: "local-first", Provider: "openai-codex"}}, base.accounts...)
			source := &limitSwitcher{fakeSwitcher: base}
			switch scenario {
			case "explicit model":
				x.autoEnabled = false
			case "different model":
				source.slot.Model = "manually-selected"
			case "selection failure":
				source.selectionError = true
			}
			x.client = quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":0}}}`
				if scenario == "unknown quota" {
					body = `{}`
				}
				if scenario == "exhausted quota" {
					body = `{"rate_limit":{"allowed":false,"limit_reached":true,"primary_window":{"used_percent":100}}}`
				}
				if r.Header.Get("Authorization") == "Bearer CLAUDE-TEST" {
					body = `{"limits":[{"kind":"session","percent":20}]}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			err := x.switchOnLimit(source, "openai-codex")
			if (err != nil) != source.selectionError {
				t.Fatalf("err=%v", err)
			}
			if source.selected != "" {
				t.Fatal("selected an ineligible account")
			}
			fallback := scenario == "unknown quota" || scenario == "exhausted quota"
			if (source.slot == x.fallback) != fallback {
				t.Fatalf("slot=%+v", source.slot)
			}
		})
	}
}

func TestQuotaFailureRotationPreservesFallbackAndCooldownState(t *testing.T) {
	x, source := modelSwitchFixture(t, 0, 20)
	// Codex is the default fallback; rotation must retain that model identity.
	x.primary, x.fallback = x.fallback, x.primary
	source.accounts = append([]sdk.OAuthAccount{{ID: "local-first", Provider: "openai-codex"}}, source.accounts...)
	if err := x.store.SetRateLimit("openai-codex", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := x.switchOnLimit(source, "openai-codex"); err != nil {
		t.Fatal(err)
	}
	if source.selected != "local-first" || source.slot != x.fallback {
		t.Fatalf("selected=%s slot=%+v", source.selected, source.slot)
	}
	if left, err := x.store.RateLimitLeft("openai-codex", time.Now()); err != nil || left != 0 {
		t.Fatalf("new account inherited failed account cooldown: %v %v", left, err)
	}
}

func TestQuotaFailureRotationDoesNotOverwriteChangedModel(t *testing.T) {
	x, source := modelSwitchFixture(t, 100, 20)
	source.accounts = append([]sdk.OAuthAccount{{ID: "local-first", Provider: "openai-codex"}}, source.accounts...)
	x.client = quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
		source.slot.Model = "manual-change-during-fetch"
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":0}}}`))}, nil
	})}}
	if err := x.switchOnLimit(source, "openai-codex"); err != nil {
		t.Fatal(err)
	}
	if source.selected != "" || source.slot.Model != "manual-change-during-fetch" || x.retryPending {
		t.Fatal("stale quota fetch overwrote manual selection")
	}
}
