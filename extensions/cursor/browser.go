package cursor

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
)

// Browser sign-in follows @cursor/sdk 1.2.0 Cursor.auth.login: approve the
// SDK redirect, then mint a user API key with DashboardService.CreateUserApiKey.
// Only that key, not the browser session token, goes to the SDK Bridge.
const (
	loginURL         = "https://cursor.com/loginDeepControl"
	apiBaseURL       = "https://api2.cursor.sh"
	refreshClientID  = "KbZUR41cY7W6zRSdpSUJ7I7mLYBKOCmB" // public client id of the Cursor sign-in
	pollInterval     = 2 * time.Second
	loginTimeout     = 5 * time.Minute
	requestTimeout   = 30 * time.Second
	expiryMargin     = 5 * time.Minute
	maxResponseBytes = 1 << 20
	userKeyPath      = "/aiserver.v1.DashboardService/CreateUserApiKey"
	userKeyLifetime  = 90 * 24 * time.Hour
)

type tokens struct {
	Access  string
	Refresh string
	Expires time.Time
}

// browserAuth carries the endpoints and timing so tests can replace them.
type browserAuth struct {
	http         *http.Client
	loginURL     string
	apiBaseURL   string
	pollInterval time.Duration
	timeout      time.Duration
}

func newBrowserAuth() *browserAuth {
	return &browserAuth{http: &http.Client{}, loginURL: loginURL, apiBaseURL: apiBaseURL, pollInterval: pollInterval, timeout: loginTimeout}
}

// loginSession is what one browser approval yields. Key goes to the SDK Bridge.
// Browser is the dashboard session that reads quota. It stays zero when Cursor
// gave no usable session, and the account then works without a quota meter.
type loginSession struct {
	Key     tokens
	Browser tokens
}

