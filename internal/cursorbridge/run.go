package cursorbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// TurnOptions controls one generation.
type TurnOptions struct {
	// Park keeps the agent, its run and the callback server alive when the turn
	// ends in tool calls. Outcome.Session then holds them. The caller must call
	// Session.Resume or Session.Close. A new Run on the same Process closes it.
	Park bool
	// ParkTimeout bounds how long a parked run waits for results. Zero uses
	// DefaultParkTimeout.
	ParkTimeout time.Duration
}

// DefaultParkTimeout is how long a parked run waits for tool results. It stays
// below five minutes because the bridge's HTTP client may drop a callback that
// stays open longer (not verified). A dropped callback is detected by Resume and
// the caller replays the transcript instead.
const DefaultParkTimeout = 4 * time.Minute

// ErrSessionGone means a parked run cannot be resumed: it was closed, timed out,
// replaced by another turn, lost a callback, or the results did not match. The
// caller should replay the transcript on a fresh agent. Nothing was executed.
var ErrSessionGone = errors.New("the parked Cursor run is gone")

// round is the state of one turn on a session: one Run or one Resume.
type round struct {
	emit        func(Event) error
	streamed    bool
	outputBytes int
}

// Session is one agent whose run waits on unanswered tool callbacks. A Process
// runs one turn at a time. A parked session does not hold that slot, so another
// conversation can start. Starting one closes the parked session.
type Session struct {
	p      *Process
	agent  string
	server *ToolServer
	allow  map[string]bool

	cancelRun context.CancelFunc
	sendEnded chan struct{} // closed when the Send stream returned
	gone      chan struct{} // closed when the session is disposed
	proposed  chan struct{} // woken once per round after the grace period

	emitMu sync.Mutex // held while emit runs, so a retired round never emits
	deltas deltaTracker

	mu        sync.Mutex
	closed    bool
	armed     bool // a grace timer is running for this round
	round     *round
	backlog   []Event // output that arrived while no round was waiting
	result    RunResult
	sendErr   error
	timer     *time.Timer
	proposals []Proposal // calls the session waits to have answered

	// Usage accounting. The SDK reports usage per model call and cumulatively.
	calls    []TokenUsage // per-call usage seen in the current round
	seen     TokenUsage   // sum of every per-call usage seen on this run
	reported TokenUsage   // part of the run's usage that earlier rounds returned

	// Selection and Tools are what the agent was created with.
	Selection Selection
	Tools     []ToolDef
}

// Done is closed once the session is disposed, whatever the reason.
func (s *Session) Done() <-chan struct{} { return s.gone }

// Proposals returns the tool calls the parked run waits on.
func (s *Session) Proposals() []Proposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Proposal(nil), s.proposals...)
}

// Run runs one generation on a fresh agent. History is rendered by the caller
// into prompt. Without Park the agent never outlives the turn, so no account or
// branch can observe another's state. With Park and tool calls, the run stays
// open and Outcome.Session holds it.
func (p *Process) Run(ctx context.Context, key string, model Selection, tools []ToolDef, prompt string, emit func(Event) error, opts TurnOptions, images ...Image) (out Outcome, err error) {
	p.turn.Lock()
	defer p.turn.Unlock()
	if p.Client == nil {
		return out, errors.New("cursor bridge is not running")
	}
	// A turn that starts now replaces whatever was parked.
	if stale := p.takeParked(nil); stale != nil {
		out.CleanupError = stale.dispose()
	}
	// The run must outlive the caller's context when it parks, so it has its own.
	runCtx, cancelRun := context.WithCancel(context.Background())
	s := &Session{p: p, allow: map[string]bool{}, cancelRun: cancelRun, sendEnded: make(chan struct{}), gone: make(chan struct{}), proposed: make(chan struct{}, 1), Selection: model, Tools: tools}
	for _, tool := range tools {
		s.allow[tool.Name] = true
	}
	server, err := NewToolServer(tools, s.armGrace)
	if err != nil {
		cancelRun()
		return out, err
	}
	s.server = server
	parked := false
	defer func() {
		if !parked {
			out.CleanupError = errors.Join(out.CleanupError, s.dispose())
		}
	}()
	if err = p.Client.SetToolCallback(ctx, server.URL, server.Token); err != nil {
		return out, fmt.Errorf("register Cursor tool callback: %w", err)
	}
	if s.agent, err = p.Client.Create(ctx, key, model, tools); err != nil {
		return out, fmt.Errorf("create Cursor agent: %w", err)
	}
	server.Expect(s.agent)
	s.round = &round{emit: emit}
	go func() {
		result, sendErr := p.Client.Send(runCtx, s.agent, prompt, images, s.handle)
		s.mu.Lock()
		s.result, s.sendErr = result, sendErr
		s.mu.Unlock()
		close(s.sendEnded)
	}()
	var waitErr error
	out, waitErr = s.wait(ctx, opts.Park)
	if waitErr == nil && opts.Park && len(out.Proposals) > 0 {
		s.park(out.Proposals, opts)
		out.Session = s
		parked = true
	}
	return out, waitErr
}

