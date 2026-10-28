package contextprune

import (
	"encoding/json"
	"strings"
	"testing"
)

func messageEntry(m map[string]any) map[string]any {
	return map[string]any{"type": "message", "message": m}
}
func stateEntry(data map[string]any) map[string]any {
	return map[string]any{"type": "custom", "customType": SkillStateType, "data": data}
}

func TestSkillLifetimesRequireExplicitCompletion(t *testing.T) {
	parent := skillMessage(strings.Repeat("parent ", 100), "parent request", 1)
	child := skillMessage(strings.Repeat("child ", 100), "delegated work", 2)
	entries := []map[string]any{messageEntry(parent), messageEntry(child), messageEntry(user("another prompt")), {"type": "compaction"}}
	inv := Lifetimes(entries)
	if len(inv) != 2 || inv[0].State != "unknown" || inv[1].State != "unknown" {
		t.Fatal("nested or elapsed work completed a skill")
	}
	data, err := SkillTransition(entries, inv[1].ID, "finish")
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, stateEntry(data))
	if got := Lifetimes(entries); got[0].State != "unknown" || got[1].State != "finished" {
		t.Fatal("completion leaked to parent")
	}
	// Simulate restart from serialized session entries.
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	var restored []map[string]any
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if got := Lifetimes(restored); got[1].State != "finished" || got[1].ID != inv[1].ID {
		t.Fatal("restart lost lifetime state")
	}
	// Navigating before the completion or to another branch cannot see it.
	if got := Lifetimes(entries[:2]); got[1].State != "unknown" {
		t.Fatal("completion crossed branch boundary")
	}
	if _, err := SkillTransition(entries[:1], inv[1].ID, "finish"); err == nil {
		t.Fatal("accepted an invocation from another branch")
	}
}

func TestSkillLifetimeActionsAndUnknownIDs(t *testing.T) {
	entries := []map[string]any{messageEntry(skillMessage("rules", "task", 1))}
	id := Lifetimes(entries)[0].ID
	for _, test := range []struct{ action, state string }{{"active", "active"}, {"keep", "retained"}, {"release", "idle"}, {"finish", "finished"}} {
		data, err := SkillTransition(entries, id, test.action)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, stateEntry(data))
		if Lifetimes(entries)[0].State != test.state {
			t.Fatalf("action %s failed", test.action)
		}
	}
	if _, err := SkillTransition(entries, "other-session", "finish"); err == nil {
		t.Fatal("accepted unknown ID")
	}
	if _, err := SkillTransition(entries, id, "guess"); err == nil {
		t.Fatal("accepted unknown action")
	}
}
