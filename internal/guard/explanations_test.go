package guard

import "testing"

func TestEveryBehaviorHasAnExplanationThatIsNotItsLabel(t *testing.T) {
	for id, label := range behaviorLabels {
		explanation, ok := behaviorExplanations[id]
		if !ok || explanation == "" {
			t.Errorf("behavior %q has no explanation", id)
			continue
		}
		if explanation == label {
			t.Errorf("behavior %q explanation repeats its label", id)
		}
	}
	for id := range behaviorExplanations {
		if _, ok := behaviorLabels[id]; !ok {
			t.Errorf("explanation for unknown behavior %q", id)
		}
	}
}

func TestDescribeRiskKeepsUnknownBehaviors(t *testing.T) {
	got := describeRisk([]string{"sudo", "mystery"}, []string{"sudo command", "mystery"}, "")
	want := "- sudo command: " + behaviorExplanations["sudo"] + "\n- mystery"
	if got != want {
		t.Errorf("describeRisk = %q, want %q", got, want)
	}
}
