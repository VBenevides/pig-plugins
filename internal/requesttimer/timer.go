// Package requesttimer tracks elapsed time for one agent request.
package requesttimer

import (
	"fmt"
	"sync"
	"time"
)

// Timer owns at most one update loop. Stop waits for pending updates before clearing the display.
type Timer struct {
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

// Format renders elapsed time without a 24-hour wrap.
func Format(elapsed time.Duration) string {
	seconds := max(int64(elapsed/time.Second), 0)
	return fmt.Sprintf("Working - %02dh %02dm %02ds", seconds/3600, seconds/60%60, seconds%60)
}

// Start replaces any previous request. The time values supplied by time.Now retain monotonic time.
func (t *Timer) Start(update func(string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopLocked()
	start := time.Now()
	t.stop = make(chan struct{})
	t.done = make(chan struct{})
	stop, done := t.stop, t.done
	update(Format(0))
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				update(Format(now.Sub(start)))
			}
		}
	}()
}

// Stop ends the request and clears its display. Repeated calls are safe.
func (t *Timer) Stop(update func(string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopLocked()
	update("")
}

func (t *Timer) stopLocked() {
	if t.stop == nil {
		return
	}
	close(t.stop)
	<-t.done
	t.stop, t.done = nil, nil
}
