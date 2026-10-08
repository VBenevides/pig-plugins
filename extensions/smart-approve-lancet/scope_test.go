package smartapprovelancet

import (
	"encoding/json"
	"testing"
)

func TestParseScopeResponse(t *testing.T) {
	valid := `{"stopReason":"stop","content":[{"type":"text","text":"{\"risk\":\"Kills processes\",\"affectedItems\":[\"process: $PID - SIGKILL; PID unknown\"]}"}]}`
	got, err := parseScopeResponse(json.RawMessage(valid))
	if err != nil || got.Risk != "Kills processes" || len(got.AffectedItems) != 1 {
		t.Fatalf("assessment = %+v, %v", got, err)
	}
	for _, raw := range []string{
		`{}`, `not JSON`,
		`{"stopReason":"error","content":[{"type":"text","text":"{}"}]}`,
		`{"stopReason":"length","content":[]}`,
		`{"stopReason":"stop","content":[{"type":"text","text":"not JSON"}]}`,
		`{"stopReason":"stop","content":[{"type":"text","text":"{\"risk\":\"risk\",\"affectedItems\":[]}"}]}`,
		`{"stopReason":"stop","content":[{"type":"text","text":"{\"risk\":\"risk\",\"affectedItems\":[\" \"]}"}]}`,
	} {
		if _, err := parseScopeResponse(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted invalid response: %s", raw)
		}
	}
}
