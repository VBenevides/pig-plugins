package todo

// PhasedSchema is pi-todotools' public op-based parameter contract.
func PhasedSchema() map[string]any {
	task := map[string]any{"type": "string", "description": "Task content"}
	return map[string]any{"type": "object", "required": []string{"op"}, "properties": map[string]any{
		"op":    map[string]any{"type": "string", "enum": []string{"init", "start", "done", "rm", "drop", "append", "view"}},
		"task":  task,
		"phase": map[string]any{"type": "string", "description": "Phase name"},
		"items": map[string]any{"type": "array", "items": task, "description": "Tasks to append"},
		"list":  map[string]any{"type": "array", "description": "Phased task list for init", "items": map[string]any{"type": "object", "required": []string{"phase", "items"}, "properties": map[string]any{"phase": map[string]any{"type": "string", "description": "Phase name"}, "items": map[string]any{"type": "array", "items": task, "minItems": 1, "description": "Tasks for this phase"}}}},
	}}
}
