package websearch

import "testing"

func TestThinkingClampsUnavailableEffort(t *testing.T) {
	for _, tc := range []struct {
		requested, want string
		mapping         map[string]*string
		enabled         bool
	}{{"xhigh", "high", nil, true}, {"medium", "deep", map[string]*string{"medium": nil, "high": new("deep")}, true}, {"high", "medium", map[string]*string{"high": nil, "xhigh": nil}, true}, {"unknown", "", nil, false}, {"off", "", nil, false}} {
		t.Run(tc.requested+tc.want, func(t *testing.T) {
			effort, enabled := thinkingEffort(Model{Reasoning: true, ThinkingMap: tc.mapping}, tc.requested)
			if effort != tc.want || enabled != tc.enabled {
				t.Fatalf("effort=%q enabled=%v; want %q %v", effort, enabled, tc.want, tc.enabled)
			}
		})
	}
}
