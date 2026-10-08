package adhdoutput

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// memoryHost is a session service test double. The controller under test performs
// all state selection and marker recognition. Context visibility is supplied explicitly.
type memoryHost struct {
	mu                          sync.Mutex
	snapshot                    Snapshot
	flag                        bool
	sends                       []map[string]any
	queued                      []map[string]any
	status                      map[string]string
	notices                     []string
	readErr, appendErr, sendErr error
	dropSend                    bool
	beforeIdentity              func()
}

func newHost() *memoryHost {
	return &memoryHost{snapshot: Snapshot{SessionID: "session-1", Idle: true}, status: map[string]string{"other-extension": "unchanged"}}
}
func (h *memoryHost) Snapshot() (Snapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshot, h.readErr
}
func (h *memoryHost) Identity() (string, string, error) {
	if h.beforeIdentity != nil {
		h.beforeIdentity()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshot.SessionID, h.snapshot.LeafID, h.readErr
}
func (h *memoryHost) DefaultFlag() (bool, error) { return h.flag, nil }
func (h *memoryHost) append(entry map[string]any) {
	entry["id"] = fmt.Sprintf("entry-%d", len(h.snapshot.Branch)+1)
	if h.snapshot.LeafID != "" {
		entry["parentId"] = h.snapshot.LeafID
	}
	h.snapshot.Branch = append(h.snapshot.Branch, entry)
	h.snapshot.LeafID = entry["id"].(string)
}
func (h *memoryHost) AppendEntry(customType string, state State) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.appendErr != nil {
		return h.appendErr
	}
	data, _ := json.Marshal(state)
	var decoded map[string]any
	_ = json.Unmarshal(data, &decoded)
	h.append(map[string]any{"type": "custom", "customType": customType, "data": decoded})
	return nil
}
func (h *memoryHost) SendMessage(customType, content string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sendErr != nil {
		return h.sendErr
	}
	message := map[string]any{"role": "custom", "customType": customType, "content": content}
	h.sends = append(h.sends, message)
	if h.dropSend {
		return nil
	}
	if !h.snapshot.Idle {
		h.queued = append(h.queued, message)
		return nil
	}
	h.snapshot.Messages = append(h.snapshot.Messages, message)
	h.append(map[string]any{"type": "custom_message", "customType": customType, "content": content})
	return nil
}
func (h *memoryHost) SetStatus(key, text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if text == "" {
		delete(h.status, key)
	} else {
		h.status[key] = text
	}
}
func (h *memoryHost) Notify(message, level string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notices = append(h.notices, level+": "+message)
}
func controller() *Controller {
	return New(func() (Config, error) { return Config{ShowStatus: true, Rules: "REAL PRESENTATION RULES"}, nil })
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCommandsDefaultAndIdempotency(t *testing.T) {
	h, c := newHost(), controller()
	must(t, c.Restore(h))
	if len(h.sends) != 0 || h.status[StatusKey] != "" {
		t.Fatal("default was not off")
	}
	must(t, c.Command(h, "on"))
	must(t, c.Command(h, " ON "))
	must(t, c.Command(h, "status"))
	if len(h.sends) != 1 || h.status[StatusKey] != Badge {
		t.Fatalf("enable: sends=%d status=%v", len(h.sends), h.status)
	}
	must(t, c.Command(h, "off"))
	must(t, c.Command(h, "off"))
	if len(h.sends) != 2 || h.sends[1]["customType"] != DisabledType || h.status[StatusKey] != "" {
		t.Fatal("disable did not cancel once and clear badge")
	}
	if h.status["other-extension"] != "unchanged" {
		t.Fatal("unrelated status removed")
	}
	must(t, c.Command(h, ""))
	if len(h.sends) != 3 || h.sends[2]["customType"] != RulesType {
		t.Fatal("toggle after off did not enable exactly once")
	}
	before := len(h.sends)
	must(t, c.Command(h, "invalid"))
	must(t, c.Command(h, "status"))
	if len(h.sends) != before || !strings.Contains(strings.Join(h.notices, "\n"), "Usage:") {
		t.Fatal("status or invalid command changed model context")
	}
}

func TestResumeBranchesReloadAndNewSession(t *testing.T) {
	h, c := newHost(), controller()
	must(t, c.Command(h, "off"))
	off := h.snapshot
	must(t, c.Command(h, "on"))
	on := h.snapshot
	h.snapshot = off
	must(t, c.Restore(h))
	if h.status[StatusKey] != "" || len(h.sends) != 1 {
		t.Fatal("earlier off branch was not restored")
	}
	h.snapshot = on
	must(t, c.Restore(h))
	must(t, controller().Restore(h))
	if h.status[StatusKey] != Badge || len(h.sends) != 1 {
		t.Fatal("on branch/resume/reload duplicated rules")
	}
	// An unrelated later off branch must not win over the raw selected on branch.
	h.snapshot = Snapshot{SessionID: "new-session", Idle: true}
	must(t, c.Restore(h))
	if h.status[StatusKey] != "" || len(h.sends) != 1 {
		t.Fatal("previous session leaked into new session")
	}
	h.flag = true
	must(t, c.Restore(h))
	if h.status[StatusKey] != Badge || len(h.sends) != 2 {
		t.Fatal("launch flag did not set new default")
	}
	must(t, c.Command(h, "off"))
	must(t, controller().Restore(h))
	if h.status[StatusKey] != "" {
		t.Fatal("saved off did not beat launch flag")
	}
}

func TestCompactionUsesOnlyModelVisibleContext(t *testing.T) {
	h, c := newHost(), controller()
	must(t, c.Command(h, "on"))
	must(t, c.Sync(h))
	must(t, c.Sync(h))
	if len(h.sends) != 1 {
		t.Fatal("retained rules were duplicated")
	}
	// Raw branch still holds the original rules. The host's post-compact context does not.
	h.snapshot.Messages = []map[string]any{{"role": "compactionSummary", "content": RulesMessage("REAL PRESENTATION RULES")}}
	must(t, c.Sync(h))
	must(t, c.Sync(h))
	if len(h.sends) != 2 {
		t.Fatal("removed rules did not restore exactly once")
	}
	must(t, c.Command(h, "off"))
	h.snapshot.Messages = []map[string]any{h.sends[1]}
	must(t, c.Sync(h))
	if len(h.sends) != 4 || h.sends[3]["customType"] != DisabledType {
		t.Fatal("off compact did not cancel surviving old rules")
	}
	h.snapshot.Messages = nil
	must(t, c.Sync(h))
	if len(h.sends) != 4 {
		t.Fatal("off compact re-injected rules")
	}
}

func TestErrorsNeverClaimEnabled(t *testing.T) {
	for _, kind := range []string{"read", "append", "send", "lost", "malformed", "config"} {
		t.Run(kind, func(t *testing.T) {
			h, c := newHost(), controller()
			switch kind {
			case "read":
				h.readErr = errors.New("read failed")
			case "append":
				h.appendErr = errors.New("disk full")
			case "send":
				h.sendErr = errors.New("injection rejected")
			case "lost":
				h.dropSend = true
			case "malformed":
				h.append(map[string]any{"type": "custom", "customType": StateType, "data": map[string]any{"version": float64(1), "enabled": "off"}})
			case "config":
				c = New(func() (Config, error) { return Config{}, errors.New("bad configuration") })
			}
			before := len(h.snapshot.Branch)
			if err := c.Command(h, "on"); err == nil {
				t.Fatal("failure was hidden")
			}
			if h.status[StatusKey] != "" || len(h.notices) == 0 {
				t.Fatal("failure displayed a badge or no diagnostic")
			}
			if (kind == "read" || kind == "malformed" || kind == "config") && len(h.snapshot.Branch) != before {
				t.Fatal("destructive state write after failed read")
			}
		})
	}
}

func TestStreamingQueueRapidTogglesAndSettlement(t *testing.T) {
	h, c := newHost(), controller()
	h.snapshot.Idle = false
	for _, command := range []string{"on", "on", "off", "on", "on"} {
		must(t, c.Command(h, command))
	}
	if len(h.sends) != 3 || h.status[StatusKey] != "" || !c.NeedsSync() {
		t.Fatal("pending rules duplicated or displayed as effective")
	}
	h.snapshot.Messages = append(h.snapshot.Messages, h.queued...)
	h.queued = nil
	h.snapshot.Idle = true
	must(t, c.Sync(h))
	if len(h.sends) != 3 || h.status[StatusKey] != Badge || c.NeedsSync() {
		t.Fatal("settled queued style did not become effective")
	}
}

func TestConcurrentEnableCompactAndToggles(t *testing.T) {
	h, c := newHost(), controller()
	var wg sync.WaitGroup
	errors := make(chan error, 32)
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				errors <- c.Command(h, "on")
			} else {
				errors <- c.Sync(h)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		must(t, err)
	}
	if len(h.sends) != 1 {
		t.Fatalf("concurrent enable duplicated %d messages", len(h.sends))
	}
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			command := "on"
			if i%2 == 0 {
				command = "off"
			}
			if err := c.Command(h, command); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := 1; i < len(h.sends); i++ {
		if h.sends[i]["customType"] == h.sends[i-1]["customType"] {
			t.Fatal("concurrent toggle duplicated marker")
		}
	}
}

