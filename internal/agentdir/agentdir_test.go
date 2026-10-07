package agentdir

import "testing"

func lookup(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func TestDirPrecedence(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"coding agent dir wins", map[string]string{"PIG_CODING_AGENT_DIR": "/a", "PIG_HOME": "/h", "HOME": "/u"}, "/a"},
		{"pig home next", map[string]string{"PIG_HOME": "/h", "HOME": "/u"}, "/h/agent"},
		{"home fallback", map[string]string{"HOME": "/u"}, "/u/.pig/agent"},
		{"empty values are unset", map[string]string{"PIG_CODING_AGENT_DIR": "", "PIG_HOME": "", "HOME": "/u"}, "/u/.pig/agent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Dir(lookup(tc.env)); got != tc.want {
				t.Fatalf("Dir() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFileJoinsInsideAgentDir(t *testing.T) {
	got := File(lookup(map[string]string{"PIG_HOME": "/h"}), "pi-curator.json")
	if got != "/h/agent/pi-curator.json" {
		t.Fatalf("File() = %q", got)
	}
}
