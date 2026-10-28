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
	// MinSavingChars is the smallest total saving that is worth a prune. A prune changes earlier request text, so
	// it invalidates the provider prefix cache from the first changed message on.
	MinSavingChars int
	// BashMaxChars is the size above which a shell result outside the recent window keeps only its head and tail.
	BashMaxChars int
	// BashHeadChars and BashTailChars are the parts of a long shell result that stay.
	BashHeadChars, BashTailChars int
}

// DefaultSettings follow the DeepSeek Harness tool-result pruner limits for shell output.
var DefaultSettings = Settings{KeepRecent: 8, MinSavingChars: 20000, BashMaxChars: 8192, BashHeadChars: 4096, BashTailChars: 1024}

// Decision replaces the text of one tool result or skill message.
type Decision struct {
	// Text is the replacement.
	Text string
	// Saved is how many characters the replacement removes.
	Saved int
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
	skills := map[string]int{} // Skill name to the index of its newest message.
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
		case "user":
			if name := skillName(message); name != "" {
				skills[name] = index
			}
		}
	}

	protectedFrom := len(messages) - settings.KeepRecent
	decisions := Decisions{}
	for index, raw := range messages {
		if index >= protectedFrom {
			break
		}
		message, _ := raw.(map[string]any)
		switch message["role"] {
		case "toolResult":
			id, _ := message["toolCallId"].(string)
			call, ok := calls[id]
			if !ok {
				continue
			}
			text, ok := plainText(message)
			if !ok {
				continue
			}
			if replacement, ok := replacementFor(call, index, text, byPath, lastTodo, settings); ok && len(replacement) < len(text) {
				decisions[id] = Decision{Text: replacement, Saved: len(text) - len(replacement)}
			}
		case "user":
			name := skillName(message)
			if name == "" || skills[name] == index {
				continue
			}
			text, ok := plainText(message)
			replacement := fmt.Sprintf("[pruned: skill %s was loaded again later in this conversation]", name)
			if ok && len(replacement) < len(text) {
				decisions[SkillKey(index)] = Decision{Text: replacement, Saved: len(text) - len(replacement)}
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

// skillName returns the skill name of a user message that carries a skill body, or "".
func skillName(message map[string]any) string {
	text, ok := plainText(message)
	if !ok {
		return ""
	}
	const prefix = `<skill name="`
	if !strings.HasPrefix(text, prefix) {
		return ""
	}
	rest := text[len(prefix):]
	end := strings.IndexByte(rest, '"')
	if end <= 0 {
		return ""
	}
	return rest[:end]
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
			if skillName(message) != "" {
				decision, found = decisions[SkillKey(index)]
			}
		}
		if !found {
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