func TestConfiguredDefaultAndHiddenStatus(t *testing.T) {
	h := newHost()
	c := New(func() (Config, error) { return Config{DefaultEnabled: true, Rules: "configured"}, nil })
	must(t, c.Restore(h))
	if len(h.sends) != 1 || h.status[StatusKey] != "" {
		t.Fatal("hidden status disabled rules or ignored default")
	}
	must(t, c.Command(h, "off"))
	h.snapshot = Snapshot{SessionID: "second", Idle: true}
	must(t, c.Restore(h))
	if len(h.sends) != 3 {
		t.Fatal("new session did not use configured default")
	}
}

func BenchmarkLifecycle(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprintf("enabled=%t", enabled), func(b *testing.B) {
			h, c := newHost(), controller()
			if enabled {
				if err := c.Command(h, "on"); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := c.Sync(h); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestLateWorkCannotWriteIntoReplacedSession(t *testing.T) {
	h, c := newHost(), controller()
	var once sync.Once
	h.beforeIdentity = func() {
		once.Do(func() {
			h.mu.Lock()
			h.snapshot = Snapshot{SessionID: "replacement-session", Idle: true}
			h.mu.Unlock()
		})
	}
	if err := c.Command(h, "on"); err == nil {
		t.Fatal("late work did not reject session replacement")
	}
	if len(h.snapshot.Branch) != 0 || len(h.sends) != 0 || h.status[StatusKey] != "" {
		t.Fatal("old session work wrote into the replacement")
	}
	must(t, c.Restore(h))
	if len(h.sends) != 0 {
		t.Fatal("replacement session inherited old toggle")
	}
}
