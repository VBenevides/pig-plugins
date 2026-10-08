// Package cursorlogin adds a Cursor OAuth login to PiG's /login picker.
//
// Cursor has no documented OAuth API. This follows the PKCE "deep control" flow used by cursor-agent, which polls
// for tokens instead of receiving a callback. The endpoints are undocumented and may change.
package cursorlogin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

const (
	Name     = "cursor-login"
	Provider = "cursor"

	loginURL   = "https://cursor.com/loginDeepControl"
	apiBaseURL = "https://api2.cursor.sh"

	pollInterval   = 2 * time.Second
	loginTimeout   = 5 * time.Minute
	requestTimeout = 30 * time.Second
	// expiryMargin refreshes slightly before the JWT's own expiry.
	expiryMargin = 5 * time.Minute
	// maxResponseBytes bounds any response body read.
	maxResponseBytes = 1 << 20
	// refreshClientID is the OAuth client the Cursor IDE renews its login session with (public; same value Oh My Pi uses).
	refreshClientID = "KbZUR41cY7W6zRSdpSUJ7I7mLYBKOCmB"
)

// client carries the endpoints and timing so tests can substitute them.
type client struct {
	http         *http.Client
	loginURL     string
	apiBaseURL   string
	pollInterval time.Duration
	timeout      time.Duration
}

func defaultClient() *client {
	return &client{
		http:         &http.Client{Timeout: requestTimeout},
		loginURL:     loginURL,
		apiBaseURL:   apiBaseURL,
		pollInterval: pollInterval,
		timeout:      loginTimeout,
	}
}

type tokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func Extension() *sdk.Extension {
	c := defaultClient()
	e := sdk.New(Name)
	e.RegisterProvider(Provider, sdk.ProviderConfig{
		"baseUrl": c.apiBaseURL,
		"oauth": &sdk.OAuthProvider{
			Name:           "Cursor",
			IsSubscription: true,
			Login: func(cb *sdk.OAuthLoginCallbacks) (sdk.OAuthCredentials, error) {
				return c.login(context.Background(), cb)
			},
			RefreshToken: func(creds sdk.OAuthCredentials) (sdk.OAuthCredentials, error) {
				return c.refresh(context.Background(), creds)
			},
			GetAPIKey: func(creds sdk.OAuthCredentials) string { return creds.Access },
		},
	})
	return e
}

// loginUI is the subset of the SDK login callbacks the flow uses.
type loginUI interface {
	OnAuth(sdk.OAuthAuthInfo)
	OnProgress(string)
}

func (c *client) login(ctx context.Context, ui loginUI) (sdk.OAuthCredentials, error) {
	verifier, challenge, err := newPKCE()
	if err != nil {
		return sdk.OAuthCredentials{}, fmt.Errorf("cursor login: generate PKCE: %w", err)
	}
	id, err := newUUID()
	if err != nil {
		return sdk.OAuthCredentials{}, fmt.Errorf("cursor login: generate id: %w", err)
	}
	query := url.Values{"challenge": {challenge}, "uuid": {id}, "mode": {"login"}, "redirectTarget": {"cli"}}
	ui.OnAuth(sdk.OAuthAuthInfo{
		URL:          c.loginURL + "?" + query.Encode(),
		Instructions: "Approve the login in your browser; this window continues automatically.",
	})
	ui.OnProgress("Waiting for Cursor login approval...")

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	pollURL := c.apiBaseURL + "/auth/poll?" + url.Values{"uuid": {id}, "verifier": {verifier}}.Encode()
	for {
		tokens, pending, err := c.pollOnce(ctx, pollURL)
		if err != nil {
			return sdk.OAuthCredentials{}, err
		}
		if !pending {
			return credentialsFrom(tokens, "")
		}
		select {
		case <-ctx.Done():
			return sdk.OAuthCredentials{}, fmt.Errorf("cursor login: timed out waiting for approval: %w", ctx.Err())
		case <-time.After(c.pollInterval):
		}
	}
}

