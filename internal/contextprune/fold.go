package contextprune

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNothingToFold means that the view holds none of the requested calls.
// A repeated request for an applied range returns it and changes nothing.
var ErrNothingToFold = errors.New("no requested tool call remains in the view")

// protectedFoldTools keep active workflow state and recovery output in the view.
var protectedFoldTools = map[string]bool{"todo": true, "goal": true, "context_recover": true}

// FoldRequest names one explicit closed range. The caller must create it from
// trusted checkpoint state, never from assistant prose or elapsed time.
type FoldRequest struct {
	Scope      ReductionScope
	Checkpoint string   // Fingerprint stored when the user or host opened the checkpoint.
	CallIDs    []string // Every call of each assistant message in the range.
	RangeHash  string   // Optional hash from an earlier receipt. A mismatch rejects the range.
}

// FoldReceipt records what an applied fold removed. Original history stays in the session.
type FoldReceipt struct {
	RangeHash string
	CallIDs   []string
	Handles   []string
	Removed   int
	Saved     int
}

// Fold replaces a closed range of calls and results with one short note.
// It never changes the input, calls a model, or runs a tool.
// protectedFrom is the first message index that must stay unchanged.
func Fold(messages []any, request FoldRequest, refs map[string]RecoveryRef, protectedFrom int) ([]any, FoldReceipt, error) {
	wanted := map[string]bool{}
	for _, id := range request.CallIDs {
		if id == "" || wanted[id] {
			return nil, FoldReceipt{}, fmt.Errorf("fold range has an empty or repeated call id")
		}
		wanted[id] = true
	}
	if len(wanted) == 0 {
		return nil, FoldReceipt{}, fmt.Errorf("fold range is empty")
	}
	rangeIndexes, err := foldIndexes(messages, wanted, protectedFrom)
	if err != nil {
		return nil, FoldReceipt{}, err
	}
	members := make([]any, len(rangeIndexes))
	for position, index := range rangeIndexes {
		members[position] = messages[index]
	}
	if err := FoldCompatibility(request.Scope, request.Checkpoint, members); err != nil {
		return nil, FoldReceipt{}, err
	}
	hash, size, err := foldRangeHash(members)
	if err != nil {
		return nil, FoldReceipt{}, err
	}
	if request.RangeHash != "" && request.RangeHash != hash {
		return nil, FoldReceipt{}, fmt.Errorf("fold range changed since its checkpoint")
	}
	note, handles, err := foldNote(members, refs)
	if err != nil {
		return nil, FoldReceipt{}, err
	}
	if len(note) >= size {
		return nil, FoldReceipt{}, fmt.Errorf("fold would not reduce the range")
	}
	view := foldView(messages, rangeIndexes, wanted, note)
	ids := append([]string(nil), request.CallIDs...)
	sort.Strings(ids)
	return view, FoldReceipt{RangeHash: hash, CallIDs: ids, Handles: handles, Removed: len(rangeIndexes), Saved: size - len(note)}, nil
}

// foldIndexes finds every member and rejects unbalanced, partial, or unresolved ranges.
func foldIndexes(messages []any, wanted map[string]bool, protectedFrom int) ([]int, error) {
	indexes := []int{}
	seenCalls, seenResults := map[string]int{}, map[string]int{}
	for index, value := range messages {
		message, _ := value.(map[string]any)
		switch message["role"] {
		case "assistant":
			blocks, _ := message["content"].([]any)
			in, out := 0, 0
			for _, raw := range blocks {
				block, _ := raw.(map[string]any)
				if block["type"] != "toolCall" {
					continue
				}
				id, _ := block["id"].(string)
				if wanted[id] {
					in++
					seenCalls[id]++
					if name, _ := block["name"].(string); protectedFoldTools[name] {
						return nil, fmt.Errorf("fold range contains an active workflow tool")
					}
				} else {
					out++
				}
			}
			if in == 0 {
				continue
			}
			if out > 0 {
				return nil, fmt.Errorf("fold range splits a parallel call group")
			}
			if index >= protectedFrom {
				return nil, fmt.Errorf("fold range reaches the protected recent window")
			}
			indexes = append(indexes, index)
		case "toolResult":
			id, _ := message["toolCallId"].(string)
			if !wanted[id] {
				continue
			}
			seenResults[id]++
			if index >= protectedFrom {
				return nil, fmt.Errorf("fold range reaches the protected recent window")
			}
			if message["isError"] == true {
				return nil, fmt.Errorf("unresolved failure details stay in the view")
			}
			indexes = append(indexes, index)
		}
	}
	if len(seenCalls) == 0 && len(seenResults) == 0 {
		return nil, ErrNothingToFold
	}
	for id := range wanted {
		if seenCalls[id] != 1 || seenResults[id] != 1 {
			return nil, fmt.Errorf("fold range is incomplete or ambiguous for call %q", id)
		}
	}
	return indexes, nil
}

