package browseruseext

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/browseruse"
)

type loginUI interface {
	HasUI() bool
	SelectWithOptions(string, []string, sdk.DialogOptions) (string, bool, error)
}

func manualLogin(request context.Context, ctx loginUI, client *browseruse.Client) (any, error) {
	if !ctx.HasUI() {
		return nil, errors.New("login is required; use an interactive session to log in directly in the visible browser")
	}
	if err := request.Err(); err != nil {
		return nil, err
	}
	choice, ok, err := ctx.SelectWithOptions("Log in directly in the visible browser. Enter passwords and verification codes there, not in chat. Then select Logged in.", []string{"Logged in", "Other"}, sdk.DialogOptions{Timeout: 600000})
	if err != nil {
		return nil, fmt.Errorf("wait for browser login: %w", err)
	}
	if err := request.Err(); err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("browser login was canceled or timed out; do not continue secured-site work")
	}
	if choice != "Logged in" {
		return nil, errors.New("browser login was not confirmed; the user selected Other. Ask how to proceed before using the secured site")
	}
	page, err := client.Run(request, "snapshot", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("read page after manual login: %w", err)
	}
	result, ok := page.(map[string]any)
	if !ok {
		return nil, errors.New("browser returned an invalid post-login snapshot")
	}
	if result["loginRequired"] == true {
		return nil, errors.New("the page still shows a login form; login is not verified. Do not continue secured-site work")
	}
	return map[string]any{"loginStatus": "confirmed_by_user", "page": page}, nil
}

const hiddenLoginMessage = "Browser visibility is hidden. Never open a visible browser for login. Use another authorized method or a public source. Do not bypass authentication or retry the same login automatically. The user can explicitly select /browser-use visibility login or always for direct login."

func loginWithVisibility(request context.Context, ui loginUI, client *browseruse.Client, visibility browseruse.Visibility) (value any, err error) {
	if visibility != browseruse.Login && visibility != browseruse.Always {
		return nil, errors.New(hiddenLoginMessage)
	}
	if visibility == browseruse.Always {
		return manualLogin(request, ui, client)
	}
	if !ui.HasUI() {
		return nil, errors.New("manual browser login requires an interactive UI")
	}
	// Always return to headless mode, including Other, cancellation, or failure.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var hideErr error
		if err != nil {
			hideErr = client.Hide(cleanup)
		} else {
			hideErr = client.SetVisible(cleanup, false)
		}
		if hideErr != nil {
			closeErr := client.Close(cleanup)
			err = errors.Join(err, fmt.Errorf("return browser to hidden mode: %w", hideErr))
			if closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close browser after failed hidden restart: %w", closeErr))
			}
			value = nil
		}
		if err != nil {
			return
		}
		page, snapshotErr := client.Run(request, "snapshot", nil, nil)
		if snapshotErr != nil {
			err = fmt.Errorf("read authenticated page after headless restart: %w", snapshotErr)
			value = nil
			return
		}
		if page.(map[string]any)["loginRequired"] == true {
			err = errors.New("login did not survive the headless restart. Do not repeat login automatically. Ask whether to use /browser-use visibility always or another authorized method")
			value = nil
			return
		}
		value = map[string]any{"loginStatus": "confirmed_by_user", "page": page}
	}()
	if err = client.SetVisible(request, true); err != nil {
		return nil, fmt.Errorf("open visible browser for login: %w", err)
	}
	if _, err = client.Run(request, "snapshot", nil, nil); err != nil {
		return nil, fmt.Errorf("check visible login page: %w", err)
	}
	return manualLogin(request, ui, client)
}