// pollOnce reports pending on 404, which Cursor returns until the login is approved.
func (c *client) pollOnce(ctx context.Context, pollURL string) (tokenResponse, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
	if err != nil {
		return tokenResponse{}, false, fmt.Errorf("cursor login: build poll request: %w", err)
	}
	status, body, err := c.do(req)
	if err != nil {
		return tokenResponse{}, false, fmt.Errorf("cursor login: poll: %w", err)
	}
	if status == http.StatusNotFound {
		return tokenResponse{}, true, nil
	}
	if status != http.StatusOK {
		return tokenResponse{}, false, fmt.Errorf("cursor login: poll failed (HTTP %d)", status)
	}
	var tokens tokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return tokenResponse{}, false, fmt.Errorf("cursor login: decode poll response: %w", err)
	}
	return tokens, false, nil
}

func (c *client) refresh(ctx context.Context, creds sdk.OAuthCredentials) (sdk.OAuthCredentials, error) {
	if creds.Refresh == "" {
		return sdk.OAuthCredentials{}, errors.New("cursor refresh: credential has no refresh token; run /login cursor again")
	}
	reqBody, err := json.Marshal(map[string]string{"grant_type": "refresh_token", "client_id": refreshClientID, "refresh_token": creds.Refresh})
	if err != nil {
		return sdk.OAuthCredentials{}, fmt.Errorf("cursor refresh: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/oauth/token", bytes.NewReader(reqBody))
	if err != nil {
		return sdk.OAuthCredentials{}, fmt.Errorf("cursor refresh: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	status, body, err := c.do(req)
	if err != nil {
		return sdk.OAuthCredentials{}, fmt.Errorf("cursor refresh: %w", err)
	}
	if status != http.StatusOK {
		return sdk.OAuthCredentials{}, fmt.Errorf("cursor refresh failed (HTTP %d)", status)
	}
	var refreshed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ShouldLogout bool   `json:"shouldLogout"`
	}
	if err := json.Unmarshal(body, &refreshed); err != nil {
		return sdk.OAuthCredentials{}, fmt.Errorf("cursor refresh: decode response: %w", err)
	}
	// Cursor answers a session it will not renew with 200, an empty token and shouldLogout.
	if refreshed.ShouldLogout {
		return sdk.OAuthCredentials{}, errors.New("cursor refresh: Cursor ended this session; run /login cursor again")
	}
	return credentialsFrom(tokenResponse{AccessToken: refreshed.AccessToken, RefreshToken: refreshed.RefreshToken}, creds.Refresh)
}

func (c *client) do(req *http.Request) (int, []byte, error) {
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	return resp.StatusCode, body, err
}

// credentialsFrom builds credentials, keeping fallbackRefresh when the response rotates no refresh token.
func credentialsFrom(tokens tokenResponse, fallbackRefresh string) (sdk.OAuthCredentials, error) {
	if tokens.AccessToken == "" {
		return sdk.OAuthCredentials{}, errors.New("cursor: response carried no access token")
	}
	refresh := tokens.RefreshToken
	if refresh == "" {
		refresh = fallbackRefresh
	}
	expires, err := jwtExpiry(tokens.AccessToken)
	if err != nil {
		return sdk.OAuthCredentials{}, err
	}
	return sdk.OAuthCredentials{Access: tokens.AccessToken, Refresh: refresh, Expires: expires.Add(-expiryMargin).UnixMilli()}, nil
}

// jwtExpiry reads the exp claim without verifying the signature; it only schedules refresh.
func jwtExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("cursor: access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, fmt.Errorf("cursor: decode access token payload: %w", err)
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("cursor: parse access token payload: %w", err)
	}
	if claims.Exp <= 0 {
		return time.Time{}, errors.New("cursor: access token has no exp claim")
	}
	return time.Unix(int64(claims.Exp), 0), nil
}

func newPKCE() (verifier, challenge string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
