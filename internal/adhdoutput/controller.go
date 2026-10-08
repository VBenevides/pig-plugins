package adhdoutput

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// Host is the production adapter's session API, not a second context projection.
type Host interface {
	Snapshot() (Snapshot, error)
	Identity() (sessionID, leafID string, err error)
	DefaultFlag() (bool, error)
	AppendEntry(string, State) error
	SendMessage(customType, content string) error
	SetStatus(key, text string)
	Notify(message, level string)
}

type operation struct {
	host       Host
	command    string
	restore    bool
	generation uint64
	done       chan error
}

type pendingMessage struct{ session, anchor, customType, content string }

// Controller serializes callbacks without holding its mutex over host IPC.
// The elected caller drains a bounded queue. It creates no worker goroutine.
type Controller struct {
	mu           sync.Mutex
	busy         bool
	generation   uint64
	queue        [64]*operation
	head, count  int
	loadConfig   func() (Config, error)
	config       Config
	rulesMessage string
	configured   bool
	pending      *pendingMessage
	waiting      atomic.Bool
}

func New(loadConfig func() (Config, error)) *Controller { return &Controller{loadConfig: loadConfig} }

// Restore invalidates older work after start, resume, reload, or branch selection.
func (c *Controller) Restore(host Host) error  { return c.submit(host, "", true) }
func (c *Controller) Sync(host Host) error     { return c.submit(host, "", false) }
func (c *Controller) NeedsSync() bool          { return c.waiting.Load() }
func (c *Controller) Shutdown(host Host) error { return c.submit(host, "shutdown", false) }
func (c *Controller) Command(host Host, args string) error {
	args = strings.ToLower(strings.TrimSpace(args))
	switch args {
	case "", "on", "off", "status":
		return c.submit(host, "command:"+args, false)
	default:
		host.Notify("Usage: /adhd [on|off|status]", "warning")
		return nil
	}
}

func (c *Controller) submit(host Host, command string, restore bool) error {
	c.mu.Lock()
	if c.count == len(c.queue) {
		c.mu.Unlock()
		err := errors.New("ADHD callback queue is full. Try the command again")
		host.Notify(err.Error(), "error")
		return err
	}
	if restore || command == "shutdown" {
		c.generation++
	}
	op := &operation{host: host, command: command, restore: restore, generation: c.generation, done: make(chan error, 1)}
	c.queue[(c.head+c.count)%len(c.queue)] = op
	c.count++
	if c.busy {
		c.mu.Unlock()
		return <-op.done
	}
	c.busy = true
	c.mu.Unlock()
	for {
		c.mu.Lock()
		if c.count == 0 {
			c.busy = false
			c.mu.Unlock()
			break
		}
		next := c.queue[c.head]
		c.queue[c.head] = nil
		c.head = (c.head + 1) % len(c.queue)
		c.count--
		c.mu.Unlock()
		err := c.execute(next)
		if err != nil {
			if c.current(next.generation) {
				next.host.SetStatus(StatusKey, "")
			}
			next.host.Notify("ADHD output: "+err.Error(), "error")
		}
		next.done <- err
	}
	return <-op.done
}

func (c *Controller) current(generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation == generation
}

func (c *Controller) check(op *operation, snapshot Snapshot) error {
	if !c.current(op.generation) {
		return errors.New("session or branch changed. ADHD state will be restored")
	}
	id, leaf, err := op.host.Identity()
	if err != nil {
		return err
	}
	if !c.current(op.generation) {
		return errors.New("session or branch changed during ADHD synchronization")
	}
	if id != snapshot.SessionID || leaf != snapshot.LeafID {
		return errors.New("session or active branch changed during ADHD synchronization")
	}
	return nil
}

