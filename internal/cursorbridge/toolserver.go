package cursorbridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Valid callbacks stay pending. PiG executes the recorded proposals through its
// normal approval loop. The pending callbacks are then answered with the real
// tool results (Answer), or the run is disposed and a fresh turn receives the
// results in its transcript. Cursor never receives a fabricated success.

const callbackPath = "/sdk.v1.SdkCustomToolCallbackService/CallCustomTool"
const maxCallbackBody = 4 << 20
const maxProposals = 32

// ToolDef is a PiG tool declared to Cursor. Cursor only proposes calls; PiG runs them.
type ToolDef struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Proposal is a tool call Cursor asked PiG to run.
type Proposal struct {
	ID   string
	Name string
	Args json.RawMessage
}

// ToolResult is the real result PiG produced for a proposal. Call is the call as
// the host recorded it, so a changed name or argument is never answered.
type ToolResult struct {
	Call    Proposal
	Text    string
	IsError bool
}

// maxResultBytes bounds one answered result. A larger one is replayed in a
// fresh transcript instead.
const maxResultBytes = 4 << 20

// pendingCall is a callback that waits for its answer.
type pendingCall struct {
	proposal  Proposal
	generated bool          // PiG made the identifier because Cursor sent none
	done      chan struct{} // closed after body is set
	body      []byte
}

// ToolServer is a bearer-authenticated loopback endpoint for one turn. It only
// accepts declared tool names from the expected agent and records proposals.
type ToolServer struct {
	URL, Token string
	server     *http.Server
	listener   net.Listener
	allowed    map[string]bool
	onProposal func()
	done       chan struct{}
	closeOnce  sync.Once
	active     chan struct{}
	failed     chan struct{} // closed by the first Fail

	mu           sync.Mutex
	generatedIDs int
	agentID      string
	calls        []*pendingCall // unanswered callbacks of the current round
	answered     map[string]bool
	lost         bool // a callback ended before it was answered
	violation    error
}

func NewToolServer(tools []ToolDef, onProposal func()) (*ToolServer, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("create Cursor callback token: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for Cursor tool callbacks: %w", err)
	}
	s := &ToolServer{Token: hex.EncodeToString(raw), listener: listener, allowed: map[string]bool{}, answered: map[string]bool{}, onProposal: onProposal, done: make(chan struct{}), failed: make(chan struct{}), active: make(chan struct{}, maxProposals)}
	for _, tool := range tools {
		s.allowed[tool.Name] = true
	}
	s.URL = "http://" + listener.Addr().String()
	// No ReadTimeout: it would cancel the request context of a callback that
	// waits for a tool result. The body read has its own deadline in serve.
	s.server = &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { _ = s.server.Serve(listener) }()
	return s, nil
}

// Expect binds callbacks to one agent. Callbacks for any other agent are refused.
func (s *ToolServer) Expect(agentID string) { s.mu.Lock(); s.agentID = agentID; s.mu.Unlock() }

func (s *ToolServer) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// Results returns the proposals of the current round and any boundary violation.
func (s *ToolServer) Results() ([]Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proposals := make([]Proposal, 0, len(s.calls))
	for _, call := range s.calls {
		proposals = append(proposals, call.proposal)
	}
	return proposals, s.violation
}

// Answer returns real tool results to the waiting callbacks. It answers all of
// them or none: the results must cover exactly the pending calls, with the
// same names and arguments, and no callback may have been lost.
func (s *ToolServer) Answer(results []ToolResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.violation != nil:
		return s.violation
	case s.lost:
		return errors.New("cursor dropped a pending tool callback")
	case len(s.calls) == 0 || len(results) != len(s.calls):
		return fmt.Errorf("tool results do not match the %d pending Cursor calls", len(s.calls))
	}
	byID := make(map[string]ToolResult, len(results))
	for _, result := range results {
		if _, repeated := byID[result.Call.ID]; repeated || result.Call.ID == "" {
			return errors.New("tool results repeat or omit a call identifier")
		}
		byID[result.Call.ID] = result
	}
	bodies := make([][]byte, len(s.calls))
	for i, call := range s.calls {
		result, ok := byID[call.proposal.ID]
		if !ok || result.Call.Name != call.proposal.Name || !sameJSON(result.Call.Args, call.proposal.Args) {
			return errors.New("tool results do not match the pending Cursor calls")
		}
		if len(result.Text) > maxResultBytes {
			return fmt.Errorf("tool result is %d bytes, over the %d byte limit for a parked run", len(result.Text), maxResultBytes)
		}
		body, err := json.Marshal(map[string]any{"result": map[string]any{
			"content": []any{map[string]string{"type": "text", "text": result.Text}},
			"isError": result.IsError,
		}})
		if err != nil {
			return fmt.Errorf("encode tool result: %w", err)
		}
		bodies[i] = body
	}
	for i, call := range s.calls {
		call.body = bodies[i]
		close(call.done)
		s.answered[call.proposal.ID] = true
	}
	s.calls = nil
	return nil
}

// Lost reports whether a callback ended before it was answered.
func (s *ToolServer) Lost() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lost
}

func sameJSON(a, b json.RawMessage) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

