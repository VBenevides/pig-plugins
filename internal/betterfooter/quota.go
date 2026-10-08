package betterfooter

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// QuotaCacheAge is how long a read snapshot is reused before the account is asked again.
	QuotaCacheAge = 60 * time.Second
	// UntimedWindowTrust is how long a window without a reset time proves exhaustion.
	UntimedWindowTrust = 5 * time.Minute
	// ChatGPTLimitTrust is how long a ChatGPT app-limit denial counts: it proves this app is limited, not that
	// the whole plan is empty.
	ChatGPTLimitTrust = 5 * time.Minute
)

// ProviderQuota is what is known about one quota account, including native account keys.
type ProviderQuota struct {
	Windows        []RateWindow
	CopilotCredits string
	// ChatGPTLimitAt is the time of a live ChatGPT app-limit denial; zero when there is none.
	ChatGPTLimitAt time.Time
	UpdatedAt      time.Time
}

// QuotaStore keeps the snapshots shared by the footer and model cycling.
type QuotaStore struct {
	mu       sync.Mutex
	quotas   map[string]ProviderQuota
	versions map[string]uint64
}

// Get returns the snapshot of key.
func (s *QuotaStore) Get(key string) (ProviderQuota, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.quotas[key]
	q.Windows = slices.Clone(q.Windows)
	return q, ok
}

// Update applies change to the snapshot of key (creating it), stamps it with now and marks it newer than any
// read that started earlier.
func (s *QuotaStore) Update(key string, now time.Time, change func(*ProviderQuota)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quotas == nil {
		s.quotas, s.versions = map[string]ProviderQuota{}, map[string]uint64{}
	}
	q := s.quotas[key]
	change(&q)
	q.UpdatedAt = now
	s.quotas[key] = q
	s.versions[key]++
}

func (s *QuotaStore) version(key string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.versions[key]
}

// Invalidate removes a snapshot and rejects any read that started before this call.
func (s *QuotaStore) Invalidate(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.versions == nil {
		s.quotas, s.versions = map[string]ProviderQuota{}, map[string]uint64{}
	}
	delete(s.quotas, key)
	s.versions[key]++
}

// invalidateRead drops failed native observations without discarding newer headers.
func (s *QuotaStore) invalidateRead(key string, version uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.versions[key] != version {
		return
	}
	if s.versions == nil {
		s.quotas, s.versions = map[string]ProviderQuota{}, map[string]uint64{}
	}
	delete(s.quotas, key)
	s.versions[key]++
}

// applyRead compares and publishes under one lock, so a response-header update
// cannot be overwritten between a version check and publication.
func (s *QuotaStore) applyRead(key string, version uint64, now time.Time, windows []RateWindow, credits string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.versions[key] != version || len(windows) == 0 && credits == "" {
		return
	}
	if s.quotas == nil {
		s.quotas, s.versions = map[string]ProviderQuota{}, map[string]uint64{}
	}
	q := s.quotas[key]
	if len(windows) > 0 {
		q.Windows = windows
	}
	if credits != "" {
		q.CopilotCredits = credits
	}
	q.UpdatedAt = now
	s.quotas[key] = q
	s.versions[key]++
}

// HasRecentChatGPTLimit reports a denial younger than [ChatGPTLimitTrust].
func HasRecentChatGPTLimit(at, now time.Time) bool {
	return !at.IsZero() && !now.Before(at) && now.Sub(at) < ChatGPTLimitTrust
}

var chatGPTLimitError = regexp.MustCompile(`\bsubscription_sharing_usage_limit_exceeded\b`)

// IsChatGPTLimitError tells a ChatGPT plan denial from a temporary failure or an ordinary API rate limit.
func IsChatGPTLimitError(message string) bool { return chatGPTLimitError.MatchString(message) }

// QuotaKey is the account a model's quota is cached under: its provider, except that the ChatGPT sign-in (the
// "openai" provider on api.openai.com through OAuth) has its own. A virtual model can route anywhere, so it has
// none (""). usingOAuth is only called for the OpenAI provider.
func QuotaKey(api, provider, baseURL string, usingOAuth func() bool) string {
	if provider == "" || api == "pi-virtual" {
		return ""
	}
	if provider == "openai" && hostOf(baseURL) == "api.openai.com" && usingOAuth != nil && usingOAuth() {
		return ChatGPTQuotaKey
	}
	return provider
}

