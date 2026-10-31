package contextprune

import (
	"bufio"
	"fmt"
	"io"
)

// BranchRecoveryReferences reads bounded session data rather than fetching an
// unbounded SDK branch payload. Only ancestors of the current leaf are authorized.
func BranchRecoveryReferences(session, leaf string) (map[string]RecoveryRef, error) {
	file, err := openRecoverySession(session)
	if err != nil {
		return nil, fmt.Errorf("open recovery branch: %w", err)
	}
	defer file.Close()
	type event struct {
		parent string
		ref    RecoveryRef
	}
	events := map[string]event{}
	reader := bufio.NewReaderSize(io.LimitReader(file, RecoveryScanLimit+1), RecoveryRecordLimit)
	consumed := 0
	for records := 0; ; records++ {
		if records >= 100000 {
			return nil, fmt.Errorf("recovery branch exceeds 100000 events")
		}
		line, readErr := reader.ReadSlice('\n')
		consumed += len(line)
		if consumed > RecoveryScanLimit {
			return nil, fmt.Errorf("recovery branch exceeds %d bytes", RecoveryScanLimit)
		}
		if readErr != nil && readErr != io.EOF {
			return nil, fmt.Errorf("read recovery branch record: %w", readErr)
		}
		if len(line) > 0 {
			entry, err := decodeRecoveryRecord(line)
			if err != nil {
				return nil, fmt.Errorf("decode recovery branch record %d: %w", records+1, err)
			}
			id, _ := entry["id"].(string)
			if id != "" {
				if _, exists := events[id]; exists {
					return nil, fmt.Errorf("duplicate recovery event ID")
				}
				parent, _ := entry["parentId"].(string)
				message, _ := entry["message"].(map[string]any)
				ref, _ := recoveryReference(session, id, message)
				events[id] = event{parent: parent, ref: ref}
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	refs := map[string]RecoveryRef{}
	seen := map[string]bool{}
	for leaf != "" {
		if seen[leaf] {
			return nil, fmt.Errorf("cycle in recovery branch")
		}
		seen[leaf] = true
		event, ok := events[leaf]
		if !ok {
			return nil, fmt.Errorf("recovery branch event is not durable: %s", leaf)
		}
		if event.ref.Handle != "" {
			if previous, ok := refs[event.ref.Handle]; ok && previous != event.ref {
				return nil, fmt.Errorf("ambiguous recovery handle")
			}
			refs[event.ref.Handle] = event.ref
		}
		leaf = event.parent
	}
	return refs, nil
}
