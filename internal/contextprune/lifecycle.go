package contextprune

import (
	"fmt"
	"sort"
)

// SkillStateType is the session custom-entry type for explicit lifetime changes.
const SkillStateType = "context-prune.skill-state"

// InteractionType records an explicit non-command user input, not a tool turn.
const InteractionType = "context-prune.user-interaction"

// Invocation is one skill envelope in the active branch. Unknown is protected:
// recognizing markup never proves that a workflow is complete or authorized.
type Invocation struct {
	SkillBody
	State string
	// Age counts recorded user interactions since invocation or the last state change.
	Age int
}

// Lifetimes rebuilds state only from the active branch. No assistant prose, run
// boundary, nested invocation, or compaction entry marks a skill finished.
func Lifetimes(entries []map[string]any) []Invocation {
	var invocations []Invocation
	indexes := map[string]int{}
	duplicates := map[string]bool{}
	turns := 0
	born := map[string]int{}
	for _, entry := range entries {
		if entry["type"] == "custom" && entry["customType"] == InteractionType {
			turns++
		}
		if entry["type"] == "message" {
			message, _ := entry["message"].(map[string]any)
			bodies := SkillBodies(message)
			sort.Slice(bodies, func(i, j int) bool {
				if bodies[i].Block != bodies[j].Block {
					return bodies[i].Block < bodies[j].Block
				}
				return bodies[i].Start < bodies[j].Start
			})
			for _, body := range bodies {
				if _, exists := indexes[body.ID]; exists {
					duplicates[body.ID] = true
					continue
				}
				indexes[body.ID] = len(invocations)
				invocations = append(invocations, Invocation{SkillBody: body, State: "unknown"})
				born[body.ID] = turns
			}
		}
		if entry["type"] != "custom" || entry["customType"] != SkillStateType {
			continue
		}
		data, _ := entry["data"].(map[string]any)
		id, _ := data["id"].(string)
		state, _ := data["state"].(string)
		if index, ok := indexes[id]; ok && validSkillState(state) {
			invocations[index].State = state
			born[id] = turns
		}
	}
	for id := range duplicates {
		invocations[indexes[id]].State = "unknown"
	}
	for i := range invocations {
		invocations[i].Age = turns - born[invocations[i].ID]
	}
	return invocations
}

func validSkillState(state string) bool {
	switch state {
	case "active", "finished", "retained", "idle":
		return true
	}
	return false
}

// SkillTransition validates the exact identifier against the active branch.
// Persist its returned data before reporting success. No transition executes a skill.
func SkillTransition(entries []map[string]any, id, action string) (map[string]any, error) {
	states := map[string]string{"finish": "finished", "keep": "retained", "active": "active", "release": "idle"}
	state, ok := states[action]
	if !ok {
		return nil, fmt.Errorf("unknown skill action %q", action)
	}
	for _, inv := range Lifetimes(entries) {
		if inv.ID == id {
			return map[string]any{"id": id, "state": state}, nil
		}
	}
	return nil, fmt.Errorf("skill invocation %q is not in the active branch", id)
}

// SkillReasons contains only finished bodies and explicitly released idle bodies
// whose opt-in expiry elapsed. Unknown, active, and retained bodies stay protected.
func SkillReasons(invocations []Invocation, maxAge int) map[string]string {
	reasons := map[string]string{}
	for _, inv := range invocations {
		if inv.State == "finished" {
			reasons[inv.ID] = "explicitly finished"
		}
		if inv.State == "idle" && maxAge > 0 && inv.Age >= maxAge {
			reasons[inv.ID] = "released invocation expired"
		}
	}
	return reasons
}
