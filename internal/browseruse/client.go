package browseruse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxResponseBytes = 8 << 20
const elementKey = "element-6066-11e4-a52e-4f735466cecf"

// Client owns only the WebDriver session it creates, never an existing browser.
// Operations are serialized and are never retried, including after cancellation.
// A closed client cannot create another session.
type Client struct {
	mu         sync.Mutex
	endpoint   string
	mode       Mode
	http       *http.Client
	session    string
	refs       map[string]element
	generation uint64
	closed     bool
}

type element struct{ ID, Type string }

func NewClient(mode Mode, endpoint string) (*Client, error) {
	if mode != Chrome && mode != Firefox {
		return nil, errors.New("browser mode is off")
	}
	if err := ValidateEndpoint(endpoint); err != nil {
		return nil, err
	}
	return &Client{mode: mode, endpoint: strings.TrimRight(endpoint, "/"), http: &http.Client{
		Timeout:       35 * time.Second,
		Transport:     &http.Transport{Proxy: nil, MaxConnsPerHost: 1, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("browser driver redirects are disabled") },
	}}, nil
}

func (c *Client) request(ctx context.Context, method, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode browser request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return fmt.Errorf("create browser request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("browser driver request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read browser driver response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return errors.New("browser driver response exceeds 8 MiB")
	}
	var envelope struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return errors.New("browser driver returned invalid JSON")
	}
	if len(envelope.Value) == 0 {
		return errors.New("browser driver response has no value")
	}
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(envelope.Value, &failure)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || failure.Error != "" {
		// Never return the remote message or stack: these can contain URLs, form data,
		// credentials, or page instructions. Keep only known protocol error categories.
		code := "request failed"
		switch failure.Error {
		case "stale element reference", "no such element", "no such window", "invalid session id", "session not created", "element not interactable", "element click intercepted", "timeout", "invalid argument", "unexpected alert open":
			code = failure.Error
		}
		return fmt.Errorf("browser driver: %s (HTTP %d); do not retry a write automatically", code, resp.StatusCode)
	}
	if output != nil {
		if err := json.Unmarshal(envelope.Value, output); err != nil {
			return errors.New("browser driver returned an invalid value")
		}
	}
	return nil
}

