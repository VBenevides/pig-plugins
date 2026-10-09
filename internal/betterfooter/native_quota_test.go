package betterfooter

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNativeQuotaSourceMapping(t *testing.T) {
	for _, base := range []string{CodexProvider, CopilotProvider, ChatGPTQuotaKey, "zai", OpenCodeGoID, "anthropic"} {
		key := NativeQuotaKey(base, "first:opaque:id")
		if QuotaSource(key) != base || Polled(key) != Polled(base) || PollInterval(key) != PollInterval(base) {
			t.Fatal("native namespace changed source behavior", base)
		}
		if id, native := NativeQuotaAccount(key); !native || id != "first:opaque:id" {
			t.Fatal("opaque ID changed", id, native)
		}
	}
	if _, native := NativeQuotaAccount(CodexProvider); native {
		t.Fatal("CLI key became native")
	}
}

func TestNativeCodexWithoutOverrideNeverRunsCLI(t *testing.T) {
	var store QuotaStore
	calls := 0
	reader := Reader{Store: &store, Codex: func(context.Context) ([]RateWindow, error) { calls++; return []RateWindow{{Percent: 0}}, nil }}
	q, known, err := reader.Read(context.Background(), NativeQuotaKey(CodexProvider, "native"), true, 0)
	if err == nil || known || calls != 0 || IsQuotaExhausted(q, known, time.Now()) {
		t.Fatal("native quota fell back to CLI", calls, known, err)
	}
	_, known, err = reader.Read(context.Background(), CodexProvider, true, 0)
	if err != nil || !known || calls != 1 {
		t.Fatal("no-native-source CLI fallback changed", calls, known, err)
	}
}

func TestNativeQuotaInvalidationRejectsOlderRead(t *testing.T) {
	var store QuotaStore
	key := NativeQuotaKey(CodexProvider, "first")
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	reader := Reader{Store: &store, Fetch: func(context.Context, string) ([]RateWindow, string, bool, error) {
		close(started)
		<-release
		return []RateWindow{{Percent: 0, CapturedAt: time.Now()}}, "", true, nil
	}}
	go func() { defer close(done); reader.Read(context.Background(), key, true, 0) }()
	<-started
	store.Invalidate(key)
	close(release)
	<-done
	if q, known := store.Get(key); known || IsQuotaExhausted(q, known, time.Now()) {
		t.Fatal("old read survived account invalidation", q)
	}
}

func TestFailedNativeRefreshKeepsLastValue(t *testing.T) {
	key := NativeQuotaKey(CodexProvider, "first")
	var store QuotaStore
	store.Update(key, time.Now(), func(q *ProviderQuota) { q.Windows = []RateWindow{{Percent: 42, CapturedAt: time.Now()}} })
	reader := Reader{Store: &store, Fetch: func(context.Context, string) ([]RateWindow, string, bool, error) {
		return nil, "", true, errors.New("HTTP 429")
	}}
	q, known, err := reader.Read(context.Background(), key, true, 0)
	if err == nil || !known || len(q.Windows) != 1 || q.Windows[0].Percent != 42 {
		t.Fatal("failed native refresh did not keep the last value", q, known, err)
	}
}
