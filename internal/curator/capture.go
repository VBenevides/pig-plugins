package curator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Gap names an event that never became durable, so curator can journal the hole.
type Gap struct {
	SessionID string `json:"session_id"`
	EventID   string `json:"event_id"`
}

// IngestRequest is the stdin of `curator ingest`.
type IngestRequest struct {
	Events []EventIn `json:"events"`
	Gaps   []Gap     `json:"gaps"`
}

// Item outcomes reported by curator.
const (
	OutcomeDurable   = "durable"
	OutcomeDuplicate = "duplicate"
	OutcomeRejected  = "rejected"
	OutcomeFailed    = "failed"
)

// codeMemoryInspection marks events curator deliberately refuses to journal.
const codeMemoryInspection = "memory_inspection"

// ItemResult is curator's verdict on one event or gap.
type ItemResult struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Code    string `json:"code,omitempty"`
	Error   string `json:"error,omitempty"`
}

// IngestResponse is the stdout of `curator ingest`.
type IngestResponse struct {
	Results  []ItemResult `json:"results"`
	Gaps     []ItemResult `json:"gaps,omitempty"`
	Warnings []string     `json:"journal_warnings,omitempty"`
}

// Transport sends one request to curator and fails when no valid response was obtained. It must return when ctx
// ends.
type Transport func(ctx context.Context, request IngestRequest) (IngestResponse, error)

// CaptureOptions tune a queue; zero values take the defaults.
type CaptureOptions struct {
	// MaxQueue is the number of events held in memory before overflow becomes explicit gaps (default 1000).
	MaxQueue int
	// BatchSize is events per curator call, at most the curator limit of 200 (default 100).
	BatchSize int
	// MaxAttempts is the number of consecutive failures before an event is declared a gap (default 3).
	MaxAttempts int
	// MaxGaps caps the remembered gaps; beyond it only a counter grows (default 1000).
	MaxGaps int
	// OnProblem receives every handled failure; it may be nil.
	OnProblem func(message string)
}

// CaptureStats is a snapshot of a queue.
type CaptureStats struct {
	Queued        int
	Gaps          int
	GapsUncounted int
	Rejected      int
	Durable       int
}

type pending struct {
	event    EventIn
	attempts int
	bytes    int
}

// Capture is a bounded, single-flight capture queue. Acknowledgement means curator reported durable or duplicate;
// anything else stays visible as a retry or a gap, and nothing is claimed durable from the queue alone.
type Capture struct {
	send        Transport
	maxQueue    int
	batchSize   int
	maxAttempts int
	maxGaps     int
	problem     func(string)

	flight chan struct{} // holds a token while a drain runs

	mu            sync.Mutex
	queue         []*pending
	queuedBytes   int
	gaps          []Gap
	rejected      int
	durable       int
	gapsUncounted int
}

// NewCapture returns an empty queue that delivers through send.
func NewCapture(send Transport, options CaptureOptions) *Capture {
	c := &Capture{
		send:        send,
		maxQueue:    options.MaxQueue,
		batchSize:   min(options.BatchSize, 200),
		maxAttempts: options.MaxAttempts,
		maxGaps:     options.MaxGaps,
		problem:     options.OnProblem,
		flight:      make(chan struct{}, 1),
	}
	if c.maxQueue <= 0 {
		c.maxQueue = 1000
	}
	if c.batchSize <= 0 {
		c.batchSize = 100
	}
	if c.maxAttempts <= 0 {
		c.maxAttempts = 3
	}
	if c.maxGaps <= 0 {
		c.maxGaps = 1000
	}
	if c.problem == nil {
		c.problem = func(string) {}
	}
	return c
}

// Enqueue adds events; those that do not fit become gaps.
func (c *Capture) Enqueue(events []EventIn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, event := range events {
		if event.Content != nil && len(*event.Content) > 256<<10 {
			c.recordGap(event, "event content exceeds curator's 256 KiB limit")
			continue
		}
		encoded, err := json.Marshal(event)
		if err != nil || len(encoded) > 2<<20 {
			c.recordGap(event, "event exceeds capture byte limit")
			continue
		}
		if c.queuedBytes+len(encoded) > 8<<20 {
			c.recordGap(event, "queue byte limit")
			continue
		}
		if len(c.queue) >= c.maxQueue {
			c.recordGap(event, "queue full")
			continue
		}
		c.queue = append(c.queue, &pending{event: event, bytes: len(encoded)})
		c.queuedBytes += len(encoded)
	}
}

// AbandonQueue turns every still-queued event into a gap; nothing is silently dropped.
func (c *Capture) AbandonQueue() {
	c.mu.Lock()
	defer c.mu.Unlock()
	left := c.queue
	c.queue = nil
	c.queuedBytes = 0
	for _, p := range left {
		c.recordGap(p.event, "not delivered before the session ended")
	}
}

