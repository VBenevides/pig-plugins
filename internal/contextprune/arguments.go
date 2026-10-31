package contextprune

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

const (
	DefaultArgumentBudgetBytes = 32768
	minimumArgumentBudgetBytes = 4096
	maximumArgumentBudgetBytes = 16 << 20
	// argumentStringMinBytes is the smallest string that a reduction replaces.
	// Paths, short commands, and flags stay readable.
	argumentStringMinBytes = 512
	argumentKeyPrefix      = "args:"
)

// argumentTools lists the tools whose old call payloads can be large and are saved on disk.
var argumentTools = map[string]bool{"edit": true, "write": true}

// ArgumentKey names the decision for one call's arguments. Result decisions use the bare call ID.
func ArgumentKey(callID string) string { return argumentKeyPrefix + callID }

// ArgumentHash binds a decision to the exact arguments that the saved event holds.
func ArgumentHash(arguments map[string]any) (string, bool) {
	data, err := json.Marshal(arguments)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true
}

// argumentHashes returns the hash of each edit or write call in a saved assistant message.
func argumentHashes(message map[string]any) map[string]string {
	hashes := map[string]string{}
	blocks, _ := message["content"].([]any)
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		name, _ := block["name"].(string)
		id, _ := block["id"].(string)
		arguments, _ := block["arguments"].(map[string]any)
		if block["type"] != "toolCall" || id == "" || !argumentTools[name] || arguments == nil {
			continue
		}
		if hash, ok := ArgumentHash(arguments); ok {
			hashes[id] = hash
		}
	}
	return hashes
}

