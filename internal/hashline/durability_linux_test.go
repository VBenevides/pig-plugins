//go:build linux

package hashline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectorySyncRunsAfterPublicationAndReportsCommittedFailure(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, failure := range []bool{false, true} {
			t.Run(map[bool]string{false: "create", true: "replace"}[existing]+map[bool]string{false: "-success", true: "-failure"}[failure], func(t *testing.T) {
				dir := realTempDir(t)
				path := filepath.Join(dir, "f.txt")
				if existing {
					writeFile(t, path, "original\n", 0751)
				}
				cause := errors.New("injected directory sync failure")
				calls := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				syncParent := func(directory string) error {
					calls++
					if directory != dir {
						t.Fatalf("wrong directory synced: %s", directory)
					}
					content, err := os.ReadFile(path)
					if err != nil || string(content) != "changed\n" {
						t.Fatalf("sync preceded publication: %v", err)
					}
					if len(tempFiles(t, dir)) != 0 {
						t.Fatal("sync preceded preparation cleanup")
					}
					cancel() // Post-publication cancellation must not bypass required syncing.
					if failure {
						return cause
					}
					return syncDirectory(directory)
				}
				var err error
				if existing {
					snapshot, snapshotErr := TakeSnapshot(path, dir, MaxEditBytes)
					if snapshotErr != nil {
						t.Fatal(snapshotErr)
					}
					err = replaceAtomicallyWithSync(ctx, snapshot, "changed\n", nil, syncParent)
				} else {
					err = createTextFileWithSync(ctx, path, "changed\n", syncParent)
				}
				if calls != 1 {
					t.Fatalf("directory sync called %d times", calls)
				}
				if failure {
					var committed *CommittedError
					if !errors.As(err, &committed) || !errors.Is(err, cause) || committed.Path != path || !strings.Contains(err.Error(), "was committed") {
						t.Fatalf("sync failure lost commit state or cause: %v", err)
					}
					if errors.Is(err, context.Canceled) {
						t.Fatal("committed failure was mislabeled cancellation")
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
