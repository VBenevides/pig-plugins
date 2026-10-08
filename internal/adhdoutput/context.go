package adhdoutput

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

const (
	StateType    = "adhd-output.state"
	RulesType    = "adhd-output.rules.v1"
	DisabledType = "adhd-output.disabled.v1"
	StatusKey    = "adhd-output"
	Badge        = "● ADHD ON"
	Cancellation = "[adhd-output.disabled.v1]\nADHD output mode is disabled. The earlier ADHD-friendly presentation instructions are no longer in effect. Follow the current system and project instructions, tool contracts, and the user's requested response style."
)

type State struct {
	Version int  `json:"version"`
	Enabled bool `json:"enabled"`
}

// Snapshot uses raw active ancestry for state and the host's resolved context for markers.
// These are different views: compaction removes messages, not branch preferences.
type Snapshot struct {
	SessionID string
	LeafID    string
	Branch    []map[string]any
	Messages  []map[string]any
	Idle      bool
}

// Choice rejects malformed ancestry and malformed latest state instead of falling back to off.
func (s Snapshot) Choice() (enabled, explicit bool, err error) {
	previous := ""
	seen := make(map[string]struct{}, len(s.Branch))
	for _, entry := range s.Branch {
		id, ok := entry["id"].(string)
		parent, parentOK := entry["parentId"].(string)
		if entry["parentId"] == nil {
			parentOK = true
		}
		if !ok || id == "" || !parentOK || parent != previous {
			return false, false, errors.New("invalid active session ancestry")
		}
		if _, duplicate := seen[id]; duplicate {
			return false, false, errors.New("duplicate entry in active session ancestry")
		}
		seen[id] = struct{}{}
		previous = id
	}
	if previous != s.LeafID {
		return false, false, errors.New("active branch changed during session read")
	}
	for i := len(s.Branch) - 1; i >= 0; i-- {
		entry := s.Branch[i]
		if entry["type"] != "custom" || entry["customType"] != StateType {
			continue
		}
		data, ok := entry["data"].(map[string]any)
		if !ok {
			return false, false, errors.New("ADHD state has no object data")
		}
		version, ok := data["version"].(float64)
		if !ok || version != 1 {
			return false, false, errors.New("unsupported ADHD state version")
		}
		enabled, ok := data["enabled"].(bool)
		if !ok {
			return false, false, errors.New("ADHD state has no boolean enabled value")
		}
		return enabled, true, nil
	}
	return false, false, nil
}

func (s Snapshot) Contains(id string) bool {
	if id == "" {
		return true
	} // The empty leaf is the root before the first append.
	for _, entry := range s.Branch {
		if entry["id"] == id {
			return true
		}
	}
	return false
}

func RulesMessage(rules string) string {
	return fmt.Sprintf("[%s sha256=%x]\n%s", RulesType, sha256.Sum256([]byte(rules)), rules)
}

type marker struct{ active, current bool }

// Only actual custom messages count. A quoted marker in user text or a summary does not.
func contextMarker(messages []map[string]any, rulesMessage string) marker {
	var latest marker
	for _, message := range messages {
		if message["role"] != "custom" {
			continue
		}
		switch message["customType"] {
		case RulesType:
			latest = marker{active: true, current: messageText(message["content"]) == rulesMessage}
		case DisabledType:
			if strings.Contains(messageText(message["content"]), "["+DisabledType+"]") {
				latest = marker{}
			}
		}
	}
	return latest
}

func messageText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	blocks, ok := content.([]any)
	if !ok {
		return ""
	}
	var text strings.Builder
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok || block["type"] != "text" {
			continue
		}
		part, _ := block["text"].(string)
		text.WriteString(part)
	}
	return text.String()
}