// NativeQuotaKey isolates a native credential from both other logins and CLI sources.
// The opaque local ID is never displayed or persisted by the footer.
func NativeQuotaKey(base, id string) string { return base + "\x00oauth:" + id }

// QuotaSource identifies the provider-specific parser/source of an account key.
func QuotaSource(key string) string {
	base, _, _ := strings.Cut(key, "\x00oauth:")
	return base
}

// NativeQuotaAccount returns the opaque native selector, not a provider account ID.
func NativeQuotaAccount(key string) (string, bool) {
	_, id, native := strings.Cut(key, "\x00oauth:")
	return id, native
}

var creditsPattern = regexp.MustCompile(`^(\d+)/`)

// IsQuotaExhausted reports that the account provably cannot serve a request now. Missing, stale or failed
// readings never count as exhausted: a timed window proves exhaustion only until its reset, an untimed one only
// while its reading is fresh, and advisory windows never.
func IsQuotaExhausted(q ProviderQuota, known bool, now time.Time) bool {
	if !known {
		return false
	}
	if HasRecentChatGPTLimit(q.ChatGPTLimitAt, now) {
		return true
	}
	// Every Copilot model consumes premium requests, so "0/N" blocks the whole provider.
	if m := creditsPattern.FindStringSubmatch(q.CopilotCredits); m != nil {
		if n, _ := strconv.Atoi(m[1]); n <= 0 {
			return true
		}
	}
	return slices.ContainsFunc(q.Windows, func(w RateWindow) bool {
		if w.Advisory || w.Percent > 0 {
			return false
		}
		if !w.HasReset {
			return now.Sub(w.CapturedAt) < UntimedWindowTrust
		}
		return w.Remaining(now) > 0
	})
}

// Registry is what the quota readers need to know about the host's providers.
type Registry interface {
	// BaseURLs are the base URLs of the provider's registered models.
	BaseURLs(provider string) []string
	// APIKey is Pi's credential for the provider, or "" when there is none.
	APIKey(provider string) string
}

// Reader reads account quotas from their provider-specific sources into a [QuotaStore], coalescing concurrent
// reads of one account. Zero values of the optional fields mean production defaults.
type Reader struct {
	Store    *QuotaStore
	Registry Registry
	// AuthPath is Pi's auth.json (the Copilot token lives there).
	AuthPath string
	Env      Environment
	HTTP     *http.Client
	Now      func() time.Time
	// Fetch overrides an account source before any CLI/file fallback. handled must
	// remain true on errors when a native account owns the source.
	Fetch func(context.Context, string) (windows []RateWindow, credits string, handled bool, err error)
	// Codex reads the Codex CLI rate limits; nil uses [ReadCodexRateLimits].
	Codex func(ctx context.Context) ([]RateWindow, error)
	// OpenCodeGo overrides the OpenCode Go URLs; the zero value means production.
	OpenCodeGo OpenCodeGoEndpoints
	// ZaiURL overrides the Z.AI monitor URL derived from the models' base URLs (tests only).
	ZaiURL string
	// CopilotBase overrides the Copilot API base derived from auth.json (tests only).
	CopilotBase string

	mu       sync.Mutex
	inFlight map[string]*read
}

type read struct {
	done chan struct{}
	err  error
}