var opaqueID = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,256}$`)

func (c *Client) start(ctx context.Context) error {
	if c.session != "" {
		return nil
	}
	caps := map[string]any{"browserName": string(c.mode), "acceptInsecureCerts": false,
		"unhandledPromptBehavior": "dismiss and notify",
		"timeouts":                map[string]any{"implicit": 0, "pageLoad": 25000, "script": 10000},
	}
	// Only fresh profiles are supported. Never attach to or copy a personal profile.
	if c.mode == Chrome {
		caps["goog:chromeOptions"] = map[string]any{"prefs": map[string]any{"download_restrictions": 3, "credentials_enable_service": false, "profile.password_manager_enabled": false}}
	} else {
		caps["moz:firefoxOptions"] = map[string]any{"prefs": map[string]any{"browser.download.useDownloadDir": false, "browser.helperApps.alwaysAsk.force": true, "signon.rememberSignons": false}}
	}
	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.request(ctx, "POST", "/session", map[string]any{"capabilities": map[string]any{"alwaysMatch": caps}}, &result); err != nil {
		return fmt.Errorf("start %s WebDriver session: %w", c.mode, err)
	}
	if !opaqueID.MatchString(result.SessionID) {
		return errors.New("browser driver returned an invalid session ID")
	}
	c.session = result.SessionID
	return nil
}

// Close deletes only this client's automation session. It never kills the driver
// server or a browser outside that session. Failed cleanup remains observable.
func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.session == "" {
		return nil
	}
	id := c.session
	c.session = ""
	c.invalidate()
	return c.request(ctx, "DELETE", "/session/"+id, nil, nil)
}

func (c *Client) invalidate() { c.refs = nil; c.generation++ }

// Run confirms state changes before dispatch. Arbitrary JavaScript, CDP,
// uploads, and host scripts are absent.
func (c *Client) Run(ctx context.Context, action string, args map[string]any, confirm func(string, string) (bool, error)) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("browser session is closed")
	}
	if err := ConfirmAction(ctx, action, confirm); err != nil {
		return nil, err
	}
	get := func(key string) string { v, _ := args[key].(string); return v }
	if action == "navigate" {
		if err := ValidateNavigation(get("url")); err != nil {
			return nil, err
		}
	}
	if action == "fill" && len(get("text")) > 16384 {
		return nil, errors.New("browser input exceeds 16384 bytes")
	}
	var target element
	if action == "click" || action == "fill" {
		var ok bool
		target, ok = c.refs[get("ref")]
		if !ok {
			return nil, errors.New("unknown browser reference; take a new snapshot")
		}
		if target.Type == "file" {
			return nil, errors.New("browser file uploads are disabled")
		}
	}
	if action == "select_tab" && !opaqueID.MatchString(get("handle")) {
		return nil, errors.New("invalid browser tab handle")
	}
	if err := c.start(ctx); err != nil {
		return nil, err
	}
	path := "/session/" + c.session
	switch action {
	case "navigate":
		c.invalidate()
		if err := c.request(ctx, "POST", path+"/url", map[string]any{"url": get("url")}, nil); err != nil {
			return nil, err
		}
		return "Navigation complete. Take a new browser_snapshot.", nil
	case "snapshot":
		return c.snapshot(ctx, path)
	case "click", "fill":
		c.invalidate()
		suffix := "/click"
		input := map[string]any{}
		if action == "fill" {
			// Use the native DOM setter with bubbling input/change events for controlled
			// fields. The script is fixed and the value remains a separate argument.
			// Recheck the live type to prevent a DOM change from enabling file uploads.
			var filled bool
			err := c.request(ctx, "POST", path+"/execute/sync", map[string]any{"script": fillScript, "args": []any{map[string]string{elementKey: target.ID}, get("text")}}, &filled)
			if err != nil {
				return nil, err
			}
			if !filled {
				return nil, errors.New("browser field is not a supported text input")
			}
			return "Field updated. Take a new browser_snapshot.", nil
		}
		if err := c.request(ctx, "POST", path+"/element/"+url.PathEscape(target.ID)+suffix, input, nil); err != nil {
			return nil, err
		}
		return "Click complete. Take a new browser_snapshot.", nil
	case "screenshot":
		var encoded string
		if err := c.request(ctx, "GET", path+"/screenshot", nil, &encoded); err != nil {
			return nil, err
		}
		image, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(image) < 8 || !bytes.Equal(image[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
			return nil, errors.New("browser driver returned an invalid PNG screenshot")
		}
		return Screenshot{Data: encoded}, nil
	case "tabs":
		var handles []string
		if err := c.request(ctx, "GET", path+"/window/handles", nil, &handles); err != nil {
			return nil, err
		}
		if len(handles) > 100 {
			return nil, errors.New("browser has more than 100 tabs")
		}
		for _, h := range handles {
			if !opaqueID.MatchString(h) {
				return nil, errors.New("browser returned an invalid tab handle")
			}
		}
		return handles, nil
	case "select_tab":
		c.invalidate()
		if err := c.request(ctx, "POST", path+"/window", map[string]string{"handle": get("handle")}, nil); err != nil {
			return nil, err
		}
		return "Tab selected. Take a new browser_snapshot.", nil
	}
	return nil, errors.New("unsupported browser action")
}

type Screenshot struct{ Data string }

func (c *Client) snapshot(ctx context.Context, path string) (any, error) {
	c.invalidate()
	var page struct {
		Title    string `json:"title"`
		Text     string `json:"text"`
		Elements []struct {
			Element map[string]string `json:"element"`
			Tag     string            `json:"tag"`
			Type    string            `json:"type"`
			Label   string            `json:"label"`
		} `json:"elements"`
	}
	if err := c.request(ctx, "POST", path+"/execute/sync", map[string]any{"script": snapshotScript, "args": []any{}}, &page); err != nil {
		return nil, err
	}
	if len(page.Elements) > 150 || len(page.Text) > 100000 || len(page.Title) > 4000 {
		return nil, errors.New("browser snapshot exceeds its limits")
	}
	c.refs = make(map[string]element)
	elements := make([]map[string]string, 0, len(page.Elements))
	for i, el := range page.Elements {
		id := el.Element[elementKey]
		if !opaqueID.MatchString(id) || len(el.Label) > 2000 || len(el.Tag) > 50 || len(el.Type) > 50 {
			c.invalidate()
			return nil, errors.New("browser returned an invalid snapshot element")
		}
		ref := fmt.Sprintf("r%d-%d", c.generation, i+1)
		c.refs[ref] = element{ID: id, Type: el.Type}
		elements = append(elements, map[string]string{"ref": ref, "tag": el.Tag, "type": el.Type, "label": el.Label})
	}
	return map[string]any{"untrustedPageContent": true, "title": page.Title, "text": page.Text, "elements": elements}, nil
}

const snapshotScript = `return {
 title: document.title.slice(0,1000),
 text: (document.body ? document.body.innerText : '').slice(0,20000),
 elements: Array.from(document.querySelectorAll('a[href],button,input,textarea,select,[role="button"]'))
 .filter(e => e.getClientRects().length && !e.disabled && e.type !== 'file' && e.type !== 'hidden')
 .slice(0,150).map(e => ({element:e, tag:e.tagName.toLowerCase(),type:e.type || '',
 label:(e.getAttribute('aria-label') || e.innerText || e.getAttribute('placeholder') || e.name || '').slice(0,400)}))
};`

const fillScript = `const e=arguments[0], value=arguments[1];
if (!(e instanceof HTMLInputElement || e instanceof HTMLTextAreaElement) ||
    (e instanceof HTMLInputElement && !['text','search','email','password','url','tel','number'].includes(e.type))) return false;
const proto=e instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
Object.getOwnPropertyDescriptor(proto,'value').set.call(e,value);
e.dispatchEvent(new Event('input',{bubbles:true}));
e.dispatchEvent(new Event('change',{bubbles:true}));return true;`
