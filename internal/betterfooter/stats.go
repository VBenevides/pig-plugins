package betterfooter

import (
	"bytes"
	"encoding/json"
)

// Totals are the session's summed token usage and cost.
type Totals struct {
	Input, Output, CacheRead, CacheWrite int
	Cost                                 float64
}

// usageBlock is the usage block of a session entry or message.
type usageBlock struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Cost       struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

type sessionEntry struct {
	Type    string `json:"type"`
	Message *struct {
		Role  string      `json:"role"`
		Usage *usageBlock `json:"usage"`
	} `json:"message"`
	Usage *usageBlock `json:"usage"`
}

// SessionStats sums the usage of session entries incrementally: entries are append-only until the session is
// replaced, so only the new tail is parsed. It is not safe for concurrent use.
type SessionStats struct {
	totals    Totals
	latestHit float64
	hasHit    bool
	count     int
	last      []byte
	maps      bool
}

// SummarizeEntries consumes SDK-decoded entries without a second JSON encoding.
// Stable entry ids identify an unchanged prefix. Without ids, rebuild safely.
func (s *SessionStats) SummarizeEntries(entries []map[string]any) (Totals, float64, bool) {
	resume := s.maps && s.count > 0 && len(entries) >= s.count && len(s.last) > 0
	if resume {
		id, _ := entries[s.count-1]["id"].(string)
		resume = id != "" && id == string(s.last)
	}
	if !resume {
		*s = SessionStats{}
	}
	s.maps = true
	for _, entry := range entries[s.count:] {
		kind, _ := entry["type"].(string)
		message, _ := entry["message"].(map[string]any)
		role, _ := message["role"].(string)
		var usage map[string]any
		switch {
		case kind == "message" && (role == "assistant" || role == "toolResult"):
			usage, _ = message["usage"].(map[string]any)
		case kind == "usage" || kind == "branch_summary" || kind == "compaction":
			usage, _ = entry["usage"].(map[string]any)
		}
		if usage == nil {
			continue
		}
		input, _ := finiteNumber(usage["input"])
		output, _ := finiteNumber(usage["output"])
		read, _ := finiteNumber(usage["cacheRead"])
		write, _ := finiteNumber(usage["cacheWrite"])
		if role == "assistant" && input+read+write > 0 {
			s.latestHit, s.hasHit = read/(input+read+write)*100, true
		}
		s.totals.Input += int(input)
		s.totals.Output += int(output)
		s.totals.CacheRead += int(read)
		s.totals.CacheWrite += int(write)
		cost, _ := usage["cost"].(map[string]any)
		total, _ := finiteNumber(cost["total"])
		s.totals.Cost += total
	}
	s.count = len(entries)
	if s.count > 0 {
		id, _ := entries[s.count-1]["id"].(string)
		s.last = append(s.last[:0], id...)
	}
	return s.totals, s.latestHit, s.hasHit
}

// Summarize returns the totals over entries and the cache hit rate (percent) of the latest assistant message
// that had any prompt tokens. Assistant and tool-result message usage is summed, as are the usage of "usage",
// "branch_summary" and "compaction" entries (side LLM calls that Pi's own footer counts too). An entry that
// does not parse is skipped; it carries no usage the footer could show.
func (s *SessionStats) Summarize(entries []json.RawMessage) (Totals, float64, bool) {
	resume := !s.maps && s.count > 0 && len(entries) >= s.count && bytes.Equal(entries[s.count-1], s.last)
	if !resume {
		*s = SessionStats{}
	}
	for _, raw := range entries[s.count:] {
		var entry sessionEntry
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		var usage *usageBlock
		switch {
		case entry.Type == "message" && entry.Message != nil && entry.Message.Role == "assistant":
			usage = entry.Message.Usage
			if usage != nil {
				if prompt := usage.Input + usage.CacheRead + usage.CacheWrite; prompt > 0 {
					s.latestHit, s.hasHit = usage.CacheRead/prompt*100, true
				}
			}
		case entry.Type == "message" && entry.Message != nil && entry.Message.Role == "toolResult":
			usage = entry.Message.Usage
		case entry.Type == "usage" || entry.Type == "branch_summary" || entry.Type == "compaction":
			usage = entry.Usage
		}
		if usage == nil {
			continue
		}
		s.totals.Input += int(usage.Input)
		s.totals.Output += int(usage.Output)
		s.totals.CacheRead += int(usage.CacheRead)
		s.totals.CacheWrite += int(usage.CacheWrite)
		s.totals.Cost += usage.Cost.Total
	}
	s.count = len(entries)
	if s.count > 0 {
		s.last = bytes.Clone(entries[s.count-1])
	}
	return s.totals, s.latestHit, s.hasHit
}
