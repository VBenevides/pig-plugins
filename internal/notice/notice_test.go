package notice

import "testing"

type fakeContext struct {
	notified int
}

func (f *fakeContext) Notify(string, string) { f.notified++ }

func TestShowSupportsNotifierOnly(t *testing.T) {
	c := &fakeContext{}
	if !Show(c, "text", "warning") || c.notified != 1 {
		t.Fatalf("notified=%d", c.notified)
	}
}
