package cursor

import (
	"encoding/json"
	"fmt"

	"github.com/VBenevides/pig-plugins/internal/cursormodels"
)

const catalogVersion = "cursor-catalog-v1:"

// The host preserves model extension fields and ETag in ModelsStoreEntry.
// Keep exact selections with each model, rather than guessing from its public ID.
func persistCatalog(account string, entries []cursormodels.Entry) (json.RawMessage, error) {
	models := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		model := modelMap(entry)
		model["cursorSelection"] = entry
		models = append(models, model)
	}
	data, err := json.Marshal(map[string]any{"etag": catalogVersion + account, "models": models})
	if err != nil {
		return nil, fmt.Errorf("persist Cursor catalog: %w", err)
	}
	return data, nil
}

func restoreCatalog(stored map[string]any, account string) ([]cursormodels.Entry, error) {
	if stored == nil || stored["etag"] != catalogVersion+account {
		return nil, nil
	}
	data, err := json.Marshal(stored["models"])
	if err != nil {
		return nil, fmt.Errorf("read stored Cursor catalog: %w", err)
	}
	var models []struct {
		ID        string             `json:"id"`
		Selection cursormodels.Entry `json:"cursorSelection"`
	}
	if err = json.Unmarshal(data, &models); err != nil {
		return nil, fmt.Errorf("decode stored Cursor catalog: %w", err)
	}
	if models == nil {
		return nil, fmt.Errorf("stored Cursor catalog has no model list")
	}
	entries := make([]cursormodels.Entry, 0, len(models))
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		entry := model.Selection
		if entry.ID == "" || entry.ID != model.ID || seen[entry.ID] || len(entry.Selections) == 0 {
			return nil, fmt.Errorf("stored Cursor catalog has an invalid model selection")
		}
		for level, selection := range entry.Selections {
			valid := level == "" && !entry.Reasoning
			for _, known := range cursormodels.Levels {
				if entry.Reasoning && level == known {
					valid = true
				}
			}
			if !valid || selection.ID == "" {
				return nil, fmt.Errorf("stored Cursor catalog has an invalid thinking selection")
			}
		}
		seen[entry.ID] = true
		entries = append(entries, entry)
	}
	return entries, nil
}