func (c *Controller) execute(op *operation) error {
	defer func() { c.waiting.Store(c.pending != nil) }()
	if !c.current(op.generation) {
		return errors.New("discarded ADHD work from an earlier session or branch")
	}
	if op.command == "shutdown" {
		c.pending = nil
		op.host.SetStatus(StatusKey, "")
		return nil
	}
	if op.restore {
		c.pending = nil
	}
	if !c.configured || op.restore {
		config, err := c.loadConfig()
		if err != nil {
			return err
		}
		c.config, c.configured = config, true
		c.rulesMessage = RulesMessage(config.Rules)
	}
	snapshot, err := op.host.Snapshot()
	if err != nil {
		return fmt.Errorf("read active session: %w", err)
	}
	enabled, explicit, err := snapshot.Choice()
	if err != nil {
		return err
	}
	if !explicit {
		flag, err := op.host.DefaultFlag()
		if err != nil {
			return fmt.Errorf("read --adhd flag: %w", err)
		}
		enabled = flag || c.config.DefaultEnabled
	}
	rules := c.rulesMessage
	active := contextMarker(snapshot.Messages, rules)
	if c.pending != nil {
		p := c.pending
		if p.session != snapshot.SessionID || !snapshot.Contains(p.anchor) {
			c.pending = nil
		} else if latestMessage(snapshot.Messages, p.customType, p.content) {
			c.pending = nil
		} else if snapshot.Idle {
			c.pending = nil
			return errors.New("host accepted a style message, but it is absent from model context. Use /adhd on or /adhd off to retry")
		} else {
			active = marker{active: p.customType == RulesType, current: p.customType == RulesType && p.content == rules}
		}
	}
	isCommand := strings.HasPrefix(op.command, "command:")
	command := strings.TrimPrefix(op.command, "command:")
	if isCommand && command == "status" {
		if err := c.check(op, snapshot); err != nil {
			return err
		}
		c.status(op.host, enabled && active.current && c.pending == nil)
		context := "absent"
		if contextMarker(snapshot.Messages, rules).current {
			context = "present"
		}
		if c.pending != nil {
			context += "; a style change is queued until the active turn ends"
		}
		mode := "OFF"
		if enabled {
			mode = "ON"
		}
		op.host.Notify("ADHD output "+mode+". Current rules in model context: "+context+".", "info")
		return nil
	}
	if isCommand {
		target := !enabled
		if command == "on" {
			target = true
		}
		if command == "off" {
			target = false
		}
		if !explicit || target != enabled {
			if err := c.check(op, snapshot); err != nil {
				return err
			}
			if err := op.host.AppendEntry(StateType, State{Version: 1, Enabled: target}); err != nil {
				return fmt.Errorf("persist ADHD choice: %w", err)
			}
			previous := snapshot
			snapshot, err = op.host.Snapshot()
			if err != nil {
				return err
			}
			if snapshot.SessionID != previous.SessionID || !snapshot.Contains(previous.LeafID) {
				return errors.New("session changed while saving ADHD choice")
			}
			actual, saved, err := snapshot.Choice()
			if err != nil {
				return err
			}
			if !saved || actual != target {
				return errors.New("ADHD choice was not persisted on the active branch")
			}
			if c.pending == nil {
				active = contextMarker(snapshot.Messages, rules)
			}
		}
		enabled = target
	}
	if err := c.check(op, snapshot); err != nil {
		return err
	}
	if !enabled {
		c.status(op.host, false)
	}
	customType, content := "", ""
	if enabled && !active.current {
		customType, content = RulesType, rules
	}
	if !enabled && active.active {
		customType, content = DisabledType, Cancellation
	}
	if customType != "" {
		if err := op.host.SendMessage(customType, content); err != nil {
			return fmt.Errorf("inject style message: %w", err)
		}
		previous := snapshot
		snapshot, err = op.host.Snapshot()
		if err != nil {
			return err
		}
		if snapshot.SessionID != previous.SessionID || !snapshot.Contains(previous.LeafID) || !c.current(op.generation) {
			return errors.New("session or branch changed while injecting ADHD style")
		}
		if !latestMessage(snapshot.Messages, customType, content) {
			if snapshot.Idle {
				return errors.New("style message is not in model context. The ADHD badge remains hidden")
			}
			c.pending = &pendingMessage{session: snapshot.SessionID, anchor: snapshot.LeafID, customType: customType, content: content}
		} else {
			c.pending = nil
		}
	}
	if err := c.check(op, snapshot); err != nil {
		return err
	}
	c.status(op.host, enabled && contextMarker(snapshot.Messages, rules).current && c.pending == nil)
	if isCommand {
		message := "ADHD output OFF."
		if enabled {
			message = "ADHD output ON."
		}
		if c.pending != nil {
			message += " Style change queued until the active turn ends."
		}
		op.host.Notify(message, "info")
	}
	return nil
}

func (c *Controller) status(host Host, enabled bool) {
	text := ""
	if enabled && c.config.ShowStatus {
		text = Badge
	}
	host.SetStatus(StatusKey, text)
}

func latestMessage(messages []map[string]any, customType, content string) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message["role"] != "custom" {
			continue
		}
		if message["customType"] == RulesType || message["customType"] == DisabledType {
			return message["customType"] == customType && messageText(message["content"]) == content
		}
	}
	return false
}
