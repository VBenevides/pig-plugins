// Package cursorbridge implements the published Cursor SDK Bridge v1.2.0 contract.
// It does not use Cursor's private inference protocol.
package cursorbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const Version = "1.2.0"
const maxMessage = 8 << 20
const control = "SdkBridgeControlService/"
const agents = "SdkAgentService/"
const catalog = "SdkCursorService/"

// Client owns one loopback connection pool. It never retries RPCs or follows redirects.
type Client struct {
	endpoint, token string
	http            *http.Client
}

func NewClient(endpoint, token string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("Cursor bridge requires a loopback HTTP endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" || token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("invalid Cursor bridge address or bearer token")
	}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 4, ResponseHeaderTimeout: 30 * time.Second}
	return &Client{strings.TrimRight(endpoint, "/"), token, &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

// RPCError omits remote messages because they may contain credentials or prompts.
type RPCError struct {
	Code   string
	Status int
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("Cursor bridge RPC failed (%s, HTTP %d)", e.Code, e.Status)
}
func decodeError(data []byte, status int) error {
	var wire struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(data, &wire)
	// Only known Connect codes may enter diagnostics.
	switch wire.Code {
	case "canceled", "unknown", "invalid_argument", "deadline_exceeded", "not_found", "already_exists", "permission_denied", "resource_exhausted", "failed_precondition", "aborted", "out_of_range", "unimplemented", "internal", "unavailable", "data_loss", "unauthenticated":
	default:
		wire.Code = "unknown"
	}
	return &RPCError{wire.Code, status}
}

func (c *Client) request(ctx context.Context, method string, input any, stream bool) (*http.Response, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode Cursor %s: %w", method, err)
	}
	if len(body) > maxMessage {
		return nil, errors.New("Cursor request exceeds the bridge message limit")
	}
	contentType := "application/json"
	if stream {
		frame := make([]byte, 5+len(body))
		binary.BigEndian.PutUint32(frame[1:5], uint32(len(body)))
		copy(frame[5:], body)
		body = frame
		contentType = "application/connect+json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/sdk.v1."+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Connect-Protocol-Version", "1")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call Cursor %s: %w", method, err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, decodeError(data, response.StatusCode)
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), contentType) {
		response.Body.Close()
		return nil, errors.New("unexpected Cursor bridge response type")
	}
	return response, nil
}

func (c *Client) Unary(ctx context.Context, method string, input, output any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := c.request(ctx, method, input, false)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxMessage+1))
	if err != nil {
		return fmt.Errorf("read Cursor %s: %w", method, err)
	}
	if len(data) > maxMessage {
		return errors.New("Cursor response exceeds the bridge message limit")
	}
	if err = json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode Cursor %s: %w", method, err)
	}
	return nil
}

type BridgeVersion struct {
	BridgeVersion   string   `json:"bridgeVersion"`
	ProtocolVersion string   `json:"protocolVersion"`
	Capabilities    []string `json:"capabilities"`
}

func (c *Client) Check(ctx context.Context) error {
	var pong map[string]any
	if err := c.Unary(ctx, control+"Ping", struct{}{}, &pong); err != nil {
		return err
	}
	var version BridgeVersion
	if err := c.Unary(ctx, control+"GetVersion", struct{}{}, &version); err != nil {
		return err
	}
	if version.ProtocolVersion != "sdk.v1" || version.BridgeVersion != Version {
		return errors.New("Cursor bridge version does not match the pinned sdk.v1 contract")
	}
	return nil
}

// Stream requires the Connect end envelope. Send also checks the SDK result.
// It does not reconnect after a partial response.
func (c *Client) Stream(ctx context.Context, method string, input any, consume func(json.RawMessage) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	response, err := c.request(ctx, method, input, true)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	for count := 0; count < 100000; count++ {
		var header [5]byte
		if _, err = io.ReadFull(response.Body, header[:]); err != nil {
			return fmt.Errorf("incomplete Cursor stream: %w", err)
		}
		size := binary.BigEndian.Uint32(header[1:])
		if size > maxMessage {
			return errors.New("Cursor stream frame exceeds the message limit")
		}
		if header[0] != 0 && header[0] != 2 {
			return errors.New("unsupported Cursor stream frame flags")
		}
		data := make([]byte, size)
		if _, err = io.ReadFull(response.Body, data); err != nil {
			return fmt.Errorf("read Cursor stream frame: %w", err)
		}
		if header[0] == 2 {
			var end struct {
				Error json.RawMessage `json:"error"`
			}
			if err = json.Unmarshal(data, &end); err != nil {
				return errors.New("invalid Cursor end envelope")
			}
			if len(end.Error) > 0 && string(end.Error) != "null" {
				return decodeError(end.Error, 200)
			}
			return nil
		}
		if !json.Valid(data) {
			return errors.New("invalid JSON in Cursor stream")
		}
		if err = consume(data); err != nil {
			return err
		}
	}
	return errors.New("Cursor stream exceeds the event limit")
}

type Parameter struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}
type Selection struct {
	ID     string      `json:"id"`
	Params []Parameter `json:"params,omitempty"`
}
type ParameterDefinition struct {
	ID     string      `json:"id"`
	Values []Parameter `json:"values"`
}
type Model struct {
	ID          string                `json:"id"`
	DisplayName string                `json:"displayName"`
	Parameters  []ParameterDefinition `json:"parameters"`
	Variants    []struct {
		Params    []Parameter `json:"params"`
		IsDefault bool        `json:"isDefault"`
	} `json:"variants"`
}

func (c *Client) Models(ctx context.Context, key string) ([]Model, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("Cursor API key is required")
	}
	var result struct {
		Items []Model `json:"items"`
	}
	err := c.Unary(ctx, catalog+"ListModels", map[string]any{"options": map[string]string{"apiKey": key}}, &result)
	return result.Items, err
}
func (c *Client) Cancel(ctx context.Context, agentID, runID string) error {
	var out struct{}
	return c.Unary(ctx, agents+"CancelRun", map[string]string{"agentId": agentID, "runId": runID}, &out)
}
func (c *Client) CloseAgent(ctx context.Context, agentID string) error {
	var out struct{}
	return c.Unary(ctx, agents+"CloseAgent", map[string]string{"agentId": agentID}, &out)
}
