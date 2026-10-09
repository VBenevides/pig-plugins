package notice

import (
	"errors"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

type fakeContext struct {
	sendErr  error
	sent     int
	notified int
}

func (f *fakeContext) Notify(string, string) { f.notified++ }
func (f *fakeContext) SendMessage(string, string, bool, sdk.SendMessageOptions) error {
	f.sent++
	return f.sendErr
}

func TestShowReportsWhetherTranscriptReceivedMessage(t *testing.T) {
	ok := &fakeContext{}
	if !Show(ok, "text", "warning") || ok.sent != 1 || ok.notified != 0 {
		t.Fatalf("transcript send: sent=%d notified=%d", ok.sent, ok.notified)
	}
	failing := &fakeContext{sendErr: errors.New("unavailable")}
	if Show(failing, "text", "warning") || failing.notified != 1 {
		t.Fatalf("fallback: notified=%d", failing.notified)
	}
}
