// Package contextprune decides which old tool results and skill bodies a provider request can leave out, with no model
// call. Decisions depend only on the message list, so the same history gives the same decisions.
package contextprune

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Settings bound what the pruner touches.
type Settings struct {
	// KeepRecent is the number of newest messages that are never changed.
	KeepRecent int
	// KeepRecentGroups protects complete assistant tool-call groups, including parallel results.
	KeepRecentGroups int
	// SessionFile permits age-based masking with an exact saved-result recovery reference.
	// Without durable session storage, only the conservative supersession rules apply.
	SessionFile string
	// RecoveryHandles uses active-branch handles instead of repeated full file paths.
	RecoveryHandles map[string]string
	// MinSavingChars is the smallest total saving that is worth a prune. A prune changes earlier request text, so
	// it invalidates the provider prefix cache from the first changed message on.
	MinSavingChars int
	// BashMaxChars is the size above which a shell result outside the recent window keeps only its head and tail.
	BashMaxChars int
	// BashHeadChars and BashTailChars are the parts of a long shell result that stay.
	BashHeadChars, BashTailChars int
	// SkillReasons contains explicitly eligible invocation IDs and their prune reasons.
	// A missing ID is protected, even when an identical skill appears later.
	SkillReasons map[string]string
}

// DefaultSettings batch roughly 8k tokens of savings and protect the latest ten tool-call groups.
// Shell output retains the existing head and tail limits.
var DefaultSettings = Settings{KeepRecent: 8, KeepRecentGroups: 10, MinSavingChars: 32000, BashMaxChars: 8192, BashHeadChars: 4096, BashTailChars: 1024}

// Decision replaces the text of one tool result or skill message.
type Decision struct {
	// Text is the replacement.
	Text string
	// Saved is how many characters the replacement removes.
	Saved int
	// SkillEdits replace only validated skill envelopes, never the whole request.
	SkillEdits []SkillEdit
}

// Decisions maps a tool call ID, or a skill message key, to its replacement.
type Decisions map[string]Decision

// Saved sums the characters that all decisions remove.
func (d Decisions) Saved() int {
	total := 0
	for _, decision := range d {
		total += decision.Saved
	}
	return total
}

// SkillKey is the Decisions key of the skill message at an index.
func SkillKey(index int) string { return fmt.Sprintf("skill:%d", index) }

type toolCall struct {
	name string
	args map[string]any
}

type pathEvent struct {
	index int
	kind  string // read, edit or write.
	span  string // The read range, empty when it is not a read.
}

