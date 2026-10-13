package hashline

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMetricsBoundedPayloadFreeAndConcurrent(t *testing.T) {
	dir := realTempDir(t)
	writeFile(t, filepath.Join(dir, "secret-path.txt"), "source-secret\n", 0600)
	engine := NewEngine()
	if _, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "secret-path.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "secret-path.txt", "version": "bad", "edits": []any{}}); err == nil {
		t.Fatal("expected rejection")
	}
	engine.observe(Result{}, &CommittedError{Path: "secret-path", Err: errors.New("payload-secret")})
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			for range 100 {
				engine.observe(Result{Text: "response-secret"}, nil)
				_ = engine.Metrics()
			}
		})
	}
	workers.Wait()
	metrics := engine.Metrics()
	if metrics.Successful != 1601 || metrics.Failed != 2 || metrics.CommitFailures != 1 || metrics.ReadBytes != 14 || metrics.AdmissionNanoseconds == 0 || metrics.Rejections[1] != 1 || metrics.Rejections[7] != 1 {
		t.Fatalf("incorrect metrics: %+v", metrics)
	}
	encoded, err := json.Marshal(metrics)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || len(encoded) > 1024 {
		t.Fatalf("unbounded or sensitive metrics: %s", encoded)
	}
	var counter atomic.Uint64
	counter.Store(math.MaxUint64 - 1)
	addMetric(&counter, 10)
	addMetric(&counter, 1)
	if counter.Load() != math.MaxUint64 {
		t.Fatal("counter overflowed")
	}
}
