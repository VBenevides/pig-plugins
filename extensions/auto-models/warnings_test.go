package automodels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

func TestWarningCoalescingRemindersAndBound(t *testing.T) {
	x := &extension{}
	now := time.Now()
	if allowed, repeats := x.admitWarning("expired", now); !allowed || repeats != 0 {
		t.Fatal(allowed, repeats)
	}
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if allowed, _ := x.admitWarning("expired", now.Add(time.Second)); allowed {
				t.Error("repeated warning admitted")
			}
		}()
	}
	wg.Wait()
	if allowed, repeats := x.admitWarning("expired", now.Add(warningInterval)); !allowed || repeats != 100 {
		t.Fatal(allowed, repeats)
	}
	if allowed, _ := x.admitWarning("different failure", now); !allowed {
		t.Fatal("new failure hidden")
	}
	for i := range 2 * warningHistoryLimit {
		x.admitWarning(fmt.Sprint(i), now.Add(time.Duration(i)*time.Second))
		if len(x.warnings) > warningHistoryLimit {
			t.Fatal("unbounded history", len(x.warnings))
		}
	}
}

func TestReportCoalescesModelWarning(t *testing.T) {
	x := &extension{}
	source := &fakeSwitcher{}
	err := errors.New("model selection failed")
	for range 100 {
		x.report(source, err)
	}
	x.report(source, nil)
	if len(source.notices) != 1 || source.notices[0] != "auto-models: "+err.Error() {
		t.Fatalf("notices=%v", source.notices)
	}
}

func TestRotationCoalescesExpiredNeighborsAndStillSwitches(t *testing.T) {
	source := &fakeSwitcher{fakeAccounts: twoAccounts()}
	active := source.accounts[1]
	for i := range 100 {
		id := fmt.Sprintf("expired-%d", i)
		source.accounts = append([]sdk.OAuthAccount{{ID: id, Provider: "openai-codex"}}, source.accounts...)
		source.auth[id] = map[string]any{"type": "oauth", "access": "SECRET-EXPIRED", "expires": float64(1)}
	}
	requests := 0
	x := &extension{life: context.Background(), store: quota.Store{Dir: t.TempDir()}, rates: map[string]quota.RateLimitInfo{}, client: quota.Client{HTTP: &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if strings.Contains(r.Header.Get("Authorization"), "SECRET-EXPIRED") {
			t.Fatal("queried expired account")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":10}}}`))}, nil
	})}}}
	switched, err := x.rotateAccount(source, active)
	if err != nil || !switched || source.selected != "local-first" || requests != 1 {
		t.Fatal(switched, err, source.selected, requests)
	}
	if len(source.notices) != 1 || !strings.HasPrefix(source.notices[0], "Switched to another") {
		t.Fatalf("notices=%v", source.notices)
	}
	if strings.Contains(strings.Join(source.notices, "\n"), "SECRET") {
		t.Fatal("credential leaked")
	}
}