// Stats returns the counters.
func (c *Capture) Stats() CaptureStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return CaptureStats{Queued: len(c.queue), Gaps: len(c.gaps), GapsUncounted: c.gapsUncounted, Rejected: c.rejected, Durable: c.durable}
}

// PendingGaps returns the gaps curator has not acknowledged yet.
func (c *Capture) PendingGaps() []Gap {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.gaps)
}

// recordGap must be called with c.mu held.
func (c *Capture) recordGap(event EventIn, why string) {
	c.problem(fmt.Sprintf("capture gap %s/%s: %s", event.SessionID, event.ID, why))
	if len(c.gaps) >= c.maxGaps {
		c.gapsUncounted++
		return
	}
	c.gaps = append(c.gaps, Gap{SessionID: event.SessionID, EventID: event.ID})
}

// Flush delivers the queue and the pending gaps. It is single-flight: a concurrent caller waits for the drain in
// progress, then drains what is left. It stops when ctx ends, after a transport failure (the next flush retries,
// so a dead transport is never spun on) or when a round makes no progress. The error is ctx's when ctx ended first.
func (c *Capture) Flush(ctx context.Context) error {
	select {
	case c.flight <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.flight }()
	return c.drain(ctx)
}

func (c *Capture) drain(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		count, bytes := 0, 0
		for _, p := range c.queue {
			if count >= c.batchSize || bytes+p.bytes > 6<<20 {
				break
			}
			count++
			bytes += p.bytes
		}
		batch := slices.Clone(c.queue[:count])
		gaps := slices.Clone(c.gaps[:min(len(c.gaps), c.batchSize)])
		c.mu.Unlock()
		if len(batch) == 0 && len(gaps) == 0 {
			return nil
		}
		request := IngestRequest{Events: make([]EventIn, len(batch)), Gaps: gaps}
		for i, p := range batch {
			request.Events[i] = p.event
		}
		response, err := c.send(ctx, request)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr // cancelled by the caller, not a curator failure
			}
			c.problem("curator unavailable: " + err.Error())
			c.mu.Lock()
			for _, p := range batch {
				p.attempts++
			}
			c.expire()
			c.mu.Unlock()
			return nil
		}
		if !c.apply(batch, gaps, response) {
			return nil // no progress: wait for the next flush instead of looping
		}
	}
}

// expire must be called with c.mu held.
func (c *Capture) expire() {
	kept := c.queue[:0]
	for _, p := range c.queue {
		if p.attempts >= c.maxAttempts {
			c.recordGap(p.event, "curator unreachable")
			c.queuedBytes -= p.bytes
			continue
		}
		kept = append(kept, p)
	}
	clear(c.queue[len(kept):])
	c.queue = kept
}

// apply records curator's verdicts and reports whether anything was settled.
func (c *Capture) apply(batch []*pending, gaps []Gap, response IngestResponse) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, warning := range response.Warnings {
		c.problem("curator journal warning: " + warning)
	}
	done := make(map[*pending]bool, len(batch))
	for i, p := range batch {
		if i >= len(response.Results) || response.Results[i].ID != p.event.ID {
			p.attempts++
			c.problem(fmt.Sprintf("curator response does not match event %s", p.event.ID))
			continue
		}
		result := response.Results[i]
		switch result.Outcome {
		case OutcomeDurable, OutcomeDuplicate:
			c.durable++
			done[p] = true
		case OutcomeRejected:
			c.rejected++
			// Curator refuses to learn from its own tool results by design; that is not a capture problem.
			if result.Code != codeMemoryInspection {
				c.problem(strings.TrimSpace(fmt.Sprintf("event %s rejected: %s %s", p.event.ID, result.Code, result.Error)))
			}
			done[p] = true // permanent: retrying cannot change the verdict
		default:
			p.attempts++
			c.problem(strings.TrimSpace(fmt.Sprintf("event %s failed: %s %s", p.event.ID, result.Code, result.Error)))
		}
	}
	c.queue = slices.DeleteFunc(c.queue, func(p *pending) bool {
		if done[p] {
			c.queuedBytes -= p.bytes
			return true
		}
		return false
	})
	c.expire()

	gapsDone := 0
	settled := make([]bool, len(gaps))
	for i := range gaps {
		if i >= len(response.Gaps) {
			continue
		}
		switch response.Gaps[i].Outcome {
		case OutcomeDurable, OutcomeDuplicate, OutcomeRejected:
			settled[i] = true
			gapsDone++
		}
	}
	// The sent gaps are the first len(gaps) entries; only Enqueue and AbandonQueue append, after them.
	kept := make([]Gap, 0, len(c.gaps))
	for i, g := range c.gaps {
		if i < len(settled) && settled[i] {
			continue
		}
		kept = append(kept, g)
	}
	c.gaps = kept
	return len(done) > 0 || gapsDone > 0
}