// encodeArgumentHashes returns sorted JSON, which makes equal maps equal strings.
func encodeArgumentHashes(hashes map[string]string) string {
	if len(hashes) == 0 {
		return ""
	}
	data, err := json.Marshal(hashes)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeArgumentHashes(encoded string) map[string]string {
	hashes := map[string]string{}
	if encoded == "" || json.Unmarshal([]byte(encoded), &hashes) != nil {
		return map[string]string{}
	}
	return hashes
}

// ArgumentRecovery returns the unambiguous handle and saved-argument hash for each call ID.
func ArgumentRecovery(refs map[string]RecoveryRef) (map[string]string, map[string]string) {
	handles, sources, ambiguous := map[string]string{}, map[string]string{}, map[string]bool{}
	// Sorted handles keep the result deterministic when a call ID repeats.
	keys := make([]string, 0, len(refs))
	for handle := range refs {
		keys = append(keys, handle)
	}
	sort.Strings(keys)
	for _, handle := range keys {
		ref := refs[handle]
		if ref.Role != "assistant" {
			continue
		}
		for id, hash := range decodeArgumentHashes(ref.ArgHashes) {
			if ambiguous[id] {
				continue
			}
			if existing, ok := handles[id]; ok && existing != handle {
				delete(handles, id)
				delete(sources, id)
				ambiguous[id] = true
				continue
			}
			handles[id], sources[id] = handle, hash
		}
	}
	return handles, sources
}

type historicalArgument struct {
	id, name string
	args     map[string]any
	size     int
}

// decideHistoricalArguments bounds old edit and write payloads. The saved file or the session
// event holds the original, so a reduced payload stays recoverable. A failed or unfinished
// call keeps its payload, because the model can need it to retry.
func decideHistoricalArguments(messages []any, protectedFrom int, settings Settings, decisions Decisions) {
	if settings.HistoricalArgumentBytes <= 0 {
		return
	}
	finished := map[string]bool{}
	for index := 0; index < protectedFrom && index < len(messages); index++ {
		message, _ := messages[index].(map[string]any)
		if id, _ := message["toolCallId"].(string); message["role"] == "toolResult" && id != "" && message["isError"] != true {
			finished[id] = true
		}
	}
	retained := 0
	candidates := []historicalArgument{}
	for index := min(protectedFrom, len(messages)) - 1; index >= 0; index-- {
		message, _ := messages[index].(map[string]any)
		if message["role"] != "assistant" {
			continue
		}
		blocks, _ := message["content"].([]any)
		for position := len(blocks) - 1; position >= 0; position-- {
			block, _ := blocks[position].(map[string]any)
			name, _ := block["name"].(string)
			id, _ := block["id"].(string)
			arguments, _ := block["arguments"].(map[string]any)
			if block["type"] != "toolCall" || !argumentTools[name] || arguments == nil || !finished[id] {
				continue
			}
			data, err := json.Marshal(arguments)
			if err != nil {
				continue
			}
			hash, _ := ArgumentHash(arguments)
			if ReplaySensitive(message) || settings.ArgumentHandles[id] == "" || settings.ArgumentSources[id] != hash {
				retained += len(data)
				continue
			}
			candidates = append(candidates, historicalArgument{id: id, name: name, args: arguments, size: len(data)})
		}
	}
	total := retained
	for _, candidate := range candidates {
		total += candidate.size
	}
	if total <= settings.HistoricalArgumentBytes {
		return
	}
	target := settings.HistoricalArgumentBytes * postReductionPercent / 100
	// Candidates are newest first, so the newest payloads stay whole.
	for _, candidate := range candidates {
		if retained+candidate.size <= target {
			retained += candidate.size
			continue
		}
		handle := settings.ArgumentHandles[candidate.id]
		replacement, saved := reduceArgument(candidate.args, "", candidate.name, handle).(map[string]any)
		if !saved || replacement == nil {
			retained += candidate.size
			continue
		}
		data, err := json.Marshal(replacement)
		if err != nil || len(data) >= candidate.size {
			retained += candidate.size
			continue
		}
		hash, _ := ArgumentHash(candidate.args)
		decisions[ArgumentKey(candidate.id)] = Decision{
			Arguments: replacement, Handle: handle, Saved: candidate.size - len(data),
			SourceHash: hash, BudgetRequired: true,
		}
		retained += len(data)
	}
}

// reduceArgument replaces each large string and keeps the structure. It returns nil when nothing changed.
func reduceArgument(value any, key, tool, handle string) any {
	switch typed := value.(type) {
	case string:
		if len(typed) < argumentStringMinBytes {
			return nil
		}
		return fmt.Sprintf("[pruned: old %q argument %q, %d bytes; status unchanged; recovery: %s; use context_recover]", tool, key, len(typed), handle)
	case map[string]any:
		out, changed := make(map[string]any, len(typed)), false
		for name, item := range typed {
			if replaced := reduceArgument(item, name, tool, handle); replaced != nil {
				out[name], changed = replaced, true
			} else {
				out[name] = item
			}
		}
		if changed {
			return out
		}
	case []any:
		out, changed := make([]any, len(typed)), false
		for index, item := range typed {
			if replaced := reduceArgument(item, key, tool, handle); replaced != nil {
				out[index], changed = replaced, true
			} else {
				out[index] = item
			}
		}
		if changed {
			return out
		}
	}
	return nil
}

// applyArgumentDecisions copies an assistant message with reduced arguments.
// A block whose saved hash no longer matches keeps its original arguments.
func applyArgumentDecisions(message map[string]any, decisions Decisions) (map[string]any, bool) {
	blocks, _ := message["content"].([]any)
	out, changed := make([]any, len(blocks)), false
	for index, raw := range blocks {
		out[index] = raw
		block, _ := raw.(map[string]any)
		id, _ := block["id"].(string)
		decision, found := decisions[ArgumentKey(id)]
		if block["type"] != "toolCall" || !found || decision.Arguments == nil {
			continue
		}
		arguments, _ := block["arguments"].(map[string]any)
		if hash, ok := ArgumentHash(arguments); !ok || hash != decision.SourceHash {
			continue
		}
		copied := make(map[string]any, len(block))
		for name, value := range block {
			copied[name] = value
		}
		copied["arguments"] = decision.Arguments
		out[index], changed = copied, true
	}
	if !changed {
		return message, false
	}
	replaced := make(map[string]any, len(message))
	for name, value := range message {
		replaced[name] = value
	}
	replaced["content"] = out
	return replaced, true
}

// discardChangedArguments removes argument decisions whose calls changed or vanished.
func (decisions Decisions) discardChangedArguments(messages []any) {
	verified := map[string]bool{}
	for _, value := range messages {
		message, _ := value.(map[string]any)
		blocks, _ := message["content"].([]any)
		for _, raw := range blocks {
			block, _ := raw.(map[string]any)
			id, _ := block["id"].(string)
			decision, found := decisions[ArgumentKey(id)]
			if block["type"] != "toolCall" || !found {
				continue
			}
			arguments, _ := block["arguments"].(map[string]any)
			if hash, ok := ArgumentHash(arguments); ok && hash == decision.SourceHash {
				verified[id] = true
			}
		}
	}
	for key, decision := range decisions {
		if decision.Arguments != nil && !verified[key[len(argumentKeyPrefix):]] {
			delete(decisions, key)
		}
	}
}

// ArgumentPolicy reads the optional budget for old edit and write payloads.
func ArgumentPolicy(object map[string]any) (int, error) {
	value, exists := object["historicalArgumentBytes"]
	if !exists {
		return DefaultArgumentBudgetBytes, nil
	}
	number, ok := value.(json.Number)
	if ok {
		if integer, err := number.Int64(); err == nil && integer >= minimumArgumentBudgetBytes && integer <= maximumArgumentBudgetBytes {
			return int(integer), nil
		}
	}
	return 0, fmt.Errorf("historicalArgumentBytes must be an integer between %d and %d", minimumArgumentBudgetBytes, maximumArgumentBudgetBytes)
}
