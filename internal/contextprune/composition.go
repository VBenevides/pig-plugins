package contextprune

import (
	"encoding/json"
	"strings"
)

// Composition counts serialized UTF-8 bytes of message content by category.
// It excludes system instructions, tool schemas, media payloads, IDs, and request framing.
// The numbers are not provider tokens and not a complete request size.
type Composition struct {
	UserText, AssistantText, CallArguments         int
	ResultText, FailureText                        int
	ReducedResults, RecoveryNotices, Media         int
	ProtectedResults                               int
	ResultsTotal, ResultsReduced, ResultsProtected int
}

// Compose measures a view. Marker text counts as recovery overhead, not as result text.
// protectedFrom is the first message index of the protected recent window.
func Compose(view []any, protectedFrom int) Composition {
	var out Composition
	for index, value := range view {
		message, _ := value.(map[string]any)
		blocks, _ := message["content"].([]any)
		text, _ := message["content"].(string)
		switch message["role"] {
		case "user":
			out.UserText += len(text) + blockText(blocks)
		case "assistant":
			out.AssistantText += len(text) + blockText(blocks)
			for _, raw := range blocks {
				if block, _ := raw.(map[string]any); block["type"] == "toolCall" {
					data, err := json.Marshal(block["arguments"])
					if err == nil {
						out.CallArguments += len(data)
					}
				}
			}
		case "toolResult":
			out.ResultsTotal++
			body, plain := resultPlainBody(message)
			if !plain {
				out.Media += blockCount(blocks, "image")
				continue
			}
			switch {
			case strings.Contains(body, "status unchanged") || strings.HasPrefix(body, "[pruned:"):
				out.RecoveryNotices += len(body)
				out.ResultsReduced++
			case message["isError"] == true:
				out.FailureText += len(body)
			default:
				out.ResultText += len(body)
			}
			if index >= protectedFrom {
				out.ProtectedResults += len(body)
				out.ResultsProtected++
			}
		}
	}
	return out
}

func blockText(blocks []any) int {
	total := 0
	for _, raw := range blocks {
		if block, _ := raw.(map[string]any); block["type"] == "text" {
			text, _ := block["text"].(string)
			total += len(text)
		}
	}
	return total
}

func blockCount(blocks []any, kind string) int {
	count := 0
	for _, raw := range blocks {
		if block, _ := raw.(map[string]any); block["type"] == kind {
			count++
		}
	}
	return count
}
