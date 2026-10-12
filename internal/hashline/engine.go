package hashline

import (
	"errors"
	"fmt"
	"sort"
)

const (
	MaxSnapshotFiles = 64
	MaxSnapshotBytes = 32 * 1024 * 1024
)

type retainedRead struct {
	content  []byte
	version  string
	coverage [][2]int
}

// Engine owns bounded read authority. Construct with NewEngine; do not copy it.
// The low-level stateless functions remain available for trusted internal callers.
type Engine struct {
	ready              chan struct{}
	reads              map[string]retainedRead
	order              []string // Least recently read first; edits never refresh retention.
	bytes              int
	maxFiles, maxBytes int
	scope              string
}

func NewEngine() *Engine { return newEngine(MaxSnapshotFiles, MaxSnapshotBytes) }
func newEngine(files, bytes int) *Engine {
	engine := &Engine{ready: make(chan struct{}, 1), reads: make(map[string]retainedRead), maxFiles: files, maxBytes: bytes}
	engine.ready <- struct{}{}
	return engine
}

func (engine *Engine) enter(request Cancellation) (func(), error) {
	if err := request.Err(); err != nil {
		return nil, err
	}
	select {
	case <-request.Done():
		return nil, request.Err()
	case <-engine.ready:
	}
	if err := request.Err(); err != nil {
		engine.ready <- struct{}{}
		return nil, err
	}
	scope := ""
	if scoped, ok := request.(interface{ ReadScope() string }); ok {
		scope = scoped.ReadScope()
	}
	if engine.scope != scope {
		engine.reads = make(map[string]retainedRead)
		engine.order = nil
		engine.bytes = 0
		engine.scope = scope
	}
	return func() { engine.ready <- struct{}{} }, nil
}

func snapshotCost(read retainedRead) int {
	return len(read.content) + len(read.coverage)*16 + len(read.version)
}
func (engine *Engine) forget(path string) {
	if old, ok := engine.reads[path]; ok {
		engine.bytes -= snapshotCost(old)
		delete(engine.reads, path)
	}
	for index, key := range engine.order {
		if key == path {
			engine.order = append(engine.order[:index], engine.order[index+1:]...)
			break
		}
	}
}

func mergeCoverage(previous, current [][2]int) [][2]int {
	ranges := append(append([][2]int(nil), previous...), current...)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	merged := make([][2]int, 0, len(ranges))
	for _, region := range ranges {
		if len(merged) > 0 && region[0] <= merged[len(merged)-1][1]+1 {
			if region[1] > merged[len(merged)-1][1] {
				merged[len(merged)-1][1] = region[1]
			}
		} else {
			merged = append(merged, region)
		}
	}
	return merged
}

func (engine *Engine) retain(path string, result Result) {
	version, ok := result.Details["version"].(string)
	if !ok {
		engine.forget(path)
		return
	}
	coverage, _ := result.Details["fullyDisplayedRanges"].([][2]int)
	if old, ok := engine.reads[path]; ok && old.version == version {
		coverage = mergeCoverage(old.coverage, coverage)
	} else {
		coverage = mergeCoverage(nil, coverage)
	}
	engine.forget(path)
	read := retainedRead{content: result.readContent, version: version, coverage: coverage}
	cost := snapshotCost(read)
	if cost > engine.maxBytes || engine.maxFiles < 1 {
		return
	}
	for len(engine.order) > 0 && (len(engine.reads) >= engine.maxFiles || engine.bytes+cost > engine.maxBytes) {
		engine.forget(engine.order[0])
	}
	engine.reads[path] = read
	engine.order = append(engine.order, path)
	engine.bytes += cost
}

func (engine *Engine) ReadContext(request Cancellation, cwd string, params map[string]any) (Result, error) {
	release, err := engine.enter(request)
	if err != nil {
		return Result{}, err
	}
	defer release()
	path, _ := params["path"].(string)
	if path == "" {
		return ReadContext(request, cwd, params)
	}
	target, err := canonicalMutationPath(ResolveToolPath(path, cwd))
	if err != nil {
		return Result{}, err
	}
	unlock, err := lockPathContext(request, target)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	result, err := ReadContext(request, cwd, params)
	if err == nil {
		engine.retain(target, result)
	}
	return result, err
}

func (engine *Engine) EditContext(request Cancellation, cwd string, params map[string]any) (Result, error) {
	release, err := engine.enter(request)
	if err != nil {
		return Result{}, err
	}
	defer release()
	path, _ := params["path"].(string)
	target, err := canonicalMutationPath(ResolveToolPath(path, cwd))
	if err != nil {
		return Result{}, err
	}
	read, ok := engine.reads[target]
	version, _ := params["version"].(string)
	if !ok || read.version != version {
		return Result{}, fmt.Errorf("edit rejected: %w", categorizedEditError(CategoryStale, "stale version or expired read authority for %s; read it again. Nothing was edited.", target))
	}
	if err := requireDisplayedEdits(request, read, params["edits"]); err != nil {
		return Result{}, fmt.Errorf("edit rejected: %w", err)
	}
	result, err := EditContext(request, cwd, params)
	var committed *CommittedError
	if err == nil && result.Details["changed"] == true || errors.As(err, &committed) {
		engine.forget(target)
	}
	return result, err
}

func (engine *Engine) WriteContext(request Cancellation, cwd string, params map[string]any) (Result, error) {
	release, err := engine.enter(request)
	if err != nil {
		return Result{}, err
	}
	defer release()
	result, err := WriteContext(request, cwd, params)
	var committed *CommittedError
	if err == nil && result.Details["changed"] == true || errors.As(err, &committed) {
		path, _ := params["path"].(string)
		target, resolveErr := canonicalMutationPath(ResolveToolPath(path, cwd))
		if resolveErr == nil {
			engine.forget(target)
		} else {
			engine.reads = make(map[string]retainedRead)
			engine.order = nil
			engine.bytes = 0
		}
	}
	return result, err
}
