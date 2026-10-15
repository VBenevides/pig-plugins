package hashline

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestConcurrentReadStatePublicationKeepsVersionsAndCoverageTogether(t *testing.T) {
	dir := realTempDir(t)
	engine := NewEngine()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "one\ntwo\n", 0644)
	var workers sync.WaitGroup
	failures := make(chan error, 16)
	for worker := range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for iteration := range 20 {
				if worker%3 == 0 {
					content := fmt.Sprintf("one\nvalue-%d-%d\n", worker, iteration)
					if _, err := engine.WriteContext(context.Background(), dir, map[string]any{"path": "f", "content": content}); err != nil {
						failures <- err
						return
					}
				} else {
					result, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f", "offset": worker%2 + 1, "limit": 1})
					if err != nil {
						failures <- err
						return
					}
					if result.Details["version"] != ContentVersion(result.readContent) || !reflect.DeepEqual(result.Details["fullyDisplayedRanges"], [][2]int{{worker%2 + 1, worker%2 + 1}}) {
						failures <- fmt.Errorf("mixed version/coverage publication")
						return
					}
				}
			}
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	result := engineRead(t, engine, dir, "f")
	read := engine.reads[path]
	if read.version != result.Details["version"] || !reflect.DeepEqual(read.coverage, [][2]int{{1, 2}}) || string(read.content) != string(result.readContent) {
		t.Fatal("retained state mixes versions or coverage")
	}
}

func TestSameVersionRangesMergeWithoutCrossVersionCoverage(t *testing.T) {
	dir := realTempDir(t)
	engine := NewEngine()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "one\ntwo\nthree\n", 0644)
	for _, offset := range []int{1, 3} {
		if _, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f", "offset": offset, "limit": 1}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(engine.reads[path].coverage, [][2]int{{1, 1}, {3, 3}}) {
		t.Fatal("same-version range coverage was not accumulated")
	}
	writeFile(t, path, "one\nchanged\nthree\n", 0644)
	if _, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f", "offset": 2, "limit": 1}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engine.reads[path].coverage, [][2]int{{2, 2}}) {
		t.Fatal("new version inherited old coverage")
	}
}