// Fail records a boundary violation. The first one wins.
func (s *ToolServer) Fail(err error) {
	s.mu.Lock()
	if s.violation == nil {
		s.violation = err
		close(s.failed)
	}
	s.mu.Unlock()
}

func deny(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Messages are fixed text: tool names and arguments may be sensitive.
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": "tool call refused"})
}

func (s *ToolServer) serve(w http.ResponseWriter, r *http.Request) {
	traceCallbackHit("received")
	const prefix = "Bearer "
	got := r.Header.Get("Authorization")
	if r.Method != http.MethodPost || r.URL.Path != callbackPath ||
		!strings.HasPrefix(got, prefix) || len(got) <= len(prefix) || subtle.ConstantTimeCompare([]byte(got[len(prefix):]), []byte(s.Token)) != 1 {
		deny(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		deny(w, http.StatusUnsupportedMediaType, "invalid_argument")
		return
	}
	select {
	case s.active <- struct{}{}:
		defer func() { <-s.active }()
	default:
		s.Fail(errors.New("cursor exceeded the pending tool callback limit"))
		deny(w, http.StatusTooManyRequests, "resource_exhausted")
		return
	}
	// The bridge may send chunked bodies; net/http decodes them. Bound them anyway,
	// in size and in time, then clear the deadline: the callback stays open.
	control := http.NewResponseController(w)
	_ = control.SetReadDeadline(time.Now().Add(30 * time.Second))
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBody+1))
	_ = control.SetReadDeadline(time.Time{})
	if err != nil || len(body) > maxCallbackBody {
		s.Fail(errors.New("cursor sent an unreadable or oversized tool callback"))
		deny(w, http.StatusBadRequest, "invalid_argument")
		return
	}
	var call struct {
		ToolName   string          `json:"toolName"`
		Args       json.RawMessage `json:"args"`
		ToolCallID string          `json:"toolCallId"`
		AgentID    string          `json:"agentId"`
	}
	if json.Unmarshal(body, &call) != nil {
		s.Fail(errors.New("cursor sent a malformed tool callback"))
		deny(w, http.StatusBadRequest, "invalid_argument")
		return
	}
	s.mu.Lock()
	if s.agentID == "" || call.AgentID != s.agentID {
		s.mu.Unlock()
		// A late callback from a cancelled earlier agent is refused. It cannot
		// affect this turn, so it must not fail it.
		traceCallbackHit("foreign-agent")
		deny(w, http.StatusConflict, "canceled")
		return
	}
	if !s.allowed[call.ToolName] {
		s.mu.Unlock()
		s.Fail(errors.New("cursor requested a tool that PiG did not offer"))
		deny(w, http.StatusForbidden, "permission_denied")
		return
	}
	args := bytes.TrimSpace(call.Args)
	if len(args) == 0 || string(args) == "null" {
		args = []byte("{}")
	}
	proposal := Proposal{ID: call.ToolCallID, Name: call.ToolName, Args: append(json.RawMessage(nil), args...)}
	if proposal.ID != "" && s.answered[proposal.ID] {
		s.mu.Unlock()
		s.Fail(errors.New("cursor reused the identifier of an answered tool call"))
		deny(w, http.StatusForbidden, "permission_denied")
		return
	}
	var pending *pendingCall
	for _, existing := range s.calls {
		if proposal.ID != "" && existing.proposal.ID == proposal.ID {
			if existing.proposal.Name != proposal.Name || !bytes.Equal(existing.proposal.Args, proposal.Args) {
				s.mu.Unlock()
				s.Fail(errors.New("cursor reused a tool call identifier with different content"))
				deny(w, http.StatusForbidden, "permission_denied")
				return
			}
			pending = existing
		} else if proposal.ID == "" && existing.generated && existing.proposal.Name == proposal.Name && bytes.Equal(existing.proposal.Args, proposal.Args) {
			pending = existing
		}
	}
	fresh := pending == nil
	if fresh {
		if len(s.calls) >= maxProposals {
			s.mu.Unlock()
			s.Fail(errors.New("cursor proposed too many tool calls in one turn"))
			deny(w, http.StatusForbidden, "resource_exhausted")
			return
		}
		generated := proposal.ID == ""
		if generated {
			// PiG answers a call by identifier, so every call needs one.
			s.generatedIDs++
			proposal.ID = fmt.Sprintf("cursor_%d_%s", s.generatedIDs, s.Token[:8])
		}
		pending = &pendingCall{proposal: proposal, generated: generated, done: make(chan struct{})}
		s.calls = append(s.calls, pending)
	}
	s.mu.Unlock()
	if fresh && s.onProposal != nil {
		s.onProposal()
	}
	// A callback is a proposal, not a result. Keep it pending until PiG answers
	// it with the real result or the run is disposed. Returning a fake result
	// can make the model continue with invented tool output.
	select {
	case <-pending.done:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pending.body)
	case <-r.Context().Done():
		s.mu.Lock()
		select {
		case <-pending.done:
		default:
			s.lost = true
		}
		s.mu.Unlock()
	case <-s.done:
		deny(w, http.StatusConflict, "canceled")
	}
}
