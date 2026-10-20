package cursorlogin

import (
	"context"
	"errors"
	"sync"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

type catalogFixture struct {
	fetch  func(context.Context, string) ([]map[string]any, error)
	stream func(map[string]any, map[string]any, sdk.ProviderStreamOptions) (*sdk.ModelEventStream, error)
}

func (f catalogFixture) Catalog(ctx context.Context, token string) ([]map[string]any, error) {
	return f.fetch(ctx, token)
}
func (f catalogFixture) Stream(m, r map[string]any, o sdk.ProviderStreamOptions) (*sdk.ModelEventStream, error) {
	if f.stream != nil {
		return f.stream(m, r, o)
	}
	return sdk.CreateAssistantMessageEventStream(), nil
}
func publication(p sdk.ModelsPublication) (bool, error) {
	if p.Update != nil {
		return true, p.Update()
	}
	return true, nil
}
func TestDirectCatalogPublicationIsolationAndLogout(t *testing.T) {
	var observed []string
	fixture := catalogFixture{fetch: func(ctx context.Context, token string) ([]map[string]any, error) {
		observed = append(observed, token)
		if token == "fail" {
			return nil, errors.New("catalog unavailable")
		}
		return []map[string]any{{"id": token, "cursorMaxMode": true}}, nil
	}}
	p := directProvider(defaultClient(), fixture)
	for _, token := range []string{"account-a", "account-b"} {
		if err := p.RefreshModels(sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", token), AllowNetwork: true, Publish: publication}); err != nil {
			t.Fatal(err)
		}
		models, _ := p.GetModels()
		if len(models) != 1 || models[0]["id"] != token {
			t.Fatal(models)
		}
		models[0]["id"] = "mutated"
		copy, _ := p.GetModels()
		if copy[0]["id"] != token {
			t.Fatal("catalog aliasing")
		}
	}
	if err := p.RefreshModels(sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", "fail"), AllowNetwork: true, Publish: publication}); err == nil {
		t.Fatal("fetch failure concealed")
	}
	models, _ := p.GetModels()
	if len(models) != 0 {
		t.Fatal("previous account exposed")
	}
	if err := p.RefreshModels(sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", "account-a"), AllowNetwork: false, Publish: publication}); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 3 {
		t.Fatal("unexpected offline fetch")
	}
	if err := p.RefreshModels(sdk.RefreshModelsContext{Publish: publication}); err != nil {
		t.Fatal(err)
	}
	models, _ = p.GetModels()
	if len(models) != 0 {
		t.Fatal("logout did not clear catalog")
	}
	if _, err := p.Auth.OAuth.ToAuth(sdk.NewCredential()); err == nil {
		t.Fatal("empty credential accepted")
	}
}
func TestDirectSupersededRefreshCannotPublishAndMaxModeRetained(t *testing.T) {
	var mu sync.Mutex
	generation := 0
	started := make(chan struct{})
	release := make(chan struct{})
	fixture := catalogFixture{fetch: func(ctx context.Context, token string) ([]map[string]any, error) {
		if token == "old" {
			close(started)
			<-release
		}
		return []map[string]any{{"id": token, "cursorMaxMode": true}}, nil
	}, stream: func(m, r map[string]any, o sdk.ProviderStreamOptions) (*sdk.ModelEventStream, error) {
		if m["cursorMaxMode"] != true {
			t.Error("typed host routing metadata lost")
		}
		return sdk.CreateAssistantMessageEventStream(), nil
	}}
	p := directProvider(defaultClient(), fixture)
	publish := func(g int) func(sdk.ModelsPublication) (bool, error) {
		return func(value sdk.ModelsPublication) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if generation != g {
				return false, nil
			}
			return publication(value)
		}
	}
	finished := make(chan error, 1)
	go func() {
		finished <- p.RefreshModels(sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", "old"), AllowNetwork: true, Publish: publish(0)})
	}()
	<-started
	mu.Lock()
	generation = 1
	mu.Unlock()
	if err := p.RefreshModels(sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", "new"), AllowNetwork: true, Publish: publish(1)}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	models, _ := p.GetModels()
	if len(models) != 1 || models[0]["id"] != "new" {
		t.Fatal(models)
	}
	_, err := p.Stream(map[string]any{"id": "new"}, nil, sdk.ProviderStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
}
func TestDirectLoginCancellationAndNotificationFailure(t *testing.T) {
	p := directProvider(defaultClient(), catalogFixture{})
	sentinel := errors.New("notification unavailable")
	_, err := p.Auth.OAuth.Login(sdk.AuthInteraction{Signal: context.Background(), Notify: func(map[string]any) error { return sentinel }}, sdk.LoginOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("lost notification failure: %v", err)
	}
}
