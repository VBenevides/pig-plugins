package rtk

import "context"

// ProcessResult returns an additive tool_result override. It deliberately never
// returns isError, input, or structuredContent; the host retains those fields.
// Failed/aborted/permission results and non-text variants are left untouched.
func ProcessResult(ctx context.Context, recovery *Recovery, event map[string]any, enabled bool) map[string]any {
	if !enabled || event["toolName"] != "bash" || event["isError"] == true {
		return nil
	}
	input, _ := event["input"].(map[string]any)
	command, _ := input["command"].(string)
	var blocks []map[string]any
	switch content := event["content"].(type) {
	case []map[string]any:
		blocks = content
	case []any:
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				return nil
			}
			blocks = append(blocks, block)
		}
	default:
		return nil
	}
	if len(blocks) != 1 || blocks[0]["type"] != "text" {
		return nil
	}
	raw, ok := blocks[0]["text"].(string)
	if !ok {
		return nil
	}
	reduced, technique, err := Reduce(command, raw)
	var meta *Compaction
	text := raw
	if err == nil && technique != "" {
		text, meta, err = recovery.Present(ctx, raw, reduced)
	}
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": raw + "\n[RTK: compression unavailable; original host result retained: " + err.Error() + "]"}}}
	}
	if meta == nil {
		return nil
	}
	details := map[string]any{}
	if original, ok := event["details"].(map[string]any); ok {
		for key, value := range original {
			details[key] = value
		}
	} else if event["details"] != nil {
		return nil
	}
	details["rtkCompaction"] = map[string]any{"technique": technique, "inputBytes": meta.InputBytes, "outputBytes": meta.OutputBytes, "rawOutputPath": meta.RawOutputPath}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "details": details}
}
