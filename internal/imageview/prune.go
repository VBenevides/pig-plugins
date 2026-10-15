package imageview

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	// MaxBlobAge is how long a stored preview survives without being used. Use refreshes it (see Touch).
	MaxBlobAge = 48 * time.Hour
	// MaxBlobBytes bounds the whole blob directory; the least recently used blobs go first.
	MaxBlobBytes int64 = 256 << 20
	// staleTempAge is how long an unpublished temporary file may exist before a crashed writer is assumed.
	staleTempAge = time.Hour
	// touchGranularity avoids a disk write on every redraw of a message that links the same blob.
	touchGranularity = time.Hour
)

var (
	blobName = regexp.MustCompile(`^[a-f0-9]{64}\.(?:png|jpg|gif|webp)$`)
	tempName = regexp.MustCompile(`^\.image-`)
)

// PruneResult reports what one Prune did.
type PruneResult struct {
	// Expired and OverCap count blobs removed for age and for the size cap; Temp counts abandoned temporary files.
	Expired, OverCap, Temp int
	// Kept is the number of blobs and KeptBytes their size after pruning.
	Kept      int
	KeptBytes int64
}

// Prune deletes stored previews that were not used for maxAge, then the least recently used ones until the
// directory is within maxBytes. A blob's last use is its modification time, which Store and Touch refresh. Only
// files that look like stored blobs, and unpublished temporary files older than an hour, are ever removed; other
// files and subdirectories are left alone. A missing directory is not an error. A file that cannot be removed is
// recorded in the returned error and the remaining files are still processed, so one failure neither stops the
// cleanup nor hides itself. Running it again after a partial failure finishes the job.
func Prune(root string, now time.Time, maxAge time.Duration, maxBytes int64) (PruneResult, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return PruneResult{}, nil
	}
	if err != nil {
		return PruneResult{}, fmt.Errorf("list image blobs in %s: %w", root, err)
	}
	type blob struct {
		path string
		size int64
		used time.Time
	}
	var result PruneResult
	var failures []error
	var live []blob
	remove := func(path string) bool {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove %s: %w", path, err))
			return false
		}
		return true
	}
	for _, entry := range entries {
		name := entry.Name()
		isBlob, isTemp := blobName.MatchString(name), tempName.MatchString(name)
		if !isBlob && !isTemp {
			continue
		}
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				failures = append(failures, fmt.Errorf("stat %s: %w", path, err))
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue // Never follow or delete a link or directory that only looks like a blob.
		}
		age := now.Sub(info.ModTime())
		switch {
		case isTemp:
			if age > staleTempAge && remove(path) {
				result.Temp++
			}
		case age > maxAge:
			if remove(path) {
				result.Expired++
			} else {
				live = append(live, blob{path, info.Size(), info.ModTime()})
			}
		default:
			live = append(live, blob{path, info.Size(), info.ModTime()})
		}
	}
	var total int64
	for _, b := range live {
		total += b.size
	}
	slices.SortFunc(live, func(a, b blob) int {
		if c := a.used.Compare(b.used); c != 0 {
			return c
		}
		return strings.Compare(a.path, b.path)
	})
	kept := live[:0:0]
	for _, b := range live {
		if total > maxBytes && remove(b.path) {
			total -= b.size
			result.OverCap++
			continue
		}
		kept = append(kept, b)
	}
	result.Kept, result.KeptBytes = len(kept), total
	return result, errors.Join(failures...)
}

// Touch marks a stored blob as used now, so a preview that is still being looked at does not expire. It changes
// only an existing regular blob file whose last use is more than an hour old, and ignores a missing blob: the
// reference may predate the blob directory or have been pruned.
func Touch(root, name string, now time.Time) error {
	if !blobName.MatchString(name) {
		return fmt.Errorf("not a stored image name: %q", name)
	}
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || now.Sub(info.ModTime()) < touchGranularity {
		return nil
	}
	if err := os.Chtimes(path, now, now); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("refresh %s: %w", path, err)
	}
	return nil
}
