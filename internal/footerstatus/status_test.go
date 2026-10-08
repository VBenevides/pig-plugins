package footerstatus

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

type statusCall struct{ key, text string }
type statusHost struct{ calls []statusCall }

func (h *statusHost) SetStatus(key, text string) {
	h.calls = append(h.calls, statusCall{key, text})
}

func TestStoreForwardsPreservesAndDeletes(t *testing.T) {
	var store Store
	host := &statusHost{}
	badge := "\x1b[32mprimary model\x1b[39m"
	store.Set(host, "auto-model", badge)
	store.Set(host, "smart-approve-lancet", "strict - lancet off")
	snapshot := store.Snapshot()
	if got := snapshot["auto-model"]; got != badge {
		t.Fatalf("theme ANSI changed: %q", got)
	}
	snapshot["auto-model"] = "mutated"
	delete(snapshot, "smart-approve-lancet")
	if got := store.Snapshot(); got["auto-model"] != badge || got["smart-approve-lancet"] != "strict - lancet off" {
		t.Fatalf("snapshot mutation reached store: %v", got)
	}
	store.Set(host, "auto-model", "replacement")
	store.Set(host, "auto-model", "")
	store.Set(host, "missing", "")
	if got := store.Snapshot(); !reflect.DeepEqual(got, map[string]string{"smart-approve-lancet": "strict - lancet off"}) {
		t.Fatalf("delete did not preserve neighboring badge: %v", got)
	}
	want := []statusCall{{"auto-model", badge}, {"smart-approve-lancet", "strict - lancet off"}, {"auto-model", "replacement"}, {"auto-model", ""}, {"missing", ""}}
	if !reflect.DeepEqual(host.calls, want) {
		t.Fatalf("host calls = %#v, want %#v", host.calls, want)
	}
	var isolated Store
	if len(isolated.Snapshot()) != 0 {
		t.Fatal("independent store inherited badges")
	}
}

func TestStoreConcurrentWritersAndSnapshots(t *testing.T) {
	var store Store
	host := &statusHost{}
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			key := fmt.Sprint(i)
			for j := range 32 {
				store.Set(host, key, fmt.Sprint(j))
				snapshot := store.Snapshot()
				snapshot["local-only"] = "not stored"
			}
		}()
	}
	workers.Wait()
	snapshot := store.Snapshot()
	if len(snapshot) != 16 || len(host.calls) != 16*32 {
		t.Fatalf("lost updates: %d badges, %d host calls", len(snapshot), len(host.calls))
	}
	for key, text := range snapshot {
		if text != "31" {
			t.Errorf("badge %s = %q, want final value", key, text)
		}
	}
}
