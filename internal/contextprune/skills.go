package contextprune

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
)

// SkillBody identifies one complete skill envelope in a user message. It does not
// confer activation provenance or permission to execute the skill.
type SkillBody struct {
	ID, Name, Location string
	MessageKey         string
	Block, Start, End  int
	Text               string
}

// SkillEdit replaces one envelope while retaining the surrounding content.
type SkillEdit struct {
	Block, Start, End     int
	Original, Replacement string
}

func messageKey(message map[string]any) string {
	data, err := json.Marshal(message)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "skill:" + hex.EncodeToString(sum[:])
}

func textBlocks(message map[string]any) map[int]string {
	if message["role"] != "user" {
		return nil
	}
	if text, ok := message["content"].(string); ok {
		return map[int]string{-1: text}
	}
	blocks, _ := message["content"].([]any)
	text := map[int]string{}
	for i, raw := range blocks {
		b, _ := raw.(map[string]any)
		if b["type"] == "text" {
			if s, ok := b["text"].(string); ok {
				text[i] = s
			}
		}
	}
	return text
}

// SkillBodies accepts complete, standalone skill envelopes outside fenced code.
// Malformed, nested, or ambiguous envelopes are retained. Text and image blocks
// are not joined, so their boundaries and metadata survive pruning.
func SkillBodies(message map[string]any) []SkillBody {
	key := messageKey(message)
	if key == "" {
		return nil
	}
	var found []SkillBody
	for block, text := range textBlocks(message) {
		var fence string
		offset := 0
		for offset < len(text) {
			line, _, _ := strings.Cut(text[offset:], "\n")
			next := offset + len(line)
			if next < len(text) {
				next++
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				marker := trimmed[:3]
				if fence == "" {
					fence = marker
				} else if fence == marker {
					fence = ""
				}
			}
			if fence != "" || !strings.HasPrefix(line, `<skill `) {
				offset = next
				continue
			}
			name, location, ok := skillHeader(line)
			if !ok {
				return nil
			}
			// A closing tag must occupy a line, not be prose or a quoted example.
			endStart := next
			closed := false
			for endStart < len(text) {
				endLine, _, _ := strings.Cut(text[endStart:], "\n")
				if strings.HasPrefix(endLine, "<skill ") {
					return nil
				}
				if strings.TrimSuffix(endLine, "\r") == "</skill>" {
					end := endStart + len(endLine)
					original := text[offset:end]
					found = append(found, SkillBody{ID: fmt.Sprintf("%s:%d:%d", key, block, offset), Name: name, Location: location, MessageKey: key, Block: block, Start: offset, End: end, Text: original})
					offset = end
					if offset < len(text) {
						offset++
					}
					closed = true
					break
				}
				endStart += len(endLine)
				if endStart < len(text) {
					endStart++
				}
			}
			if !closed {
				return nil
			}
		}
	}
	return found
}

func skillHeader(line string) (string, string, bool) {
	line = strings.TrimSuffix(line, "\r")
	if !strings.HasSuffix(line, ">") {
		return "", "", false
	}
	decoder := xml.NewDecoder(strings.NewReader(strings.TrimSuffix(line, ">") + "/>"))
	token, err := decoder.Token()
	if err != nil {
		return "", "", false
	}
	start, ok := token.(xml.StartElement)
	if !ok || start.Name.Local != "skill" || start.Name.Space != "" {
		return "", "", false
	}
	name, location := "", ""
	seen := map[string]bool{}
	for _, a := range start.Attr {
		if seen[a.Name.Local] || a.Name.Space != "" {
			return "", "", false
		}
		seen[a.Name.Local] = true
		switch a.Name.Local {
		case "name":
			name = a.Value
		case "location":
			location = a.Value
		default:
			return "", "", false
		}
	}
	return name, location, name != "" && location != ""
}

func skillEditsDecision(bodies []SkillBody, eligible func(SkillBody) bool, reason string) Decision {
	var d Decision
	for _, body := range bodies {
		if !eligible(body) {
			continue
		}
		marker := fmt.Sprintf("[pruned: skill %s (%s); %s]", body.Name, body.ID, reason)
		if len(marker) >= len(body.Text) {
			continue
		}
		d.SkillEdits = append(d.SkillEdits, SkillEdit{Block: body.Block, Start: body.Start, End: body.End, Original: body.Text, Replacement: marker})
		d.Saved += len(body.Text) - len(marker)
	}
	return d
}

func applySkillEdits(message map[string]any, d Decision) (map[string]any, bool) {
	texts := textBlocks(message)
	// Validate the entire decision before applying any part.
	for _, e := range d.SkillEdits {
		text, ok := texts[e.Block]
		if !ok || e.Start < 0 || e.End > len(text) || e.Start > e.End || text[e.Start:e.End] != e.Original {
			return nil, false
		}
	}
	out := make(map[string]any, len(message))
	for k, v := range message {
		out[k] = v
	}
	for block, text := range texts {
		var edits []SkillEdit
		for _, e := range d.SkillEdits {
			if e.Block == block {
				edits = append(edits, e)
			}
		}
		// Bodies arrive in source order within each block.
		for i := len(edits) - 1; i >= 0; i-- {
			e := edits[i]
			text = text[:e.Start] + e.Replacement + text[e.End:]
		}
		texts[block] = text
	}
	if _, ok := message["content"].(string); ok {
		out["content"] = texts[-1]
	} else {
		blocks := append([]any(nil), message["content"].([]any)...)
		for i, text := range texts {
			b := map[string]any{}
			for k, v := range blocks[i].(map[string]any) {
				b[k] = v
			}
			b["text"] = text
			blocks[i] = b
		}
		out["content"] = blocks
	}
	return out, true
}
