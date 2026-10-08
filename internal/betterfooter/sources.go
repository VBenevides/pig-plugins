package betterfooter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Provider ids with their own quota source.
const (
	CopilotProvider  = "github-copilot"
	CodexProvider    = "openai-codex"
	OpenCodeGoID     = "opencode-go"
	ChatGPTQuotaKey  = "openai:chatgpt"
	ChatGPTUsageURL  = "https://chatgpt.com/settings/usage"
	zaiQuotaURL      = "https://api.z.ai/api/monitor/usage/quota/limit"
	zaiQuotaURLCN    = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"
	openCodeUsageURL = "https://opencode.ai/zen/go/v1/usage"
	openCodeDashURL  = "https://opencode.ai/workspace"
)

// IsZaiProvider reports whether the provider id names a Z.AI account.
func IsZaiProvider(provider string) bool { return strings.HasPrefix(strings.ToLower(provider), "zai") }

// IsOpenCodeGoProvider reports whether the provider id is OpenCode Go.
func IsOpenCodeGoProvider(provider string) bool { return provider == OpenCodeGoID }

func finiteNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}

func plainNumberValue(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok && !math.IsNaN(n) && !math.IsInf(n, 0)
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func onHost(hosts []string, domain string) bool {
	for _, host := range hosts {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// ZaiQuotaURL picks the monitor endpoint from where the provider's models send their requests, so an unrelated
// API key is never sent to Z.AI by a provider that is only named "zai...". It returns "" for any other host.
func ZaiQuotaURL(baseURLs []string) string {
	hosts := make([]string, 0, len(baseURLs))
	for _, base := range baseURLs {
		hosts = append(hosts, hostOf(base))
	}
	switch {
	case onHost(hosts, "bigmodel.cn"):
		return zaiQuotaURLCN
	case onHost(hosts, "z.ai"):
		return zaiQuotaURL
	}
	return ""
}

// ParseZaiQuota reads the account quota-limit payload. The 5-hour (unit 3) and weekly (unit 6) windows come from
// CREDIT_LIMIT rows (percentage is the USED percent) or TOKENS_LIMIT rows (a numeric remaining already is a
// percent); the monthly TIME_LIMIT row (unit 5) meters auxiliary tool calls and stays advisory.
func ParseZaiQuota(body []byte, now time.Time) ([]RateWindow, error) {
	var payload struct {
		Success *bool `json:"success"`
		Data    struct {
			Limits []map[string]any `json:"limits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("zai quota: invalid JSON response")
	}
	if payload.Success != nil && !*payload.Success {
		return nil, errors.New("zai quota: request was not successful")
	}
	nowMs := float64(now.UnixMilli())
	isToken := func(item map[string]any) bool {
		return item["type"] == "CREDIT_LIMIT" || item["type"] == "TOKENS_LIMIT"
	}
	find := func(match func(map[string]any) bool, unit float64) map[string]any {
		for _, item := range payload.Data.Limits {
			if u, ok := item["unit"].(float64); ok && u == unit && match(item) {
				return item
			}
		}
		return nil
	}
	toWindow := func(limit map[string]any, scope string, mins float64) (RateWindow, bool) {
		if limit == nil {
			return RateWindow{}, false
		}
		pct, ok := plainNumberValue(limit["percentage"])
		if !ok {
			return RateWindow{}, false
		}
		left := 100 - pct
		if limit["type"] != "CREDIT_LIMIT" {
			if remaining, ok := plainNumberValue(limit["remaining"]); ok {
				left = remaining
			}
		}
		var resetSec float64
		if at, ok := plainNumberValue(limit["nextResetTime"]); ok && at > nowMs {
			resetSec = (at - nowMs) / 1000
		}
		return RateWindow{Scope: scope, Percent: math.Max(0, math.Min(100, left)), HasReset: resetSec > 0, ResetSec: resetSec, CapturedAt: now, WindowMins: mins}, true
	}
	var windows []RateWindow
	if w, ok := toWindow(find(isToken, 3), "zai:3", 5*60); ok {
		windows = append(windows, w)
	}
	if w, ok := toWindow(find(isToken, 6), "zai:6", 7*24*60); ok {
		windows = append(windows, w)
	}
	if monthly := find(func(item map[string]any) bool { return item["type"] == "TIME_LIMIT" }, 5); monthly != nil {
		total, hasTotal := finiteNumber(monthly["usage"])
		used, hasUsed := finiteNumber(monthly["currentValue"])
		left, hasLeft := finiteNumber(monthly["remaining"])
		if !hasLeft && hasUsed && hasTotal {
			left, hasLeft = total-used, true
		}
		if hasTotal && total > 0 && hasLeft {
			windows = append(windows, RateWindow{Scope: "zai:monthly", Percent: math.Max(0, math.Min(100, left/total*100)), CapturedAt: now, Advisory: true})
		}
	}
	SortRateWindows(windows)
	return windows, nil
}

// FetchZaiQuota reads the account quota from quotaURL (see [ZaiQuotaURL]) with the provider's own API key.
func FetchZaiQuota(ctx context.Context, client *http.Client, quotaURL, apiKey string, now time.Time) ([]RateWindow, error) {
	headers := map[string]string{"Accept-Encoding": "identity"}
	if apiKey != "" && apiKey != "proxy-managed" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	body, err := httpGet(ctx, client, "zai quota", quotaURL, headers)
	if err != nil {
		return nil, err
	}
	return ParseZaiQuota(body, now)
}

// CopilotAPIBase mirrors Pi's Copilot OAuth: an enterprise domain's REST API lives at api.<domain>.
func CopilotAPIBase(enterpriseURL string) string {
	enterpriseURL = strings.TrimSpace(enterpriseURL)
	if enterpriseURL == "" {
		return "https://api.github.com"
	}
	if !strings.Contains(enterpriseURL, "://") {
		enterpriseURL = "https://" + enterpriseURL
	}
	u, err := url.Parse(enterpriseURL)
	if err != nil || u.Hostname() == "" {
		return "https://api.github.com"
	}
	return "https://api." + u.Hostname()
}

// ParseCopilotCredits formats the premium-request allowance as "remaining/total". Invalid or unknown capacity
// returns false so it can never masquerade as an exhausted quota.
func ParseCopilotCredits(body []byte) (string, bool) {
	var payload struct {
		QuotaSnapshots struct {
			Premium map[string]any `json:"premium_interactions"`
		} `json:"quota_snapshots"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", false
	}
	q := payload.QuotaSnapshots.Premium
	remaining, okR := finiteNumber(q["remaining"])
	if !okR {
		remaining, okR = finiteNumber(q["quota_remaining"])
	}
	total, okT := finiteNumber(q["entitlement"])
	if !okT {
		total, okT = finiteNumber(q["limit"])
	}
	if !okR || !okT || total <= 0 {
		return "", false
	}
	return strconv.FormatFloat(math.Max(0, math.Floor(remaining)), 'f', 0, 64) + "/" + strconv.FormatFloat(math.Floor(total), 'f', 0, 64), true
}

// ReadCopilotCredential reads the GitHub Copilot OAuth token (and enterprise domain) from Pi's auth.json. A
// missing file or entry returns an empty token and no error; an unreadable or malformed file is an error.
func ReadCopilotCredential(authPath string) (token, enterpriseURL string, err error) {
	entry, err := readAuthEntry(authPath, CopilotProvider)
	if err != nil || entry == nil {
		return "", "", err
	}
	token, _ = entry["refresh"].(string)
	enterpriseURL, _ = entry["enterpriseUrl"].(string)
	return token, strings.TrimSpace(enterpriseURL), nil
}

func readAuthEntry(path, provider string) (map[string]any, error) {
	data, err := readCapped(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var auth map[string]json.RawMessage
	if json.Unmarshal(data, &auth) != nil {
		return nil, fmt.Errorf("decode %s: invalid JSON object", filepath.Base(path))
	}
	var entry map[string]any
	if raw, ok := auth[provider]; ok && json.Unmarshal(raw, &entry) != nil {
		return nil, nil
	}
	return entry, nil
}

var errNotRegularFile = errors.New("not a regular file")

// readCapped reads only regular files, with a descriptor check before any read.
// The platform opener must not block on special files substituted during open.
func readCapped(path string) ([]byte, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &os.PathError{Op: "read", Path: path, Err: errNotRegularFile}
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if len(data) > MaxResponseBytes {
		return nil, fmt.Errorf("read %s: file exceeds 1 MiB", filepath.Base(path))
	}
	return data, nil
}

func readJSONCapped(path string, value any) (bool, error) {
	data, err := readCapped(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if json.Unmarshal(data, value) != nil {
		return false, fmt.Errorf("decode %s: invalid JSON", filepath.Base(path))
	}
	return true, nil
}

// FetchCopilotCredits asks the Copilot REST API for the premium-request allowance. baseURL comes from
// [CopilotAPIBase]; the token is only ever sent there.
func FetchCopilotCredits(ctx context.Context, client *http.Client, baseURL, token string) (string, error) {
	body, err := httpGet(ctx, client, "copilot credits", baseURL+"/copilot_internal/user", map[string]string{
		"Accept": "application/json", "Authorization": "Bearer " + token, "User-Agent": "pig-better-footer",
	})
	if err != nil {
		return "", err
	}
	credits, ok := ParseCopilotCredits(body)
	if !ok {
		return "", errors.New("copilot credits: no premium allowance in the response")
	}
	return credits, nil
}

// Environment is what the OpenCode Go key and config lookups need from the process; tests replace it.
type Environment struct {
	Getenv func(string) string
	Home   string
	GOOS   string
}

func (e Environment) env(name string) string {
	getenv := e.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return strings.TrimSpace(getenv(name))
}

// OpenCodeAuthPaths lists where the opencode CLI keeps auth.json, in priority order.
func (e Environment) OpenCodeAuthPaths() []string {
	var paths []string
	if dir := e.env("OPENCODE_DATA_DIR"); dir != "" {
		paths = append(paths, filepath.Join(dir, "auth.json"), filepath.Join(dir, "opencode", "auth.json"))
	}
	xdg := e.env("XDG_DATA_HOME")
	if xdg == "" && e.Home != "" {
		xdg = filepath.Join(e.Home, ".local", "share")
	}
	if xdg != "" {
		paths = append(paths, filepath.Join(xdg, "opencode", "auth.json"))
	}
	if e.Home != "" {
		paths = append(paths, filepath.Join(e.Home, ".local", "share", "opencode", "auth.json"))
		if e.GOOS == "darwin" {
			paths = append(paths, filepath.Join(e.Home, "Library", "Application Support", "opencode", "auth.json"))
		}
	}
	for _, name := range []string{"APPDATA", "LOCALAPPDATA"} {
		if dir := e.env(name); dir != "" {
			paths = append(paths, filepath.Join(dir, "opencode", "auth.json"))
		}
	}
	return paths
}

// OpenCodeGoAPIKey resolves the OpenCode Go key: OPENCODE_GO_API_KEY, then Pi's own key while the provider's
// models point at opencode.ai (piKey, "" when that does not apply), then the opencode CLI's auth.json. An
// unreadable or malformed auth.json is skipped; its error is returned only when no other candidate gave a key.
func (e Environment) OpenCodeGoAPIKey(piKey string) (string, error) {
	if key := e.env("OPENCODE_GO_API_KEY"); key != "" {
		return key, nil
	}
	if key := strings.TrimSpace(piKey); key != "" {
		return key, nil
	}
	var firstErr error
	for _, path := range e.OpenCodeAuthPaths() {
		data, err := readCapped(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			firstErr = errors.Join(firstErr, err)
			continue
		}
		var parsed map[string]json.RawMessage
		if json.Unmarshal(data, &parsed) != nil {
			firstErr = errors.Join(firstErr, fmt.Errorf("decode %s: invalid JSON object", path))
			continue
		}
		var entry any
		if raw, ok := parsed[OpenCodeGoID]; ok {
			_ = json.Unmarshal(raw, &entry)
		}
		key := ""
		switch v := entry.(type) {
		case string:
			key = v
		case map[string]any:
			key, _ = v["key"].(string)
		}
		if key = strings.TrimSpace(key); key != "" {
			return key, nil
		}
	}
	return "", firstErr
}

// UsesOpenCodeHost reports whether any of the provider's model base URLs points at opencode.ai, so Pi's key for
// another host is never sent there.
func UsesOpenCodeHost(baseURLs []string) bool {
	hosts := make([]string, 0, len(baseURLs))
	for _, base := range baseURLs {
		hosts = append(hosts, hostOf(base))
	}
	return onHost(hosts, "opencode.ai")
}

// ParseOpenCodeGoUsage maps the official usage API: rolling (5h), weekly (7d) and monthly (billing cycle, 30d
// only as a sort key). The API reports the USED percent.
func ParseOpenCodeGoUsage(body []byte, now time.Time) ([]RateWindow, error) {
	var payload struct {
		Usage map[string]struct {
			Percent  *float64 `json:"percent"`
			ResetsAt string   `json:"resetsAt"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Usage == nil {
		return nil, errors.New("opencode-go usage: unexpected response")
	}
	var windows []RateWindow
	for _, spec := range []struct {
		key, scope string
		mins       float64
	}{{"rolling", "opencode-go:5h", 5 * 60}, {"weekly", "opencode-go:7d", 7 * 24 * 60}, {"monthly", "opencode-go:monthly", 30 * 24 * 60}} {
		w, ok := payload.Usage[spec.key]
		if !ok || w.Percent == nil || math.IsNaN(*w.Percent) || math.IsInf(*w.Percent, 0) {
			continue
		}
		used := math.Max(0, math.Min(100, *w.Percent))
		var resetSec float64
		if at, ok := parseDate(w.ResetsAt); ok {
			resetSec = math.Max(0, at.Sub(now).Seconds())
		}
		windows = append(windows, RateWindow{Scope: spec.scope, Percent: 100 - used, HasReset: resetSec > 0, ResetSec: resetSec, CapturedAt: now, WindowMins: spec.mins})
	}
	SortRateWindows(windows)
	return windows, nil
}

var (
	dashUsage   = regexp.MustCompile(`usagePercent:(\d+(?:\.\d+)?)`)
	dashReset   = regexp.MustCompile(`resetInSec:(\d+(?:\.\d+)?)`)
	dashObjects = map[string]*regexp.Regexp{
		"rolling": regexp.MustCompile(`rollingUsage:\$R\[\d+\]=\{([^}]*)\}`),
		"weekly":  regexp.MustCompile(`weeklyUsage:\$R\[\d+\]=\{([^}]*)\}`),
		"monthly": regexp.MustCompile(`monthlyUsage:\$R\[\d+\]=\{([^}]*)\}`),
	}
)

// ParseOpenCodeGoDashboard reads the dashboard's embedded React payload, e.g. rollingUsage:$R[3]={usagePercent:17.5,resetInSec:2345.6}.
func ParseOpenCodeGoDashboard(html string, now time.Time) []RateWindow {
	var windows []RateWindow
	for _, spec := range []struct {
		key, scope string
		mins       float64
	}{{"rolling", "opencode-go:5h", 5 * 60}, {"weekly", "opencode-go:7d", 7 * 24 * 60}, {"monthly", "opencode-go:monthly", 30 * 24 * 60}} {
		object := dashObjects[spec.key].FindStringSubmatch(html)
		if object == nil {
			continue
		}
		used := dashUsage.FindStringSubmatch(object[1])
		if used == nil {
			continue
		}
		pct, _ := strconv.ParseFloat(used[1], 64)
		pct = math.Max(0, math.Min(100, pct))
		var resetSec float64
		if reset := dashReset.FindStringSubmatch(object[1]); reset != nil {
			v, _ := strconv.ParseFloat(reset[1], 64)
			resetSec = math.Max(0, round(v))
		}
		windows = append(windows, RateWindow{Scope: spec.scope, Percent: math.Max(0, math.Min(100, 100-pct)), HasReset: resetSec > 0, ResetSec: resetSec, CapturedAt: now, WindowMins: spec.mins})
	}
	SortRateWindows(windows)
	return windows
}

// OpenCodeGoCookie holds the dashboard credentials of the cookie-only fallback.
type OpenCodeGoCookie struct{ WorkspaceID, AuthCookie string }

// OpenCodeQuotaConfigPaths lists the 0600 config files with the dashboard credentials, in priority order.
func (e Environment) OpenCodeQuotaConfigPaths() []string {
	rel := []string{"opencode", "opencode-quota", "opencode-go.json"}
	join := func(base string) string { return filepath.Join(append([]string{base}, rel...)...) }
	var paths []string
	if explicit := e.env("OPENCODE_GO_QUOTA_CONFIG"); explicit != "" {
		paths = append(paths, explicit)
	}
	xdg := e.env("XDG_CONFIG_HOME")
	if xdg == "" && e.Home != "" {
		xdg = filepath.Join(e.Home, ".config")
	}
	if xdg != "" {
		paths = append(paths, join(xdg))
	}
	if e.Home != "" {
		paths = append(paths, join(filepath.Join(e.Home, ".config")))
		if e.GOOS == "darwin" {
			paths = append(paths, join(filepath.Join(e.Home, "Library", "Application Support")))
		}
	}
	for _, name := range []string{"APPDATA", "LOCALAPPDATA"} {
		if dir := e.env(name); dir != "" {
			paths = append(paths, join(dir))
		}
	}
	return paths
}

// OpenCodeGoCookieConfig resolves the dashboard credentials: the first valid 0600 config file (a file readable
// or writable by group or others is skipped, since the cookie is a login credential), then the
// OPENCODE_GO_WORKSPACE_ID and OPENCODE_GO_AUTH_COOKIE variables. ok is false when nothing is configured.
func (e Environment) OpenCodeGoCookieConfig() (OpenCodeGoCookie, bool) {
	for _, path := range e.OpenCodeQuotaConfigPaths() {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || e.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			continue
		}
		data, err := readCapped(path)
		if err != nil {
			continue
		}
		var config struct {
			WorkspaceID string `json:"workspaceId"`
			AuthCookie  string `json:"authCookie"`
		}
		if json.Unmarshal(data, &config) != nil {
			continue
		}
		if id, cookie := strings.TrimSpace(config.WorkspaceID), strings.TrimSpace(config.AuthCookie); id != "" && cookie != "" {
			return OpenCodeGoCookie{id, cookie}, true
		}
	}
	if id, cookie := e.env("OPENCODE_GO_WORKSPACE_ID"), e.env("OPENCODE_GO_AUTH_COOKIE"); id != "" && cookie != "" {
		return OpenCodeGoCookie{id, cookie}, true
	}
	return OpenCodeGoCookie{}, false
}

// OpenCodeGoEndpoints are the URLs of the OpenCode Go sources; production uses [ProductionOpenCodeGo].
type OpenCodeGoEndpoints struct{ Usage, DashboardPrefix string }

// ProductionOpenCodeGo is the official usage API and the dashboard of opencode.ai.
var ProductionOpenCodeGo = OpenCodeGoEndpoints{Usage: openCodeUsageURL, DashboardPrefix: openCodeDashURL}

// FetchOpenCodeGoUsage reads the official usage API with the account key.
func FetchOpenCodeGoUsage(ctx context.Context, client *http.Client, endpoints OpenCodeGoEndpoints, apiKey string, now time.Time) ([]RateWindow, error) {
	body, err := httpGet(ctx, client, "opencode-go usage", endpoints.Usage, map[string]string{
		"Accept": "application/json", "Accept-Encoding": "identity", "Authorization": "Bearer " + apiKey, "User-Agent": "pig-better-footer",
	})
	if err != nil {
		return nil, err
	}
	return ParseOpenCodeGoUsage(body, now)
}

// FetchOpenCodeGoDashboard scrapes the workspace dashboard, the fallback for cookie-only setups.
func FetchOpenCodeGoDashboard(ctx context.Context, client *http.Client, endpoints OpenCodeGoEndpoints, cookie OpenCodeGoCookie, now time.Time) ([]RateWindow, error) {
	body, err := httpGet(ctx, client, "opencode-go dashboard", endpoints.DashboardPrefix+"/"+url.PathEscape(cookie.WorkspaceID)+"/go", map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Encoding": "identity", "Cookie": "auth=" + cookie.AuthCookie, "User-Agent": "pig-better-footer",
	})
	if err != nil {
		return nil, err
	}
	windows := ParseOpenCodeGoDashboard(string(body), now)
	if len(windows) == 0 {
		return nil, errors.New("opencode-go dashboard: no usage windows in the page")
	}
	return windows, nil
}
