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
	"strings"
	"sync"
	"time"
)

// Valid callbacks park until the turn is disposed. PiG executes the recorded
// proposals through its normal approval loop. A fresh turn receives the real
// tool results in its transcript; Cursor never receives a fabricated success.

const callbackPath = "/sdk.v1.SdkCustomToolCallbackService/CallCustomTool"
const maxCallbackBody = 4 << 20
const maxProposals = 32

// ToolDef is a PiG tool offered to Cursor as metadata only.
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

	mu        sync.Mutex
	agentID   string
	proposals []Proposal
	violation error
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
	s := &ToolServer{Token: hex.EncodeToString(raw), listener: listener, allowed: map[string]bool{}, onProposal: onProposal, done: make(chan struct{}), active: make(chan struct{}, maxProposals)}
	for _, tool := range tools {
		s.allowed[tool.Name] = true
	}
	s.URL = "http://" + listener.Addr().String()
	s.server = &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
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

// Results returns recorded proposals and any boundary violation.
func (s *ToolServer) Results() ([]Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Proposal(nil), s.proposals...), s.violation
}

// Fail records a boundary violation. The first one wins.
func (s *ToolServer) Fail(err error) {
	s.mu.Lock()
	if s.violation == nil {
		s.violation = err
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
		s.Fail(errors.New("Cursor exceeded the pending tool callback limit"))
		deny(w, http.StatusTooManyRequests, "resource_exhausted")
		return
	}
	// The bridge may send chunked bodies; net/http decodes them. Bound them anyway.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBody+1))
	if err != nil || len(body) > maxCallbackBody {
		s.Fail(errors.New("Cursor sent an unreadable or oversized tool callback"))
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
		s.Fail(errors.New("Cursor sent a malformed tool callback"))
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
		s.Fail(errors.New("Cursor requested a tool that PiG did not offer"))
		deny(w, http.StatusForbidden, "permission_denied")
		return
	}
	args := bytes.TrimSpace(call.Args)
	if len(args) == 0 || string(args) == "null" {
		args = []byte("{}")
	}
	proposal := Proposal{ID: call.ToolCallID, Name: call.ToolName, Args: append(json.RawMessage(nil), args...)}
	duplicate := false
	for _, existing := range s.proposals {
		if proposal.ID != "" && existing.ID == proposal.ID {
			if existing.Name != proposal.Name || !bytes.Equal(existing.Args, proposal.Args) {
				s.mu.Unlock()
				s.Fail(errors.New("Cursor reused a tool call identifier with different content"))
				deny(w, http.StatusForbidden, "permission_denied")
				return
			}
			duplicate = true
		} else if proposal.ID == "" && existing.Name == proposal.Name && bytes.Equal(existing.Args, proposal.Args) {
			duplicate = true
		}
	}
	if !duplicate {
		if len(s.proposals) >= maxProposals {
			s.mu.Unlock()
			s.Fail(errors.New("Cursor proposed too many tool calls in one turn"))
			deny(w, http.StatusForbidden, "resource_exhausted")
			return
		}
		s.proposals = append(s.proposals, proposal)
	}
	s.mu.Unlock()
	if !duplicate && s.onProposal != nil {
		s.onProposal()
	}
	// A callback is a proposal, not a result. Keep it pending until the SDK run
	// is cancelled and disposed. Returning a fake result can make the model
	// continue with invented tool output before PiG has executed anything.
	select {
	case <-r.Context().Done():
	case <-s.done:
		deny(w, http.StatusConflict, "canceled")
	}
}
