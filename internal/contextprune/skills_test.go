package contextprune

import (
	"encoding/json"
	"strings"
	"testing"
)

func skillMessage(body, request string, timestamp int) map[string]any {
	m := user(`<skill name="review" location="/skills/review/SKILL.md">` + "\n" + body + "\n</skill>\n" + request)
	m["timestamp"] = timestamp
	return m
}

func TestSkillPrunePreservesRequestAndSurvivesReordering(t *testing.T) {
	body := strings.Repeat("skill rules\n", 100)
	first := skillMessage(body, "UNIQUE REQUEST: keep this", 1)
	second := skillMessage(body, "new request", 2)
	messages := history(first, second)
	before, _ := json.Marshal(messages)
	decisions := Decide(messages, small)
	out, n := Apply(messages, decisions)
	if n != 1 || !strings.Contains(resultText(t, out[0]), "UNIQUE REQUEST: keep this") || strings.Contains(resultText(t, out[0]), body) {
		t.Fatalf("prune only the body, not the request: %v", out[0])
	}
	reordered := append([]any{user("unrelated")}, messages...)
	out, n = Apply(reordered, decisions)
	if n != 1 || !strings.Contains(resultText(t, out[1]), "UNIQUE REQUEST") || resultText(t, out[0]) != "unrelated" {
		t.Fatal("decision moved to the wrong message")
	}
	after, _ := json.Marshal(messages)
	if string(before) != string(after) {
		t.Fatal("history changed")
	}
}

func TestSkillBoundariesAndUncertainMarkup(t *testing.T) {
	body := strings.Repeat("rules ", 200)
	envelope := `<skill name="review" location="/x">` + "\n" + body + "\n</skill>"
	for _, text := range []string{envelope + "\nrequest", "prefix\n" + envelope + "\nrequest", envelope + "\n" + envelope + "\nrequest"} {
		m := user(text)
		m["timestamp"] = 1
		later := user(envelope)
		later["timestamp"] = 2
		out, n := Apply(history(m, later), Decide(history(m, later), small))
		if n != 1 || !strings.Contains(resultText(t, out[0]), "request") || strings.Contains(resultText(t, out[0]), body) {
			t.Fatalf("bad boundaries: %v", out[0])
		}
	}
	for _, text := range []string{"```xml\n" + envelope + "\n```", "> " + envelope, strings.TrimSuffix(envelope, "</skill>"), strings.Replace(envelope, "location=", "unknown=", 1)} {
		if got := SkillBodies(user(text)); len(got) != 0 {
			t.Fatalf("uncertain envelope recognized: %q", text)
		}
	}
}

func TestSkillMixedBlocksAndChangedBody(t *testing.T) {
	body := strings.Repeat("rules ", 200)
	m := skillMessage(body, "request", 1)
	blocks := m["content"].([]any)
	image := map[string]any{"type": "image", "data": "keep-image"}
	m["content"] = append(blocks, image, map[string]any{"type": "text", "text": "extra instructions"})
	later := skillMessage(body, "new request", 2)
	out, n := Apply(history(m, later), Decide(history(m, later), small))
	if n != 1 {
		t.Fatal("mixed block skill was not pruned")
	}
	got := out[0].(map[string]any)["content"].([]any)
	if got[1].(map[string]any)["data"] != "keep-image" || got[2].(map[string]any)["text"] != "extra instructions" {
		t.Fatal("surrounding blocks changed")
	}
	changed := skillMessage(body+"NEW RULE", "new request", 3)
	if len(Decide(history(m, changed), small)) != 0 {
		t.Fatal("changed body is not a duplicate")
	}
}