// park keeps the session and releases the process for other turns.
func (s *Session) park(proposals []Proposal, opts TurnOptions) {
	timeout := opts.ParkTimeout
	if timeout <= 0 {
		timeout = DefaultParkTimeout
	}
	s.mu.Lock()
	s.proposals = append([]Proposal(nil), proposals...)
	s.timer = time.AfterFunc(timeout, func() { _ = s.Close() })
	s.mu.Unlock()
	s.retireRound()
	s.p.setParked(s)
}

// Resume answers the parked callbacks with the real tool results and continues
// the same run. It returns ErrSessionGone, with the session disposed, when the
// run cannot continue. Nothing is fabricated: every result must match a pending
// call by identifier, name and arguments.
func (s *Session) Resume(ctx context.Context, results []ToolResult, emit func(Event) error, opts TurnOptions) (out Outcome, err error) {
	p := s.p
	p.turn.Lock()
	defer p.turn.Unlock()
	if p.takeParked(s) == nil {
		return out, ErrSessionGone
	}
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	parked := false
	defer func() {
		if !parked {
			out.CleanupError = errors.Join(out.CleanupError, s.dispose())
		}
	}()
	if answerErr := s.server.Answer(results); answerErr != nil {
		return out, fmt.Errorf("%w: %v", ErrSessionGone, answerErr)
	}
	s.mu.Lock()
	s.round = &round{emit: emit}
	backlog := s.backlog
	s.backlog = nil
	s.mu.Unlock()
	for _, event := range backlog {
		if emitErr := s.deliver(event); emitErr != nil {
			return out, emitErr
		}
	}
	var waitErr error
	out, waitErr = s.wait(ctx, opts.Park)
	if waitErr == nil && opts.Park && len(out.Proposals) > 0 {
		s.park(out.Proposals, opts)
		out.Session = s
		parked = true
	}
	return out, waitErr
}

// Close ends a parked run: it releases the callbacks, cancels the run, closes the
// agent and clears the bridge callback. It is safe to call more than once.
func (s *Session) Close() error {
	s.p.turn.Lock()
	defer s.p.turn.Unlock()
	s.p.takeParked(s)
	return s.dispose()
}

// armGrace starts one grace timer per round, so sibling parallel calls that
// arrive together are returned together.
func (s *Session) armGrace() {
	s.mu.Lock()
	if s.armed {
		s.mu.Unlock()
		return
	}
	s.armed = true
	s.mu.Unlock()
	time.AfterFunc(grace, func() {
		select {
		case s.proposed <- struct{}{}:
		default:
		}
	})
}

// handle processes one stream frame. It runs on the Send goroutine.
func (s *Session) handle(data json.RawMessage) error {
	traceEvent(data)
	if usage := usageFrame(data); usage != nil {
		s.recordUsage(*usage)
	}
	events, violation := classify(data, s.allow)
	if violation != nil {
		s.server.Fail(violation)
		s.cancelRun()
		return violation
	}
	for _, event := range events {
		if err := s.deliver(event); err != nil {
			return err
		}
	}
	return nil
}

// deliver passes one event to the current round, or keeps it for the next one.
func (s *Session) deliver(event Event) error {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	s.mu.Lock()
	r := s.round
	if r == nil {
		if len(s.backlog) < maxBacklogEvents {
			s.backlog = append(s.backlog, event)
		}
		s.mu.Unlock()
		return nil
	}
	if !s.deltas.keep(event) {
		s.mu.Unlock()
		return nil
	}
	if len(event.Text) > maxTurnOutputBytes-r.outputBytes {
		s.mu.Unlock()
		return errors.New("cursor output exceeded the per-turn safety limit; no retry was attempted")
	}
	r.outputBytes += len(event.Text)
	if !event.Thinking && event.Text != "" {
		r.streamed = true
	}
	emit := r.emit
	s.mu.Unlock()
	return emit(event)
}

// maxBacklogEvents bounds output kept while a run is parked.
const maxBacklogEvents = 4096

// retireRound stops emission to the finished round's callback. It waits for an
// emit that is already running.
func (s *Session) retireRound() {
	s.emitMu.Lock()
	s.mu.Lock()
	s.round = nil
	s.armed = false
	s.mu.Unlock()
	s.emitMu.Unlock()
}

