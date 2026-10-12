package hashline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalMutationAdmissionIsBoundedAndReleased(t *testing.T) {
	dir := realTempDir(t)
	releases := make([]func(), 0, MaxMutationParticipants)
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for index := range MaxMutationParticipants {
		release, err := lockPathContext(context.Background(), filepath.Join(dir, fmt.Sprint(index)))
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	if release, err := lockPathContext(context.Background(), filepath.Join(dir, "extra")); err == nil {
		release()
		t.Fatal("global capacity did not reject overflow")
	}
	for _, release := range releases {
		release()
	}
	editLocks.Lock()
	remaining := editLocks.participants
	entries := len(editLocks.held)
	editLocks.Unlock()
	if remaining != 0 || entries != 0 {
		t.Fatalf("admission leaked state: %d participants, %d entries", remaining, entries)
	}
	release, err := lockPathContext(context.Background(), filepath.Join(dir, "extra"))
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestEngineAdmissionRejectsOverflowAndCancellationDoesNotLeak(t *testing.T) {
	engine := NewEngine()
	for range MaxPathParticipants {
		engine.slots <- struct{}{}
	}
	if release, err := engine.enter(context.Background()); err == nil {
		release()
		t.Fatal("engine admitted an unbounded waiter")
	} else if !strings.Contains(err.Error(), "capacity") {
		t.Fatal(err)
	}
	for range MaxPathParticipants {
		<-engine.slots
	}
	release, err := engine.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		unlock, err := engine.enter(ctx)
		if err == nil {
			unlock()
		}
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter cancellation lost: %v", err)
	}
	release()
	if len(engine.slots) != 0 {
		t.Fatal("cancelled request leaked admission")
	}
}