// login shows the sign-in URL through show, then waits until the user approves it.
func (b *browserAuth) login(ctx context.Context, show func(url string) error) (loginSession, error) {
	verifier, challenge, err := newPKCE()
	if err != nil {
		return loginSession{}, fmt.Errorf("cursor login: create PKCE values: %w", err)
	}
	id, err := newUUID()
	if err != nil {
		return loginSession{}, fmt.Errorf("cursor login: create login id: %w", err)
	}
	query := url.Values{"challenge": {challenge}, "uuid": {id}, "mode": {"login"}, "redirectTarget": {"sdk"}}
	if err := show(b.loginURL + "?" + query.Encode()); err != nil {
		return loginSession{}, fmt.Errorf("cursor login: show sign-in page: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	pollURL := b.apiBaseURL + "/auth/poll?" + url.Values{"uuid": {id}, "verifier": {verifier}}.Encode()
	for {
		status, body, err := b.do(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return loginSession{}, fmt.Errorf("cursor login: wait for approval: %w", err)
		}
		switch status {
		case http.StatusNotFound: // Cursor answers 404 until the user approves.
		case http.StatusOK:
			var wire struct {
				AccessToken  string `json:"accessToken"`
				RefreshToken string `json:"refreshToken"`
			}
			if err := json.Unmarshal(body, &wire); err != nil {
				return loginSession{}, errors.New("cursor login: the approval reply was not valid")
			}
			key, err := b.mintKey(ctx, wire.AccessToken)
			if err != nil {
				return loginSession{}, err
			}
			// An unusable session is not fatal: the key alone still runs models.
			browser, err := tokensFrom(wire.AccessToken, wire.RefreshToken, "")
			if err != nil || browser.Refresh == "" {
				browser = tokens{}
			}
			return loginSession{Key: key, Browser: browser}, nil
		default:
			return loginSession{}, fmt.Errorf("cursor login: approval check failed (HTTP %d)", status)
		}
		select {
		case <-ctx.Done():
			return loginSession{}, fmt.Errorf("cursor login: not approved in time: %w", ctx.Err())
		case <-time.After(b.pollInterval):
		}
	}
}

// mintKey does not retry: an uncertain reply can mean a key already exists.
func (b *browserAuth) mintKey(ctx context.Context, access string) (tokens, error) {
	if access == "" {
		return tokens{}, errors.New("cursor approval reply carried no access token")
	}
	expires := time.Now().Add(userKeyLifetime)
	payload, err := json.Marshal(map[string]any{"name": "PiG", "expiresAt": fmt.Sprint(expires.UnixMilli())})
	if err != nil {
		return tokens{}, fmt.Errorf("encode Cursor key request: %w", err)
	}
	status, body, err := b.do(ctx, http.MethodPost, b.apiBaseURL+userKeyPath, payload, access)
	if err != nil {
		return tokens{}, fmt.Errorf("create Cursor user API key (not retried): %w", err)
	}
	if status != http.StatusOK {
		return tokens{}, fmt.Errorf("cursor browser approval succeeded, but user API key creation failed (HTTP %d). Your team may restrict user API keys", status)
	}
	var wire struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.APIKey == "" {
		return tokens{}, errors.New("cursor key creation returned no valid API key")
	}
	return tokens{Access: wire.APIKey, Expires: expires}, nil
}

func (b *browserAuth) refresh(ctx context.Context, refresh string) (tokens, error) {
	if refresh == "" {
		return tokens{}, errors.New("cursor session has no refresh token; run /login cursor again")
	}
	payload, err := json.Marshal(map[string]string{"grant_type": "refresh_token", "client_id": refreshClientID, "refresh_token": refresh})
	if err != nil {
		return tokens{}, fmt.Errorf("cursor refresh: encode request: %w", err)
	}
	status, body, err := b.do(ctx, http.MethodPost, b.apiBaseURL+"/oauth/token", payload)
	if err != nil {
		return tokens{}, fmt.Errorf("cursor refresh: %w", err)
	}
	if status != http.StatusOK {
		return tokens{}, fmt.Errorf("cursor refresh failed (HTTP %d)", status)
	}
	var wire struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ShouldLogout bool   `json:"shouldLogout"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return tokens{}, errors.New("cursor refresh: the reply was not valid")
	}
	// Cursor ends a session it will not renew with 200, an empty token and shouldLogout.
	if wire.ShouldLogout {
		return tokens{}, errors.New("cursor ended this session; run /login cursor again")
	}
	return tokensFrom(wire.AccessToken, wire.RefreshToken, refresh)
}

func tokensFrom(access, refresh, fallbackRefresh string) (tokens, error) {
	if access == "" {
		return tokens{}, errors.New("cursor reply carried no access token")
	}
	if refresh == "" {
		refresh = fallbackRefresh
	}
	expires, err := jwtExpiry(access)
	if err != nil {
		return tokens{}, err
	}
	return tokens{Access: access, Refresh: refresh, Expires: expires.Add(-expiryMargin)}, nil
}

// jwtExpiry reads the exp claim without verifying the signature. It only
// schedules the refresh; Cursor still checks the token.
func jwtExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("cursor access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, fmt.Errorf("decode Cursor access token: %w", err)
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return time.Time{}, errors.New("cursor access token has no expiry")
	}
	return time.Unix(int64(claims.Exp), 0), nil
}

// do talks only to the configured Cursor origin, never follows redirects, and
// bounds time and size. Errors never contain URLs, verifiers, or tokens.
func (b *browserAuth) do(ctx context.Context, method, target string, body []byte, bearer ...string) (int, []byte, error) {
	base, err := url.Parse(b.apiBaseURL)
	u, uerr := url.Parse(target)
	if err != nil || uerr != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil ||
		(u.Path != "/auth/poll" && u.Path != "/oauth/token" && u.Path != userKeyPath) {
		return 0, nil, errors.New("request origin or path rejected")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, errors.New("build request failed")
	}
	req.Header.Set("Accept", "application/json")
	if len(bearer) > 0 {
		req.Header.Set("Authorization", "Bearer "+bearer[0])
		req.Header.Set("Connect-Protocol-Version", "1")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *b.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") }
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, errors.New("request failed: " + redact(err))
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, errors.New("read response failed")
	}
	if len(data) > maxResponseBytes {
		return 0, nil, errors.New("response too large")
	}
	return resp.StatusCode, data, nil
}

// redact keeps the error kind but drops the URL, which holds the PKCE verifier.
func redact(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err.Error()
	}
	return "network error"
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
