package requesttimer

import (
	"testing"
	"time"
)

func TestStopReturnsElapsedOnlyOnce(t *testing.T) {
	timer := &Timer{}
	timer.Start(func(string) {})
	elapsed, running := timer.Stop(func(string) {})
	if !running || elapsed < 0 {
		t.Fatalf("first stop=%s,%v", elapsed, running)
	}
	if elapsed, running := timer.Stop(func(string) {}); running || elapsed != 0 {
		t.Fatalf("duplicate stop=%s,%v", elapsed, running)
	}
	timer.Start(func(string) {})
	elapsed, running = timer.Stop(func(string) {})
	if !running || elapsed > time.Second {
		t.Fatalf("new request did not reset: %s,%v", elapsed, running)
	}
}