func (r *Reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Polled reports whether the account has an own quota source worth polling.
func Polled(key string) bool {
	key = QuotaSource(key)
	return key == CopilotProvider || key == CodexProvider || IsZaiProvider(key) || IsOpenCodeGoProvider(key)
}

// PollInterval is the base polling cadence of an account: Copilot 30 s, the others 60 s; 0 when it has no source.
func PollInterval(key string) time.Duration {
	key = QuotaSource(key)
	switch {
	case key == CopilotProvider:
		return 30 * time.Second
	case Polled(key):
		return 60 * time.Second
	}
	return 0
}

// BackoffInterval stretches a polling interval while the user is idle: 10 minutes after 10 idle minutes, hourly
// after an idle hour. Account quota only moves while Pi is used.
func BackoffInterval(base, idle time.Duration) time.Duration {
	switch {
	case idle >= time.Hour:
		return max(base, time.Hour)
	case idle >= 10*time.Minute:
		return max(base, 10*time.Minute)
	}
	return base
}

// Read returns the snapshot of key, reading the account again when the cached one is older than maxAge or force
// is set. Failed legacy reads keep the previous snapshot; failed native reads discard
// it unless newer headers have arrived. Accounts without a source return the cached
// (header-derived) snapshot untouched. Callers must treat read errors as unknown quota.
func (r *Reader) Read(ctx context.Context, key string, force bool, maxAge time.Duration) (ProviderQuota, bool, error) {
	cached, known := r.Store.Get(key)
	if !force && known && r.now().Sub(cached.UpdatedAt) < maxAge {
		return cached, true, nil
	}
	if !Polled(key) {
		return cached, known, nil
	}
	r.mu.Lock()
	if r.inFlight == nil {
		r.inFlight = map[string]*read{}
	}
	if pending := r.inFlight[key]; pending != nil {
		r.mu.Unlock()
		select {
		case <-pending.done:
		case <-ctx.Done():
			return cached, known, ctx.Err()
		}
		q, ok := r.Store.Get(key)
		return q, ok, pending.err
	}
	mine := &read{done: make(chan struct{})}
	r.inFlight[key] = mine
	r.mu.Unlock()

	version := r.Store.version(key)
	windows, credits, err := r.fetch(ctx, key)
	if err == nil {
		r.Store.applyRead(key, version, r.now(), windows, credits)
	} else if _, native := NativeQuotaAccount(key); native {
		r.Store.invalidateRead(key, version)
	}
	mine.err = err
	r.mu.Lock()
	if r.inFlight[key] == mine {
		delete(r.inFlight, key)
	}
	r.mu.Unlock()
	close(mine.done)
	q, ok := r.Store.Get(key)
	return q, ok, err
}

func (r *Reader) fetch(ctx context.Context, key string) (windows []RateWindow, credits string, err error) {
	if r.Fetch != nil {
		windows, credits, handled, err := r.Fetch(ctx, key)
		if handled {
			return windows, credits, err
		}
	}
	if _, native := NativeQuotaAccount(key); native && QuotaSource(key) == CodexProvider {
		return nil, "", errors.New("native Codex quota source unavailable")
	}
	key = QuotaSource(key)
	now := r.now()
	switch {
	case key == CopilotProvider:
		token, enterprise, err := ReadCopilotCredential(r.AuthPath)
		if err != nil || token == "" {
			return nil, "", err
		}
		base := r.CopilotBase
		if base == "" {
			base = CopilotAPIBase(enterprise)
		}
		credits, err = FetchCopilotCredits(ctx, r.HTTP, base, token)
		return nil, credits, err
	case key == CodexProvider:
		if r.Codex != nil {
			windows, err = r.Codex(ctx)
		} else {
			windows, err = ReadCodexRateLimits(ctx, r.now)
		}
		return windows, "", err
	case IsZaiProvider(key):
		url := r.ZaiURL
		if url == "" {
			url = ZaiQuotaURL(r.Registry.BaseURLs(key))
		}
		if url == "" {
			return nil, "", nil
		}
		windows, err = FetchZaiQuota(ctx, r.HTTP, url, r.Registry.APIKey(key), now)
		return windows, "", err
	case IsOpenCodeGoProvider(key):
		return r.fetchOpenCodeGo(ctx, now)
	}
	return nil, "", nil
}

func (r *Reader) fetchOpenCodeGo(ctx context.Context, now time.Time) ([]RateWindow, string, error) {
	endpoints := r.OpenCodeGo
	if endpoints.Usage == "" {
		endpoints = ProductionOpenCodeGo
	}
	piKey := ""
	if UsesOpenCodeHost(r.Registry.BaseURLs(OpenCodeGoID)) {
		piKey = r.Registry.APIKey(OpenCodeGoID)
	}
	key, keyErr := r.Env.OpenCodeGoAPIKey(piKey)
	var apiErr error
	if key != "" {
		windows, err := FetchOpenCodeGoUsage(ctx, r.HTTP, endpoints, key, now)
		if err == nil && len(windows) > 0 {
			return windows, "", nil
		}
		apiErr = err
	}
	if cookie, ok := r.Env.OpenCodeGoCookieConfig(); ok {
		windows, err := FetchOpenCodeGoDashboard(ctx, r.HTTP, endpoints, cookie, now)
		if err == nil {
			return windows, "", nil
		}
		apiErr = errors.Join(apiErr, err)
	}
	return nil, "", errors.Join(keyErr, apiErr)
}
