package cursorlogin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

type directTransport interface {
	Catalog(context.Context, string) ([]map[string]any, error)
	Stream(map[string]any, map[string]any, sdk.ProviderStreamOptions) (*sdk.ModelEventStream, error)
}

// directProvider reuses the native credential store/refresh queue. It deliberately
// does not persist an account catalog in a provider-global disk cache.
func directProvider(c *client, transport directTransport) *sdk.Provider {
	var mu sync.RWMutex
	models := []map[string]any{}
	yes := true
	p := &sdk.Provider{ID: Provider, Name: "Cursor", BaseURL: &c.apiBaseURL, ReplayProhibited: true}
	// The host's typed model schema drops provider-private fields. Restore exact
	// catalog routing metadata by ID rather than guessing max-mode from its name.
	p.Stream = func(model, request map[string]any, options sdk.ProviderStreamOptions) (*sdk.ModelEventStream, error) {
		enriched := make(map[string]any, len(model))
		for k, v := range model {
			enriched[k] = v
		}
		mu.RLock()
		for _, candidate := range models {
			if candidate["id"] == model["id"] {
				enriched["cursorMaxMode"] = candidate["cursorMaxMode"]
				break
			}
		}
		mu.RUnlock()
		return transport.Stream(enriched, request, options)
	}
	p.StreamSimple = p.Stream
	p.GetModels = func() ([]map[string]any, error) {
		mu.RLock()
		defer mu.RUnlock()
		b, err := json.Marshal(models)
		if err != nil {
			return nil, err
		}
		var result []map[string]any
		err = json.Unmarshal(b, &result)
		return result, err
	}
	p.Auth.OAuth = &sdk.OAuthAuth{Name: "Cursor", IsSubscription: &yes,
		Login: func(interaction sdk.AuthInteraction, _ sdk.LoginOptions) (*sdk.Credential, error) {
			parent := interaction.Signal
			if parent == nil {
				parent = context.Background()
			}
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			ui := &directLoginUI{interaction: interaction, cancel: cancel}
			creds, err := c.login(ctx, ui)
			if ui.err != nil {
				return nil, ui.err
			}
			if err != nil {
				return nil, err
			}
			b, err := json.Marshal(creds)
			if err != nil {
				return nil, err
			}
			var out sdk.Credential
			if err = json.Unmarshal(b, &out); err != nil {
				return nil, err
			}
			out.Set("type", "oauth")
			return &out, nil
		},
		Refresh: func(credential *sdk.Credential, ctx context.Context) (*sdk.Credential, error) {
			if ctx == nil {
				ctx = context.Background()
			}
			b, err := json.Marshal(credential)
			if err != nil {
				return nil, err
			}
			var creds sdk.OAuthCredentials
			if err = json.Unmarshal(b, &creds); err != nil {
				return nil, err
			}
			next, err := c.refresh(ctx, creds)
			if err != nil {
				return nil, err
			}
			out := credential.Clone()
			out.Set("access", next.Access)
			out.Set("refresh", next.Refresh)
			out.Set("expires", next.Expires)
			return out, nil
		},
		ToAuth: func(credential *sdk.Credential) (map[string]any, error) {
			token := credential.String("access")
			if token == "" {
				return nil, errors.New("cursor: access token missing; run /login cursor")
			}
			return map[string]any{"apiKey": token}, nil
		},
	}
	p.RefreshModels = func(refresh sdk.RefreshModelsContext) error {
		if refresh.Publish == nil {
			return errors.New("cursor: missing native model publication callback")
		}
		// The host guards Publish against cancelled/superseded account generations.
		// Clear before fetch (and on logout/offline startup): never expose another
		// account's cached entitlement if a refresh fails or credentials change.
		accepted, err := refresh.Publish(sdk.ModelsPublication{Update: func() error { mu.Lock(); models = []map[string]any{}; mu.Unlock(); return nil }})
		if err != nil {
			return err
		}
		if !accepted {
			return nil
		}
		token := refresh.Credential.String("access")
		if token == "" || !refresh.AllowNetwork {
			return nil
		}
		ctx := refresh.Signal
		if ctx == nil {
			ctx = context.Background()
		}
		catalog, err := transport.Catalog(ctx, token)
		if err != nil {
			return err
		}
		_, err = refresh.Publish(sdk.ModelsPublication{Update: func() error { mu.Lock(); models = catalog; mu.Unlock(); return nil }})
		return err
	}
	return p
}

type directLoginUI struct {
	interaction sdk.AuthInteraction
	cancel      context.CancelFunc
	err         error
}

func (ui *directLoginUI) notify(event map[string]any) {
	if ui.err != nil {
		return
	}
	if ui.interaction.Notify == nil {
		ui.err = errors.New("cursor: missing login notification callback")
		ui.cancel()
		return
	}
	if err := ui.interaction.Notify(event); err != nil {
		ui.err = err
		ui.cancel()
	}
}
func (ui *directLoginUI) OnAuth(info sdk.OAuthAuthInfo) {
	ui.notify(map[string]any{"type": "auth_url", "url": info.URL, "instructions": info.Instructions})
}
func (ui *directLoginUI) OnProgress(s string) {
	ui.notify(map[string]any{"type": "progress", "message": s})
}
