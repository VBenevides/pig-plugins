package browseruseext

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

type loginTestUI struct{ selectFn func() (string, bool, error) }

func (loginTestUI) HasUI() bool { return true }
func (u loginTestUI) SelectWithOptions(string, []string, sdk.DialogOptions) (string, bool, error) {
	return u.selectFn()
}

func TestLoginDialogFailurePreservesCause(t *testing.T) {
	original := errors.New("dialog unavailable")
	ui := loginTestUI{selectFn: func() (string, bool, error) { return "", false, original }}
	if _, err := manualLogin(context.Background(), ui, nil); !errors.Is(err, original) {
		t.Fatalf("lost dialog failure: %v", err)
	}
}

func TestCanceledLoginNeverReadsBrowser(t *testing.T) {
	request, cancel := context.WithCancel(context.Background())
	ui := loginTestUI{selectFn: func() (string, bool, error) { cancel(); return "Logged in", true, nil }}
	if _, err := manualLogin(request, ui, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation accepted as login: %v", err)
	}
	ui.selectFn = func() (string, bool, error) { t.Fatal("canceled request opened a dialog"); return "", false, nil }
	if _, err := manualLogin(request, ui, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost initial cancellation: %v", err)
	}
}