// Decide returns the replacements for a message list. It returns none when the total saving is below
// MinSavingChars, and it never changes the newest KeepRecent messages, error results, or results with non-text
// content.
func Decide(messages []any, settings Settings) Decisions {
	calls := collectCalls(messages)
	results := map[string]int{}        // Tool call ID to the index of its result message.
	byPath := map[string][]pathEvent{} // Path to the reads and edits that touch it, in order.
	lastTodo := -1
	messageCounts := map[string]int{}
	for _, raw := range messages {
		m, _ := raw.(map[string]any)
		if m["role"] == "user" {
			messageCounts[messageKey(m)]++
		}
	}
	for index, raw := range messages {
		message, _ := raw.(map[string]any)
		switch message["role"] {
		case "toolResult":
			id, _ := message["toolCallId"].(string)
			call, ok := calls[id]
			if !ok {
				continue
			}
			results[id] = index
			switch call.name {
			case "read", "edit", "write":
				if path := cleanPath(call.args); path != "" {
					byPath[path] = append(byPath[path], pathEvent{index: index, kind: call.name, span: readSpan(call)})
				}
			case "todo":
				lastTodo = index
			}
		}
	}

	protectedFrom := len(messages) - settings.KeepRecent
	toolProtectedFrom := protectedFrom
	if settings.KeepRecentGroups > 0 {
		toolProtectedFrom = 0
		groups := 0
		for index := len(messages) - 1; index >= 0; index-- {
			message, _ := messages[index].(map[string]any)
			if hasToolCalls(message) {
				groups++
				if groups == settings.KeepRecentGroups {
					toolProtectedFrom = min(index, protectedFrom)
					break
				}
			}
		}
	}
	decisions := Decisions{}
	for index := len(messages) - 1; index >= 0; index-- {
		raw := messages[index]
		message, _ := raw.(map[string]any)
		switch message["role"] {
		case "toolResult":
			if index >= toolProtectedFrom {
				continue
			}
			id, _ := message["toolCallId"].(string)
			call, ok := calls[id]
			if !ok {
				continue
			}
			text, ok := plainText(message)
			if !ok {
				continue
			}
			if replacement, ok := replacementFor(call, index, text, byPath, lastTodo, settings); ok {
				if handle := settings.RecoveryHandles[id]; handle != "" {
					replacement += fmt.Sprintf("\n[recovery: %s; use context_recover]", handle)
				} else if settings.SessionFile != "" {
					replacement += fmt.Sprintf("\n[recovery: toolResult with toolCallId %q in session JSONL %q; use bash to extract the saved result, not to rerun the original command]", id, settings.SessionFile)
				}
				if len(replacement) < len(text) {
					decisions[id] = Decision{Text: replacement, Saved: len(text) - len(replacement)}
				}
			}
		case "user":
			bodies := SkillBodies(message)
			if index < protectedFrom && messageCounts[messageKey(message)] == 1 {
				d := skillEditsDecision(bodies, func(b SkillBody) string { return settings.SkillReasons[b.ID] })
				if len(d.SkillEdits) > 0 {
					decisions[messageKey(message)] = d
				}
			}
		}
	}
	if decisions.Saved() < settings.MinSavingChars {
		return Decisions{}
	}
	return decisions
}

func replacementFor(call toolCall, index int, text string, byPath map[string][]pathEvent, lastTodo int, settings Settings) (string, bool) {
	switch call.name {
	case "read":
		path := cleanPath(call.args)
		if later := laterEvent(byPath[path], index, readSpan(call), true); later != "" {
			return fmt.Sprintf("[pruned: this read of %s is out of date; %s]", path, later), true
		}
		if settings.SessionFile != "" {
			return fmt.Sprintf("[pruned: old read of %s (range %s); re-read for current content or recover the saved result]", path, readSpan(call)), true
		}
	case "edit":
		path := cleanPath(call.args)
		if later := laterEvent(byPath[path], index, "", false); later != "" {
			return fmt.Sprintf("[pruned: this edit result for %s is out of date; %s]", path, later), true
		}
	case "todo":
		if lastTodo > index {
			return "[pruned: a later todo result replaces this one]", true
		}
	case "bash":
		if len(text) > settings.BashMaxChars {
			return truncateMiddle(text, settings.BashHeadChars, settings.BashTailChars), true
		}
	case "code_search", "code_overview", "ast_search":
		if settings.SessionFile != "" {
			return "[pruned: old search output; recover the saved result or repeat this read-only search]", true
		}
	}
	return "", false
}

// laterEvent describes the first later event that makes a result out of date, or returns "". A later edit or write
// changes the file, so any earlier read or edit result of it is stale. A later read of the same range repeats an
// earlier read. Another range does not, because it may show different lines.
func laterEvent(events []pathEvent, index int, span string, isRead bool) string {
	for _, event := range events {
		if event.index <= index {
			continue
		}
		switch {
		case event.kind != "read":
			return "the file was changed later"
		case isRead && event.span == span:
			return "the file was read again later"
		case !isRead:
			return "the file was read again later"
		}
	}
	return ""
}

func truncateMiddle(text string, head, tail int) string {
	headEnd := validCut(text, head)
	tailStart := len(text) - tail
	for tailStart < len(text) && !utf8.RuneStart(text[tailStart]) {
		tailStart++
	}
	return text[:headEnd] + "\n\n[... tool result middle pruned ...]\n\n" + text[tailStart:]
}

// validCut returns the largest index at most cut that does not split a rune.
func validCut(text string, cut int) int {
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return cut
}

