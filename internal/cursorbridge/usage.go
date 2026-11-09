package cursorbridge

import "encoding/json"

// Add returns the field-wise sum of two usages.
func (u TokenUsage) Add(o TokenUsage) TokenUsage {
	return TokenUsage{
		InputTokens:      u.InputTokens + o.InputTokens,
		OutputTokens:     u.OutputTokens + o.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens + o.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + o.CacheWriteTokens,
		TotalTokens:      u.TotalTokens + o.TotalTokens,
		ReasoningTokens:  u.ReasoningTokens + o.ReasoningTokens,
	}
}

// Sub returns u minus prev per field. A field never goes below zero, so a
// cumulative count that is lower than what was already reported adds nothing.
func (u TokenUsage) Sub(prev TokenUsage) TokenUsage {
	floor := func(a, b Int64) Int64 {
		if a < b {
			return 0
		}
		return a - b
	}
	return TokenUsage{
		InputTokens:      floor(u.InputTokens, prev.InputTokens),
		OutputTokens:     floor(u.OutputTokens, prev.OutputTokens),
		CacheReadTokens:  floor(u.CacheReadTokens, prev.CacheReadTokens),
		CacheWriteTokens: floor(u.CacheWriteTokens, prev.CacheWriteTokens),
		TotalTokens:      floor(u.TotalTokens, prev.TotalTokens),
		ReasoningTokens:  floor(u.ReasoningTokens, prev.ReasoningTokens),
	}
}

// IsZero reports whether every field is zero.
func (u TokenUsage) IsZero() bool { return u == TokenUsage{} }

// UncachedInput is the part of InputTokens that was neither read from nor written
// to the prompt cache. It assumes InputTokens includes both. The bridge's own
// TotalTokens (input plus output) and the live numbers in docs/cursor-bridge.md
// support that. The Cursor SDK docs say otherwise, so treat it as measured, not
// documented.
func (u TokenUsage) UncachedInput() Int64 {
	cached := u.CacheReadTokens + u.CacheWriteTokens
	if cached > u.InputTokens {
		return 0
	}
	return u.InputTokens - cached
}

// usageFrame returns the per-model-call usage of a stream frame. The SDK emits
// one usage message at the end of each model turn that reported usage.
func usageFrame(data json.RawMessage) *TokenUsage {
	var envelope struct {
		SDKMessage *struct {
			Type  string      `json:"type"`
			Usage *TokenUsage `json:"usage"`
		} `json:"sdkMessage"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.SDKMessage == nil || envelope.SDKMessage.Type != "usage" {
		return nil
	}
	return envelope.SDKMessage.Usage
}

// recordUsage keeps the usage of one model call for the current round.
func (s *Session) recordUsage(usage TokenUsage) {
	s.mu.Lock()
	s.calls = append(s.calls, usage)
	s.seen = s.seen.Add(usage)
	s.mu.Unlock()
	traceUsage("model-call", usage)
}

// roundUsage returns what this round adds to the run's usage, and the per-call
// usages seen in it. final is the run's cumulative usage when the run finished.
// Usage is cumulative over the run, so each round reports only the part that
// earlier rounds did not report. Summed over all rounds it equals the run total.
func (s *Session) roundUsage(final *TokenUsage) (*TokenUsage, []TokenUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := s.seen
	if final != nil && !final.Sub(total).IsZero() {
		total = *final
	}
	calls := s.calls
	s.calls = nil
	delta := total.Sub(s.reported)
	s.reported = total
	if delta.IsZero() {
		return nil, calls
	}
	return &delta, calls
}
