package automodels

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
)

// Native account IDs are local credential selectors, not ChatGPT account IDs.
type accountSource interface {
	OAuthAccounts() ([]sdk.OAuthAccount, error)
	OAuthAccountAuth(string) (map[string]any, error)
}

func accountAuth(source accountSource, id string) (quota.AuthEntry, error) {
	raw, err := source.OAuthAccountAuth(id)
	if err != nil {
		// Host errors can contain credentials. Do not forward their text.
		return quota.AuthEntry{}, fmt.Errorf("native account credential lookup failed")
	}
	entry := quota.AuthEntry{}
	entry.Type, _ = raw["type"].(string)
	entry.Access, _ = raw["access"].(string)
	entry.AccountID, _ = raw["accountId"].(string)
	entry.Expires, _ = raw["expires"].(float64)
	return entry, nil
}

func selectedAccount(source accountSource, provider string) (sdk.OAuthAccount, bool, error) {
	accounts, err := source.OAuthAccounts()
	if err != nil {
		return sdk.OAuthAccount{}, false, fmt.Errorf("native OAuth account enumeration failed")
	}
	for _, account := range accounts {
		if account.Provider == provider && account.Active {
			return account, true, nil
		}
	}
	return sdk.OAuthAccount{}, false, nil
}

func selectedAuth(source accountSource, provider string) (quota.AuthEntry, bool, error) {
	account, ok, err := selectedAccount(source, provider)
	if err != nil || !ok {
		return quota.AuthEntry{}, false, err
	}
	entry, err := accountAuth(source, account.ID)
	return entry, err == nil, err
}

// accountStatus is shared by the footer and deterministic quota integration tests.
func (x *extension) accountStatus(run context.Context, source accountSource, account sdk.OAuthAccount) (*quota.StatusQuota, error) {
	status, _, err := x.accountQuota(run, source, account)
	return status, err
}

func (x *extension) accountQuota(run context.Context, source accountSource, account sdk.OAuthAccount) (*quota.StatusQuota, *float64, error) {
	entry, err := accountAuth(source, account.ID)
	if err != nil {
		return nil, nil, err
	}
	if !validOAuth(entry, time.Now()) {
		return nil, nil, fmt.Errorf("native OAuth credentials unavailable or expired")
	}
	switch account.Provider {
	case "anthropic":
		usage, err := x.client.FetchClaude(run, entry)
		return quota.ClaudeStatusQuota(usage), quota.ClaudeRemaining(usage), err
	case "openai-codex":
		usage, err := x.client.FetchCodex(run, entry)
		return quota.CodexStatusQuota(usage), quota.CodexRemaining(usage), err
	default:
		return nil, nil, nil
	}
}