// hasToolCalls identifies one tool-use iteration, regardless of parallel call count.
func hasToolCalls(message map[string]any) bool {
	if message["role"] != "assistant" {
		return false
	}
	blocks, _ := message["content"].([]any)
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		if block["type"] == "toolCall" {
			return true
		}
	}
	return false
}

func collectCalls(messages []any) map[string]toolCall {
	calls := map[string]toolCall{}
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] != "assistant" {
			continue
		}
		blocks, _ := message["content"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			if block["type"] != "toolCall" {
				continue
			}
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			args, _ := block["arguments"].(map[string]any)
			if id != "" {
				calls[id] = toolCall{name: name, args: args}
			}
		}
	}
	return calls
}

func cleanPath(args map[string]any) string {
	path, _ := args["path"].(string)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func readSpan(call toolCall) string {
	if call.name != "read" {
		return ""
	}
	offset, hasOffset := call.args["offset"]
	limit, hasLimit := call.args["limit"]
	if !hasOffset && !hasLimit {
		return ""
	}
	return fmt.Sprintf("%v:%v", offset, limit)
}

// plainText returns the text of a message whose content is only text, and false for anything else, such as an image
// or an error result, which the pruner leaves alone.
func plainText(message map[string]any) (string, bool) {
	if failed, _ := message["isError"].(bool); failed {
		return "", false
	}
	if text, ok := message["content"].(string); ok {
		return text, true
	}
	blocks, _ := message["content"].([]any)
	if len(blocks) == 0 {
		return "", false
	}
	var text strings.Builder
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		part, ok := block["text"].(string)
		if block["type"] != "text" || !ok {
			return "", false
		}
		text.WriteString(part)
	}
	return text.String(), true
}

// Apply returns a copy of the message list with the decisions applied. Messages that no decision names are shared
// with the input, and the input is not changed.
func Apply(messages []any, decisions Decisions) ([]any, int) {
	if len(decisions) == 0 {
		return messages, 0
	}
	calls := collectCalls(messages)
	out := make([]any, len(messages))
	changed := 0
	for index, raw := range messages {
		out[index] = raw
		message, _ := raw.(map[string]any)
		var decision Decision
		var found bool
		switch message["role"] {
		case "toolResult":
			if id, _ := message["toolCallId"].(string); id != "" {
				if _, known := calls[id]; known {
					decision, found = decisions[id]
				}
			}
		case "user":
			decision, found = decisions[messageKey(message)]
		}
		if !found {
			continue
		}
		if len(decision.SkillEdits) > 0 {
			if replaced, ok := applySkillEdits(message, decision); ok {
				out[index] = replaced
				changed++
			}
			continue
		}
		if _, ok := plainText(message); !ok {
			continue
		}
		replaced := make(map[string]any, len(message))
		for key, value := range message {
			replaced[key] = value
		}
		if _, isString := message["content"].(string); isString {
			replaced["content"] = decision.Text
		} else {
			replaced["content"] = []any{map[string]any{"type": "text", "text": decision.Text}}
		}
		out[index] = replaced
		changed++
	}
	return out, changed
}

// CharsPerToken is a rough ratio that turns characters into a token estimate. It is not a measurement.
const CharsPerToken = 4

// SavedTokens estimates the tokens that pruning would remove from a message list.
func SavedTokens(messages []any, settings Settings) int {
	return Decide(messages, settings).Saved() / CharsPerToken
}

// MessagesFromEntries returns the messages of session entries that come after the last compaction, in order. The
// messages that compaction kept from before its cut are not included, so a saving computed from the result can
// be lower than the true one, never higher.
func MessagesFromEntries(entries []json.RawMessage) []any {
	var messages []any
	for _, raw := range entries {
		var entry struct {
			Type    string `json:"type"`
			Message any    `json:"message"`
		}
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		switch entry.Type {
		case "compaction":
			messages = nil
		case "message":
			if entry.Message != nil {
				messages = append(messages, entry.Message)
			}
		}
	}
	return messages
}