// wait blocks until the round ends: tool calls to hand off, a final answer, a
// boundary violation, a failure, or cancellation of ctx.
func (s *Session) wait(ctx context.Context, park bool) (out Outcome, err error) {
	select {
	case <-ctx.Done():
		return out, ctx.Err()
	case <-s.server.failed:
		_, violation := s.server.Results()
		return out, violation
	case <-s.proposed:
		s.mu.Lock()
		s.armed = false
		s.mu.Unlock()
		traceCallbacks(s.server)
		proposals, violation := s.server.Results()
		if violation != nil {
			return out, violation
		}
		if !park {
			// The run is only needed up to its tool calls. Stop it before teardown.
			s.cancelRun()
			select {
			case <-s.sendEnded:
			case <-time.After(2 * time.Second):
			}
		}
		out.Proposals = proposals
		out.Usage, out.ModelCalls = s.roundUsage(nil)
		s.retireRound()
		return out, nil
	case <-s.sendEnded:
	}
	traceCallbacks(s.server)
	s.mu.Lock()
	result, sendErr, r := s.result, s.sendErr, s.round
	s.mu.Unlock()
	proposals, violation := s.server.Results()
	switch {
	case violation != nil:
		return out, violation
	case ctx.Err() != nil:
		return out, ctx.Err()
	case sendErr != nil:
		return out, fmt.Errorf("send Cursor turn: %w", sendErr)
	case len(proposals) > 0:
		out.Proposals = proposals
		out.Usage, out.ModelCalls = s.roundUsage(nil)
		s.retireRound()
		return out, nil
	}
	if r == nil {
		return out, errors.New("cursor turn ended without an active round")
	}
	if len(result.Result.Text) > maxTurnOutputBytes || (!r.streamed && len(result.Result.Text) > maxTurnOutputBytes-r.outputBytes) {
		return out, errors.New("cursor final output exceeded the per-turn safety limit; no retry was attempted")
	}
	out.Text = result.Result.Text
	if result.Result.Usage != nil {
		traceUsage("run-total", *result.Result.Usage)
	}
	out.Usage, out.ModelCalls = s.roundUsage(result.Result.Usage)
	if !r.streamed && out.Text != "" {
		if err = s.deliverFinal(r, out.Text); err != nil {
			return out, err
		}
	}
	return out, nil
}

// deliverFinal emits a final answer that did not stream.
func (s *Session) deliverFinal(r *round, text string) error {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	return r.emit(Event{Text: text})
}

// dispose releases everything a session owns. The caller holds the process turn
// slot. It is idempotent and reports each failed step.
func (s *Session) dispose() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.gone)
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	s.cancelRun()
	// Teardown must not depend on a cancelled request context.
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var failures []error
	// Release parked HTTP callbacks before agent teardown can wait on them.
	if closeErr := s.server.Close(); closeErr != nil {
		failures = append(failures, fmt.Errorf("stop Cursor tool callback server: %w", closeErr))
	}
	if s.agent != "" {
		if cancelErr := s.cancelActiveRun(cleanup); cancelErr != nil {
			failures = append(failures, cancelErr)
		}
		if closeErr := s.p.Client.CloseAgent(cleanup, s.agent); closeErr != nil {
			failures = append(failures, fmt.Errorf("close Cursor agent: %w", closeErr))
		}
	}
	if clearErr := s.p.Client.SetToolCallback(cleanup, "", ""); clearErr != nil {
		failures = append(failures, fmt.Errorf("clear Cursor tool callback: %w", clearErr))
	}
	return errors.Join(failures...)
}

// cancelActiveRun cancels the run if it did not finish. The stream carries the
// run identifier only in its final frame, so a run that was abandoned mid-stream
// is found through the agent's run list.
func (s *Session) cancelActiveRun(ctx context.Context) error {
	select {
	case <-s.sendEnded:
	case <-time.After(2 * time.Second):
	}
	s.mu.Lock()
	runID, status := s.result.RunID, s.result.Status
	s.mu.Unlock()
	if status == "RUN_LIFECYCLE_STATUS_FINISHED" {
		return nil
	}
	if runID != "" {
		if err := s.p.Client.Cancel(ctx, s.agent, runID); err != nil {
			return fmt.Errorf("cancel Cursor run: %w", err)
		}
		return nil
	}
	active, err := s.p.Client.ActiveRuns(ctx, s.agent)
	if err != nil {
		return fmt.Errorf("list Cursor runs: %w", err)
	}
	var failures []error
	for _, id := range active {
		if err := s.p.Client.Cancel(ctx, s.agent, id); err != nil {
			failures = append(failures, fmt.Errorf("cancel Cursor run: %w", err))
		}
	}
	return errors.Join(failures...)
}

// setParked records the session so the next turn can replace it.
func (p *Process) setParked(s *Session) {
	p.parkedMu.Lock()
	p.parked = s
	p.parkedMu.Unlock()
}

// takeParked removes and returns the parked session. With want set, it returns it
// only when it is that session.
func (p *Process) takeParked(want *Session) *Session {
	p.parkedMu.Lock()
	defer p.parkedMu.Unlock()
	if p.parked == nil || (want != nil && p.parked != want) {
		return nil
	}
	taken := p.parked
	p.parked = nil
	return taken
}
