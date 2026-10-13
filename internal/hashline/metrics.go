package hashline

import (
	"sync/atomic"
	"time"
)

// Metrics is an approximate concurrent snapshot, containing no paths, tokens,
// labels, source, or request payloads. Counters saturate instead of overflowing.
type Metrics struct {
	Successful           uint64
	Failed               uint64 // Returned errors, including already-committed failures.
	CommitFailures       uint64
	AdmissionNanoseconds uint64
	ReadBytes            uint64
	ResponseBytes        uint64
	Rejections           [8]uint64 // ErrorCategory constants in the order below.
}

var metricCategories = [...]ErrorCategory{CategoryInvalid, CategoryStale, CategoryConflict, CategoryPermission, CategoryConcurrent, CategoryFilesystem, CategoryCancelled, CategoryCommitted}

type engineMetrics struct {
	successful, rejected, committed, admission, readBytes, responseBytes atomic.Uint64
	categories                                                           [8]atomic.Uint64
}

func addMetric(counter *atomic.Uint64, amount uint64) {
	for {
		previous := counter.Load()
		next := previous + amount
		if next < previous {
			next = ^uint64(0)
		}
		if counter.CompareAndSwap(previous, next) {
			return
		}
	}
}
func (engine *Engine) Metrics() Metrics {
	metrics := &engine.metrics
	result := Metrics{Successful: metrics.successful.Load(), Failed: metrics.rejected.Load(), CommitFailures: metrics.committed.Load(), AdmissionNanoseconds: metrics.admission.Load(), ReadBytes: metrics.readBytes.Load(), ResponseBytes: metrics.responseBytes.Load()}
	for index := range result.Rejections {
		result.Rejections[index] = metrics.categories[index].Load()
	}
	return result
}
func (engine *Engine) observe(result Result, err error) {
	if err == nil {
		addMetric(&engine.metrics.successful, 1)
		addMetric(&engine.metrics.readBytes, uint64(len(result.readContent)))
		addMetric(&engine.metrics.responseBytes, uint64(len(result.Text)))
		return
	}
	addMetric(&engine.metrics.rejected, 1)
	category := ErrorCategoryOf(err)
	for index, candidate := range metricCategories {
		if category == candidate {
			addMetric(&engine.metrics.categories[index], 1)
			break
		}
	}
	if category == CategoryCommitted {
		addMetric(&engine.metrics.committed, 1)
	}
}
func (engine *Engine) observeAdmission(started time.Time) {
	addMetric(&engine.metrics.admission, uint64(time.Since(started)))
}
