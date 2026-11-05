// Package cursor adds a Cursor provider built on the official Cursor SDK Bridge.
//
// Accounts are API keys stored in the host's native account store. Cursor never
// runs a tool: tool calls come back as proposals and PiG executes them under its
// normal validation and approvals. Offline tests use a mock bridge; no real
// Cursor account has been exercised.
package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
	"github.com/VBenevides/pig-plugins/internal/cursormodels"
)

const (
	Name     = "cursor"
	Provider = "cursor"
	API      = "cursor-agent"

	idleTimeout = 2 * time.Minute
	turnTimeout = 30 * time.Minute
	// Cursor's catalog reports no limits, so these are estimates, not account facts.
	contextEstimate = 200000
	outputEstimate  = 64000
	// An API key does not expire. The host's account store needs an expiry, so it
	// is far in the future and Refresh returns the key unchanged.
	keyLifetime = 10 * 365 * 24 * time.Hour
)

type service struct {
	manager *manager

	mu         sync.Mutex
	accountKey string // hash of the key the cached catalog belongs to
	entries    []cursormodels.Entry
	models     []map[string]any
}

// Extension returns the extension.
func Extension() *sdk.Extension {
	s := &service{manager: newManager(startBridge, idleTimeout)}
	e := sdk.New(Name)
	if err := e.RegisterNativeProvider(s.provider()); err != nil {
		e.OnSessionStart(func(sdk.Context, map[string]any) (any, error) { return nil, err })
	}
	e.OnSessionShutdown(func(sdk.Context, map[string]any) (any, error) {
		return nil, s.manager.closeAll()
	})
	return e
}

func startBridge(ctx context.Context, key string) (runner, error) {
	path, err := cursorbridge.Installed(agentdir.Dir(os.Getenv), runtime.GOOS)
	if err != nil {
		return nil, err
	}
	process, err := cursorbridge.Start(ctx, path, key)
	if err != nil {
		return nil, err
	}
	return processRunner{process}, nil
}

func (s *service) provider() *sdk.Provider {
	p := &sdk.Provider{ID: Provider, Name: "Cursor", ReplayProhibited: true}
	p.GetModels = s.getModels
	p.Stream = s.stream
	p.StreamSimple = p.Stream
	p.RefreshModels = s.refreshModels
	p.Auth.OAuth = &sdk.OAuthAuth{
		Name:  "Cursor",
		Login: s.login,
		// An API key has no refresh token; the stored key is returned unchanged.
		Refresh: func(credential *sdk.Credential, _ context.Context) (*sdk.Credential, error) {
			if credential.String("access") == "" {
				return nil, errors.New("Cursor API key is missing; run /login")
			}
			next := credential.Clone()
			next.Set("expires", time.Now().Add(keyLifetime).UnixMilli())
			return next, nil
		},
		ToAuth: func(credential *sdk.Credential) (map[string]any, error) {
			key := credential.String("access")
			if key == "" {
				return nil, errors.New("Cursor API key is missing; run /login")
			}
			return map[string]any{"apiKey": key}, nil
		},
	}
	return p
}

// login stores a Cursor API key. The key is checked with the bridge, and the
// account identity comes from Cursor, not from the key's text.
func (s *service) login(interaction sdk.AuthInteraction, _ sdk.LoginOptions) (*sdk.Credential, error) {
	ctx := interaction.Signal
	if ctx == nil {
		ctx = context.Background()
	}
	if interaction.Prompt == nil {
		return nil, errors.New("Cursor login needs an interactive prompt")
	}
	key, err := interaction.Prompt(map[string]any{"type": "secret", "message": "Cursor API key (create one in the Cursor dashboard)"})
	if err != nil {
		return nil, fmt.Errorf("read Cursor API key: %w", err)
	}
	if key == "" {
		return nil, errors.New("Cursor API key must not be empty")
	}
	checkCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	r, release, err := s.manager.acquire(checkCtx, key)
	if err != nil {
		return nil, fmt.Errorf("start Cursor bridge for login: %w", err)
	}
	user, err := r.Me(checkCtx, key)
	release()
	if err != nil {
		// A rejected key must not leave a process holding it.
		s.manager.drop(key)
		return nil, fmt.Errorf("verify Cursor API key: %w", err)
	}
	credential := sdk.NewCredential(
		"type", "oauth",
		"access", key,
		"refresh", "",
		"expires", time.Now().Add(keyLifetime).UnixMilli(),
		"accountId", "cursor-"+strconv.FormatInt(int64(user.UserID), 10),
	)
	if user.Email != "" {
		credential.Set("email", user.Email)
	}
	return credential, nil
}

func (s *service) getModels() ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(s.models)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	err = json.Unmarshal(b, &out)
	return out, err
}

func (s *service) setCatalog(accountKey string, entries []cursormodels.Entry) {
	models := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		models = append(models, modelMap(entry))
	}
	s.mu.Lock()
	s.accountKey, s.entries, s.models = accountKey, entries, models
	s.mu.Unlock()
}

func (s *service) clearCatalog() { s.setCatalog("", nil) }

func modelMap(entry cursormodels.Entry) map[string]any {
	model := map[string]any{
		"id": entry.ID, "name": entry.Name, "provider": Provider, "api": API,
		"reasoning": entry.Reasoning, "input": []string{"text"},
		"contextWindow": contextEstimate, "maxTokens": outputEstimate,
		"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
		// The catalog does not report limits; consumers must treat these as estimates.
		"cursorLimitsEstimated": true,
	}
	if entry.Reasoning {
		// Only supported levels are selectable. A null entry removes a level, so
		// PiG can never ask for a combination Cursor did not list.
		levels := map[string]any{"off": nil}
		for _, level := range cursormodels.Levels {
			levels[level] = nil
		}
		for _, level := range entry.Levels() {
			levels[level] = level
		}
		model["thinkingLevelMap"] = levels
	}
	return model
}

// refreshModels clears the catalog first. A failed refresh, logout, or offline
// start therefore never exposes another account's models.
func (s *service) refreshModels(refresh sdk.RefreshModelsContext) error {
	if refresh.Publish == nil {
		return errors.New("Cursor: the host did not provide a model publication callback")
	}
	accepted, err := refresh.Publish(sdk.ModelsPublication{Update: func() error { s.clearCatalog(); return nil }})
	if err != nil || !accepted {
		return err
	}
	if refresh.Credential == nil || !refresh.AllowNetwork {
		return nil
	}
	key := refresh.Credential.String("access")
	if key == "" {
		return nil
	}
	ctx := refresh.Signal
	if ctx == nil {
		ctx = context.Background()
	}
	entries, err := s.fetchCatalog(ctx, key)
	if err != nil {
		return err
	}
	_, err = refresh.Publish(sdk.ModelsPublication{Update: func() error { s.setCatalog(accountHash(key), entries); return nil }})
	return err
}

func (s *service) fetchCatalog(ctx context.Context, key string) ([]cursormodels.Entry, error) {
	r, release, err := s.manager.acquire(ctx, key)
	if err != nil {
		return nil, err
	}
	defer release()
	models, err := r.Models(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("list Cursor models: %w", err)
	}
	return cursormodels.Collapse(models)
}

// entriesFor returns the catalog for this account. A cached catalog from another
// account is never used.
func (s *service) entriesFor(ctx context.Context, key string) ([]cursormodels.Entry, error) {
	s.mu.Lock()
	if s.accountKey == accountHash(key) && len(s.entries) > 0 {
		entries := s.entries
		s.mu.Unlock()
		return entries, nil
	}
	s.mu.Unlock()
	return s.fetchCatalog(ctx, key)
}
