package cursor

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

func TestCatalogOfflineRestartAndAccountIsolation(t *testing.T) {
	r := &fakeRunner{catalog: grokCatalog}
	s, starts := newService(map[string]*fakeRunner{"a": r})
	var stored map[string]any
	publish := func(p sdk.ModelsPublication) (bool, error) {
		if p.Persist != nil {
			if err := json.Unmarshal(p.Persist, &stored); err != nil {
				return false, err
			}
		}
		return true, p.Update()
	}
	refresh := sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", "a"), AllowNetwork: true, Publish: publish}
	if err := s.refreshModels(refresh); err != nil {
		t.Fatal(err)
	}
	want, _ := s.getModels()
	if len(want) == 0 || stored == nil {
		t.Fatal("catalog was not persisted")
	}
	raw, _ := json.Marshal(stored)
	if strings.Contains(string(raw), `"access"`) {
		t.Fatal("credential persisted in catalog")
	}
	refresh.AllowNetwork = false
	if err := s.refreshModels(refresh); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.getModels(); !reflect.DeepEqual(got, want) {
		t.Fatal("offline refresh cleared catalog")
	}
	restarted, restarts := newService(nil)
	refresh.Stored = stored
	if err := restarted.refreshModels(refresh); err != nil {
		t.Fatal(err)
	}
	if got, _ := restarted.getModels(); !reflect.DeepEqual(got, want) {
		t.Fatal("restart lost catalog")
	}
	// Exact variant resolution must also survive restart, not just display names.
	if !reflect.DeepEqual(restarted.entries, s.entries) {
		t.Fatal("restart changed exact selections")
	}
	if starts.Load() != 1 || restarts.Load() != 0 {
		t.Fatal("offline refresh started a bridge")
	}
	refresh.Credential = sdk.NewCredential("access", "b")
	if err := restarted.refreshModels(refresh); err != nil {
		t.Fatal(err)
	}
	if got, _ := restarted.getModels(); len(got) != 0 {
		t.Fatal("account B inherited account A catalog")
	}
	refresh.Credential = nil
	if err := restarted.refreshModels(refresh); err != nil {
		t.Fatal(err)
	}
	if stored != nil {
		t.Fatal("logout did not delete durable catalog")
	}
}

func TestCatalogSameAccountFailurePreservesModels(t *testing.T) {
	s, _ := newService(map[string]*fakeRunner{"a": {catalog: []cursorbridge.Model{{ID: "plain"}}}})
	publish := func(p sdk.ModelsPublication) (bool, error) { return true, p.Update() }
	refresh := sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", "a"), AllowNetwork: true, Publish: publish}
	if err := s.refreshModels(refresh); err != nil {
		t.Fatal(err)
	}
	want, _ := s.getModels()
	if err := s.manager.closeAll(); err != nil {
		t.Fatal(err)
	}
	if err := s.refreshModels(refresh); err == nil {
		t.Fatal("failed discovery hidden")
	}
	if got, _ := s.getModels(); !reflect.DeepEqual(got, want) {
		t.Fatal("failed refresh cleared valid catalog")
	}
}

func TestCatalogRejectsCorruptStoredSelection(t *testing.T) {
	for _, models := range []any{nil, "broken", []any{map[string]any{"id": "plain"}}, []any{map[string]any{"id": "plain", "cursorSelection": map[string]any{"ID": "plain", "Reasoning": true, "Selections": map[string]any{"mystery": map[string]any{"id": "plain"}}}}}} {
		s, _ := newService(nil)
		s.setCatalog("other-account", nil)
		err := s.refreshModels(sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", "a"), Stored: map[string]any{"etag": catalogVersion + accountHash("a"), "models": models}, Publish: func(p sdk.ModelsPublication) (bool, error) { return true, p.Update() }})
		if err == nil {
			t.Fatal("corrupt stored model accepted")
		}
		if got, _ := s.getModels(); len(got) != 0 {
			t.Fatal("invalid catalog exposed")
		}
	}
}

func TestCatalogRejectedPublicationDoesNotMutateService(t *testing.T) {
	s, starts := newService(nil)
	err := s.refreshModels(sdk.RefreshModelsContext{Credential: sdk.NewCredential("access", "a"), AllowNetwork: true, Publish: func(sdk.ModelsPublication) (bool, error) { return false, errors.New("publication rejected") }})
	if err == nil || starts.Load() != 0 {
		t.Fatal("rejected refresh continued")
	}
	if got, _ := s.getModels(); len(got) != 0 {
		t.Fatal("rejected publication changed catalog")
	}
}
