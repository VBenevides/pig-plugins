package requesttimer

import (
	"sync"
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	for _, tt := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "Working - 00h 00m 00s"},
		{0, "Working - 00h 00m 00s"},
		{59 * time.Second, "Working - 00h 00m 59s"},
		{time.Minute, "Working - 00h 01m 00s"},
		{time.Hour, "Working - 01h 00m 00s"},
		{25*time.Hour + 2*time.Minute + 3*time.Second, "Working - 25h 02m 03s"},
	} {
		if got := Format(tt.d); got != tt.want {
			t.Errorf("Format(%s)=%q want %q", tt.d, got, tt.want)
		}
	}
}

func TestRequestLifecycle(t *testing.T) {
	var timer Timer
	var mu sync.Mutex
	var values []string
	update := func(s string) { mu.Lock(); values = append(values, s); mu.Unlock() }
	timer.Start(update)
	time.Sleep(1100 * time.Millisecond) // Model calls and tools do not reset the loop.
	timer.Stop(update)
	mu.Lock()
	if len(values) < 3 || values[0] != Format(0) || values[len(values)-1] != "" {
		t.Errorf("updates=%v", values)
	}
	count := len(values)
	mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	mu.Lock()
	if len(values) != count {
		t.Error("update after Stop")
	}
	mu.Unlock()
	timer.Start(update)
	timer.Start(update) // Replacing a request stops the old loop.
	timer.Stop(update)
	timer.Stop(update)
}

func TestConcurrentStartStop(t *testing.T) {
	var timer Timer
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); timer.Start(func(string) {}); timer.Stop(func(string) {}) }()
	}
	wg.Wait()
	timer.Stop(func(string) {})
}
