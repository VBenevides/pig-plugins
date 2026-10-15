package compactlimit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTokens(t *testing.T) {
	good := map[string]int{
		"200000": 200000, "200k": 200000, "200K": 200000, "1M": 1_000_000, "1m": 1_000_000,
		"1.5M": 1_500_000, "0.5k": 500, "250.25k": 250_250, "1": 1,
	}
	for text, want := range good {
		if got, err := ParseTokens(text); err != nil || got != want {
			t.Errorf("ParseTokens(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{
		"", "k", "1.0005k", "1,000", "1 k", " 1k", "1k ", "2G", "0", "0k", "-5k", "+5k", "1.5", "1e3", "0x10",
		"99999999999999999999", "1000001M", "1/2k",
	} {
		if got, err := ParseTokens(text); err == nil {
			t.Errorf("ParseTokens(%q) accepted as %d", text, got)
		}
	}
}

func TestParsePercent(t *testing.T) {
	for text, want := range map[string]float64{"50": 50, "50%": 50, "62.5": 62.5, "100": 100, "0.5": 0.5, "1": 1} {
		if got, err := ParsePercent(text); err != nil || got != want {
			t.Errorf("ParsePercent(%q) = %v, %v; want %v", text, got, err, want)
		}
	}
	for _, text := range []string{"", "0", "0.0", "101", "100.1", "-1", "abc", "5 0", "1e1", "50%%", "%"} {
		if got, err := ParsePercent(text); err == nil {
			t.Errorf("ParsePercent(%q) accepted as %v", text, got)
		}
	}
}

func ptrInt(v int) *int           { return &v }
func ptrFloat(v float64) *float64 { return &v }

func TestEffectiveUsesMinimumAndAlwaysCapsAtWindowMinusReserve(t *testing.T) {
	const window, reserve = 1_000_000, 16384
	const cap = window - reserve
	tests := []struct {
		name        string
		limits      Limits
		window, res int
		limit       int
		userSet     bool
		clamped     string
		native      bool
	}{
		{name: "nothing set is PiG's threshold", window: window, res: reserve, limit: cap},
		{name: "absolute only", limits: Limits{Absolute: ptrInt(250_000)}, window: window, res: reserve, limit: 250_000, userSet: true},
		{name: "relative only", limits: Limits{Relative: ptrFloat(25)}, window: window, res: reserve, limit: 250_000, userSet: true},
		{name: "both, relative smaller", limits: Limits{Relative: ptrFloat(10), Absolute: ptrInt(250_000)}, window: window, res: reserve, limit: 100_000, userSet: true},
		{name: "both, absolute smaller", limits: Limits{Relative: ptrFloat(50), Absolute: ptrInt(250_000)}, window: window, res: reserve, limit: 250_000, userSet: true},
		{name: "absolute above cap is clamped", limits: Limits{Absolute: ptrInt(2_000_000)}, window: window, res: reserve, limit: cap, clamped: "absolute"},
		{name: "absolute equal to cap has no effect", limits: Limits{Absolute: ptrInt(cap)}, window: window, res: reserve, limit: cap, clamped: "absolute"},
		{name: "absolute one below cap", limits: Limits{Absolute: ptrInt(cap - 1)}, window: window, res: reserve, limit: cap - 1, userSet: true},
		{name: "relative 100 is clamped", limits: Limits{Relative: ptrFloat(100)}, window: window, res: reserve, limit: cap, clamped: "relative"},
		{name: "custom reserve moves the cap", limits: Limits{Absolute: ptrInt(900_000)}, window: window, res: 200_000, limit: 800_000, clamped: "absolute"},
		{name: "unknown window is native", limits: Limits{Absolute: ptrInt(1000)}, window: 0, res: reserve, native: true},
		{name: "window not above reserve is native", limits: Limits{Absolute: ptrInt(1000)}, window: 16384, res: reserve, native: true},
		{name: "tiny relative result is at least one token", limits: Limits{Relative: ptrFloat(0.0001)}, window: 100_000, res: reserve, limit: 1, userSet: true},
		{name: "small window", limits: Limits{Absolute: ptrInt(50_000)}, window: 100_000, res: reserve, limit: 50_000, userSet: true},
		{name: "model switch to a larger window keeps the saved absolute", limits: Limits{Absolute: ptrInt(150_000)}, window: 200_000, res: reserve, limit: 150_000, userSet: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Effective(test.limits, test.window, test.res)
			if got.Native != test.native {
				t.Fatalf("native = %v", got.Native)
			}
			if test.native {
				if got.ShouldCompact(1 << 30) {
					t.Fatal("native result must never trigger")
				}
				return
			}
			if got.Limit != test.limit || got.UserSet != test.userSet || got.Cap != test.window-test.res {
				t.Fatalf("got %+v", got)
			}
			if test.clamped != "" && !strings.Contains(strings.Join(got.Clamped, ","), test.clamped) {
				t.Fatalf("clamped = %v, want %s", got.Clamped, test.clamped)
			}
			if test.clamped == "" && len(got.Clamped) != 0 {
				t.Fatalf("unexpected clamped = %v", got.Clamped)
			}
		})
	}
}

func TestShouldCompactOnlyAboveAUserLimit(t *testing.T) {
	limited := Effective(Limits{Absolute: ptrInt(50_000)}, 100_000, DefaultReserveTokens)
	if limited.ShouldCompact(50_000) || !limited.ShouldCompact(50_001) {
		t.Fatal("the limit is exclusive: tokens must be above it")
	}
	if Effective(Limits{}, 100_000, DefaultReserveTokens).ShouldCompact(99_999) {
		t.Fatal("with no user limit PiG's own trigger decides")
	}
}

func TestSaveLoadRoundTripKeepsOtherKeysAndRejectsDamage(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nested", FileName)
	if limits, err := Load(file); err != nil || limits.IsSet() {
		t.Fatalf("missing file: %+v, %v", limits, err)
	}
	if err := Save(file, Limits{Relative: ptrFloat(62.5), Absolute: ptrInt(200_000)}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", info, err)
	}
	limits, err := Load(file)
	if err != nil || limits.Relative == nil || *limits.Relative != 62.5 || limits.Absolute == nil || *limits.Absolute != 200_000 {
		t.Fatalf("round trip: %+v, %v", limits, err)
	}
	if err := os.WriteFile(file, []byte(`{"other":{"n":9007199254740993},"limit":{"absolute":123456}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(file, Limits{Absolute: ptrInt(7)}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "9007199254740993") || strings.Contains(string(data), "relative") {
		t.Fatalf("other keys not kept or stale key left: %s", data)
	}
	if err := Save(file, Limits{}); err != nil {
		t.Fatal(err)
	}
	if limits, err := Load(file); err != nil || limits.IsSet() {
		t.Fatalf("cleared: %+v, %v", limits, err)
	}
	for _, text := range []string{`[]`, `{"limit":3}`, `{"limit":{"relative":"50"}}`, `{"limit":{"relative":101}}`, `{"limit":{"absolute":0}}`, `{"limit":{"absolute":1.5}}`, `not json`} {
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(file); err == nil {
			t.Errorf("Load accepted %s", text)
		}
		if err := Save(file, Limits{Absolute: ptrInt(1)}); err == nil && (text == `[]` || text == `not json`) {
			t.Errorf("Save replaced a file that is not a JSON object: %s", text)
		}
	}
}

func TestReserveTokensProjectOverGlobalOverDefault(t *testing.T) {
	agent, project := t.TempDir(), t.TempDir()
	getenv := func(key string) string {
		if key == "PIG_CODING_AGENT_DIR" {
			return agent
		}
		return ""
	}
	if got := ReserveTokens(getenv, project); got != DefaultReserveTokens {
		t.Fatalf("default = %d", got)
	}
	write := func(dir, text string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(agent, `{"compaction":{"reserveTokens":30000}}`)
	if got := ReserveTokens(getenv, project); got != 30000 {
		t.Fatalf("global = %d", got)
	}
	write(filepath.Join(project, ".pig"), `{"compaction":{"reserveTokens":40000}}`)
	if got := ReserveTokens(getenv, project); got != 40000 {
		t.Fatalf("project = %d", got)
	}
	write(filepath.Join(project, ".pig"), `{"compaction":{"reserveTokens":-1}}`)
	if got := ReserveTokens(getenv, project); got != 30000 {
		t.Fatalf("invalid project value must be ignored, got %d", got)
	}
}
