package notice

import (
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

type diagnosticContext struct {
	ui      bool
	sent    int
	notices []string
	levels  []string
}

func (c *diagnosticContext) HasUI() bool { return c.ui }
func (c *diagnosticContext) Notify(text, level string) {
	c.notices = append(c.notices, text)
	c.levels = append(c.levels, level)
}
func (c *diagnosticContext) SendMessage(string, string, bool, sdk.SendMessageOptions) error {
	c.sent++
	return nil
}

func TestShowNeverInjectsDisplayDiagnosticsIntoConversation(t *testing.T) {
	c := &diagnosticContext{ui: true}
	for _, message := range []string{"Claude HTTP 429 rate limit", "native OAuth credentials unavailable or expired", "Switched to another openai-codex account with available quota"} {
		if !Show(c, message, "warning") {
			t.Fatal("UI notification not dispatched")
		}
	}
	if c.sent != 0 || len(c.notices) != 3 || len(c.levels) != 3 {
		t.Fatalf("injected=%d notices=%v levels=%v", c.sent, c.notices, c.levels)
	}
}

func TestShowHeadlessKeepsDiagnosticAvailableForLogging(t *testing.T) {
	c := &diagnosticContext{}
	if Show(c, "failed", "error") || c.sent != 0 {
		t.Fatal("headless diagnostic claimed visible or was injected")
	}
}