func foldRangeHash(members []any) (string, int, error) {
	digest := sha256.New()
	size := 0
	for _, member := range members {
		data, err := json.Marshal(member)
		if err != nil {
			return "", 0, fmt.Errorf("encode fold range: %w", err)
		}
		size += len(data)
		digest.Write(data)
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}

// foldNote lists recorded actions only. Argument values stay out except for a file path.
func foldNote(members []any, refs map[string]RecoveryRef) (string, []string, error) {
	digests := map[string]string{}
	for handle, ref := range refs {
		digests[ref.Digest+"\x00"+ref.Role+"\x00"+ref.CallID] = handle
	}
	calls := collectCalls(members)
	var lines []string
	var handles []string
	seenBodies := map[string]string{}
	needsRead := false
	for _, value := range members {
		message, _ := value.(map[string]any)
		data, err := json.Marshal(message)
		if err != nil {
			return "", nil, fmt.Errorf("encode fold member: %w", err)
		}
		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		callID, _ := message["toolCallId"].(string)
		role, _ := message["role"].(string)
		handle := digests[digest+"\x00"+role+"\x00"+callID]
		if handle == "" {
			return "", nil, fmt.Errorf("fold member has no verified recovery handle")
		}
		handles = append(handles, handle)
		if role != "toolResult" {
			continue
		}
		text, plain := resultPlainBody(message)
		if !plain {
			return "", nil, fmt.Errorf("fold result is not plain text")
		}
		call := calls[callID]
		line := fmt.Sprintf("- %s ok", call.name)
		if path := cleanPath(call.args); path != "" {
			line += " " + path
		}
		line += fmt.Sprintf(", %d result bytes", len(text))
		bodyHash := resultBodyHash(text, false)
		if first, repeated := seenBodies[bodyHash]; repeated {
			line += ", same result as " + first
		} else {
			seenBodies[bodyHash] = handle
		}
		switch call.name {
		case "read", "edit", "write":
			needsRead = true
		}
		lines = append(lines, line+", recovery "+handle)
	}
	note := "[folded completed tool calls; status unchanged; recorded actions only]\n" + strings.Join(lines, "\n")
	note += "\nAssistant call handles: " + joinAssistant(members, handles)
	note += "\nUse context_recover for exact originals."
	if needsRead {
		note += " Read file contents again before editing."
	}
	return note, handles, nil
}

func joinAssistant(members []any, handles []string) string {
	out := []string{}
	for index, value := range members {
		if message, _ := value.(map[string]any); message["role"] == "assistant" {
			out = append(out, handles[index])
		}
	}
	return strings.Join(out, ", ")
}

// foldView keeps unrelated messages and any assistant text. One synthetic note replaces the range.
func foldView(messages []any, rangeIndexes []int, wanted map[string]bool, note string) []any {
	inRange := map[int]bool{}
	for _, index := range rangeIndexes {
		inRange[index] = true
	}
	view := make([]any, 0, len(messages))
	noted := false
	for index, value := range messages {
		if !inRange[index] {
			view = append(view, value)
			continue
		}
		message, _ := value.(map[string]any)
		if message["role"] != "assistant" {
			continue
		}
		if !noted {
			noted = true
			view = append(view, foldNoteMessage(message, note))
		}
		blocks, _ := message["content"].([]any)
		kept := []any{}
		for _, raw := range blocks {
			if block, _ := raw.(map[string]any); block["type"] != "toolCall" {
				kept = append(kept, raw)
			}
		}
		if len(kept) == 0 {
			continue
		}
		copied := make(map[string]any, len(message))
		for key, item := range message {
			copied[key] = item
		}
		copied["content"] = kept
		if copied["stopReason"] == "toolUse" {
			copied["stopReason"] = "stop"
		}
		view = append(view, copied)
	}
	return view
}

func foldNoteMessage(template map[string]any, note string) map[string]any {
	message := map[string]any{"role": "assistant", "stopReason": "stop", "content": []any{map[string]any{"type": "text", "text": note}}}
	for _, key := range []string{"provider", "model", "api", "timestamp"} {
		if value, ok := template[key]; ok {
			message[key] = value
		}
	}
	return message
}
