package cursor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"

	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

// parkedRun is a Cursor run that waits on PiG tool results.
type parkedRun interface {
	Resume(ctx context.Context, results []cursorbridge.ToolResult, emit func(cursorbridge.Event) error, opts cursorbridge.TurnOptions) (cursorbridge.Outcome, error)
	Close() error
	Done() <-chan struct{}
	Proposals() []cursorbridge.Proposal
}

// parkedEntry ties a parked run to the conversation that produced it. It holds
// the bridge process (release) so idle eviction cannot end the run early.
type parkedEntry struct {
	run         parkedRun
	fingerprint string
	release     func()
}

// parkRegistry keeps at most one parked run per account. The bridge process
// enforces the same limit, so a newer run always replaces an older one.
type parkRegistry struct {
	mu      sync.Mutex
	entries map[string]*parkedEntry
}

// put stores a parked run and releases the process pin when the run ends.
func (r *parkRegistry) put(account string, e *parkedEntry) {
	r.mu.Lock()
	if r.entries == nil {
		r.entries = map[string]*parkedEntry{}
	}
	r.entries[account] = e
	r.mu.Unlock()
	go func() {
		<-e.run.Done()
		r.mu.Lock()
		if r.entries[account] == e {
			delete(r.entries, account)
		}
		r.mu.Unlock()
		e.release()
	}()
}

// take removes and returns the parked run of an account.
func (r *parkRegistry) take(account string) *parkedEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.entries[account]
	delete(r.entries, account)
	return e
}

// peek returns the parked run of an account without removing it.
func (r *parkRegistry) peek(account string) *parkedEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries[account]
}

// fingerprint identifies a conversation state: account, model, effort, tools,
// system prompt and the first count messages. The same state always gives the
// same value, because encoding/json sorts map keys.
func fingerprint(account, modelID, level string, tools []cursorbridge.ToolDef, request map[string]any, count int) (string, bool) {
	messages, _ := request["messages"].([]any)
	if count < 1 || count > len(messages) {
		return "", false
	}
	raw, err := json.Marshal([]any{account, modelID, level, tools, request["systemPrompt"], messages[:count]})
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), true
}

// continuation checks that request is the parked conversation followed by the
// assistant message that carried the parked calls and exactly one real result per
// call. It returns those results, or false when anything differs. False means the
// caller replays the whole transcript; it never means an error.
func continuation(request map[string]any, proposals []cursorbridge.Proposal) ([]cursorbridge.ToolResult, int, bool) {
	messages, _ := request["messages"].([]any)
	last := -1
	for i, item := range messages {
		if m, ok := item.(map[string]any); ok && m["role"] == "assistant" {
			last = i
		}
	}
	if last < 1 || len(proposals) == 0 || len(messages)-last-1 != len(proposals) {
		return nil, 0, false
	}
	assistantMessage, _ := messages[last].(map[string]any)
	calls, _ := assistantMessage["content"].([]any)
	byID := map[string]cursorbridge.Proposal{}
	for _, p := range proposals {
		byID[p.ID] = p
	}
	seen := map[string]bool{}
	for _, block := range calls {
		call, _ := block.(map[string]any)
		if call["type"] != "toolCall" {
			continue
		}
		id, _ := call["id"].(string)
		name, _ := call["name"].(string)
		want, ok := byID[id]
		if !ok || seen[id] || name != want.Name || !sameArguments(call["arguments"], want.Args) {
			return nil, 0, false
		}
		seen[id] = true
	}
	if len(seen) != len(proposals) {
		return nil, 0, false
	}
	var results []cursorbridge.ToolResult
	answered := map[string]bool{}
	for _, item := range messages[last+1:] {
		m, _ := item.(map[string]any)
		id, _ := m["toolCallId"].(string)
		want, ok := byID[id]
		if m["role"] != "toolResult" || !ok || answered[id] || m["toolName"] != want.Name {
			return nil, 0, false
		}
		text, err := contentText(m["content"], nil)
		if err != nil {
			return nil, 0, false
		}
		failed, _ := m["isError"].(bool)
		results = append(results, cursorbridge.ToolResult{Call: want, Text: text, IsError: failed})
		answered[id] = true
	}
	return results, last, true
}

func sameArguments(value any, recorded json.RawMessage) bool {
	var want any
	if json.Unmarshal(recorded, &want) != nil {
		return false
	}
	return reflect.DeepEqual(value, want)
}

// parkingEnabled is on by default. PIG_CURSOR_PARK=0 turns it off, which restores
// the earlier behavior: every turn replays the transcript on a fresh agent.
func parkingEnabled() bool { return os.Getenv("PIG_CURSOR_PARK") != "0" }

func messageCount(request map[string]any) int {
	messages, _ := request["messages"].([]any)
	return len(messages)
}

// setFingerprint moves a parked entry to the state after a resumed round.
func (r *parkRegistry) setFingerprint(e *parkedEntry, fp string) {
	r.mu.Lock()
	e.fingerprint = fp
	r.mu.Unlock()
}

// tryResume continues the account's parked run when request is exactly that
// conversation plus the real results of the parked calls. It reports resumed
// false, with a nil error, whenever the caller should replay the transcript
// instead. A resumed run that then fails returns that error and is never
// replayed, because the generation already continued remotely.
func (s *service) tryResume(ctx context.Context, out *turnWriter, account, modelID, level string, request map[string]any, tools []cursorbridge.ToolDef) (cursorbridge.Outcome, bool, error) {
	entry := s.parked.peek(account)
	if entry == nil {
		return cursorbridge.Outcome{}, false, nil
	}
	results, last, ok := continuation(request, entry.run.Proposals())
	if !ok {
		return cursorbridge.Outcome{}, false, nil
	}
	s.parked.mu.Lock()
	want := entry.fingerprint
	s.parked.mu.Unlock()
	if got, ok := fingerprint(account, modelID, level, tools, request, last); !ok || got != want {
		return cursorbridge.Outcome{}, false, nil
	}
	outcome, err := entry.run.Resume(ctx, results, out.event, cursorbridge.TurnOptions{Park: len(tools) > 0 && parkingEnabled()})
	if errors.Is(err, cursorbridge.ErrSessionGone) {
		// Nothing reached Cursor, so replaying cannot repeat a generation.
		return cursorbridge.Outcome{CleanupError: outcome.CleanupError}, false, nil
	}
	if err == nil && outcome.Session != nil {
		fp, ok := fingerprint(account, modelID, level, tools, request, messageCount(request))
		if !ok {
			_ = entry.run.Close()
			return outcome, true, errors.New("cannot fingerprint the Cursor conversation")
		}
		s.parked.setFingerprint(entry, fp)
	}
	return outcome, true, err
}
