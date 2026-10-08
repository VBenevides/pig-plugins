package automodels

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
)

type fakeAccounts struct {
	accounts []sdk.OAuthAccount
	auth     map[string]map[string]any
	fail     string
	reads    []string
}

func (f *fakeAccounts) OAuthAccounts() ([]sdk.OAuthAccount, error) { return f.accounts, nil }
func (f *fakeAccounts) OAuthAccountAuth(id string) (map[string]any, error) {
	f.reads = append(f.reads, id)
	if id == f.fail {
		return nil, errors.New("host failure contains SECRET-FIRST")
	}
	return f.auth[id], nil
}

type accountTransport func(*http.Request) (*http.Response, error)

func (f accountTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func twoAccounts() *fakeAccounts {
	return &fakeAccounts{
		accounts: []sdk.OAuthAccount{
			{ID: "local-first", Provider: "openai-codex", Label: "Personal"},
			{ID: "local-second", Provider: "openai-codex", Label: "Work", Active: true},
		},
		auth: map[string]map[string]any{
			"local-first":  {"type": "oauth", "access": "SECRET-FIRST", "accountId": "chatgpt-first", "expires": float64(9999999999999)},
			"local-second": {"type": "oauth", "access": "SECRET-SECOND", "accountId": "chatgpt-second", "expires": float64(9999999999999)},
		},
	}
}

func TestNativeTwoCodexAccountsQuotaAndFooter(t *testing.T) {
	for _, failure := range []string{"none", "401", "credential"} {
		t.Run(failure, func(t *testing.T) {
			source := twoAccounts()
			if failure == "credential" {
				source.fail = "local-first"
			}
			var requested []string
			x := &extension{store: quota.Store{Dir: t.TempDir()}, rates: map[string]quota.RateLimitInfo{
				"openai-codex": {Utilization: "0.93", Status: "allowed", CapturedAt: float64(time.Now().UnixMilli())},
			}}
			x.client = quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
				id := r.Header.Get("chatgpt-account-id")
				requested = append(requested, id)
				percent := "27"
				if id == "chatgpt-first" {
					if r.Header.Get("Authorization") != "Bearer SECRET-FIRST" {
						t.Fatal("first identity mismatch")
					}
					if failure == "401" {
						return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("SECRET-FIRST"))}, nil
					}
					percent = "11"
				} else if id != "chatgpt-second" || r.Header.Get("Authorization") != "Bearer SECRET-SECOND" {
					t.Fatalf("wrong second identity: %q", id)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":` + percent + `,"limit_window_seconds":18000,"reset_after_seconds":3600,"reset_at":1999999999},"secondary_window":{"used_percent":35,"limit_window_seconds":604800,"reset_after_seconds":7200,"reset_at":1999999999}}}`))}, nil
			})}}
			lines, err := x.usageLines(context.Background(), source, "openai-codex", "SOL")
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(lines, "\n")
			for _, want := range []string{"Personal", "Work [active]", "27%", "35%", "5h", "Weekly"} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q: %s", want, text)
				}
			}
			if strings.Count(text, "Active model: openai-codex/SOL") != 1 {
				t.Fatal("active model header must appear once")
			}
			if strings.Contains(text, "SECRET") || strings.Contains(text, "93%") {
				t.Fatalf("secret or provider cache leaked: %s", text)
			}
			_, first, _ := strings.Cut(text, "Personal")
			first, _, _ = strings.Cut(first, "── Account")
			if failure == "none" {
				if !strings.Contains(first, "11%") {
					t.Fatalf("first quota missing: %s", first)
				}
			} else if !strings.Contains(first, "Quota unknown") || strings.Contains(first, "Quota available") || !strings.Contains(first, "Failed to fetch quota") {
				t.Fatalf("failure not isolated: %s", first)
			}
			account, ok, err := selectedAccount(source, "openai-codex")
			if err != nil || !ok || account.ID != "local-second" {
				t.Fatalf("selection: %+v %v", account, err)
			}
			status, err := x.accountStatus(context.Background(), source, account)
			if err != nil || status == nil || status.Percent != 27 {
				t.Fatalf("footer used wrong account: %+v %v", status, err)
			}
			if requested[len(requested)-1] != "chatgpt-second" {
				t.Fatal("footer queried unselected account")
			}
			if failure == "none" {
				source.accounts[0].Active, source.accounts[1].Active = true, false
				account, ok, err = selectedAccount(source, "openai-codex")
				if err != nil || !ok || account.ID != "local-first" {
					t.Fatalf("changed native selection: %+v %v", account, err)
				}
				status, err = x.accountStatus(context.Background(), source, account)
				if err != nil || status == nil || status.Percent != 11 {
					t.Fatalf("footer ignored native selection: %+v %v", status, err)
				}
			}
		})
	}
}

func TestNativeAccountCancellationAndUnsupportedOpenAI(t *testing.T) {
	source := twoAccounts()
	x := &extension{store: quota.Store{Dir: t.TempDir()}}
	run, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := x.usageLines(run, source, "openai-codex", "SOL"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if len(source.reads) != 0 {
		t.Fatal("cancelled request read credentials")
	}
	source.accounts = []sdk.OAuthAccount{{ID: "local-first", Provider: "openai", Label: "API login", Active: true}}
	x.client = quota.Client{HTTP: &http.Client{Transport: accountTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("direct OpenAI queried Codex quota")
		return nil, nil
	})}}
	lines, err := x.usageLines(context.Background(), source, "openai", "SOL")
	text := strings.Join(lines, "\n")
	if err != nil || !strings.Contains(text, "Quota unknown") || !strings.Contains(text, "Codex quota is separate") || strings.Contains(text, "Quota available") {
		t.Fatalf("unsupported login: %s %v", text, err)
	}
}
