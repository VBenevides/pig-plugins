package todo

import (
	_ "embed"
	"encoding/json"
)

//go:embed schema.json
var schemaJSON []byte

// PhasedSchema returns a fresh copy of the public op-based contract.
func PhasedSchema() map[string]any {
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		panic(err)
	}
	return schema
}
