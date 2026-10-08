package betterfooter

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

type reportHost struct {
	hasUI    bool
	messages []string
	levels   []string
}

func (h *reportHost) HasUI() bool { return h.hasUI }
func (h *reportHost) Notify(message, level string) {
	h.messages = append(h.messages, message)
	h.levels = append(h.levels, level)
}

func TestReportSanitizesMaliciousPathErrors(t *testing.T) {
	var output bytes.Buffer
	writer := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(writer) })
	for _, command := range []string{"\x1b]52;c;secret\x1b\\", "\x1b[2J", "\u009d52;c;secret\u009c", "\u009b2J"} {
		for _, hasUI := range []bool{false, true} {
			output.Reset()
			host := &reportHost{hasUI: hasUI}
			x := &extension{}
			x.report(host, &os.PathError{Op: "open", Path: "/project/" + command + "package.json", Err: os.ErrPermission})
			want := "better-footer: open /project/package.json: permission denied"
			if got := output.String(); !strings.Contains(got, want) || strings.Contains(got, command) || strings.Contains(got, "secret") {
				t.Fatalf("unsafe or missing log for %q: %q", command, got)
			}
			if hasUI {
				if len(host.messages) != 1 || host.messages[0] != want || host.levels[0] != "warning" {
					t.Fatalf("unsafe or missing notification: %q %q", host.messages, host.levels)
				}
			} else if len(host.messages) != 0 {
				t.Fatalf("notified without UI: %q", host.messages)
			}
		}
	}
}
