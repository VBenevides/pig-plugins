package contextprune

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	RecoveryRecordLimit = 2 << 20
	RecoveryScanLimit   = 64 << 20
	RecoveryPageLimit   = 16 << 10
)

// RecoveryRef binds a handle to one immutable message in the active session branch.
// It contains no copied payload and grants no source-editing authority.
type RecoveryRef struct {
	Handle     string `json:"handle"`
	EventID    string `json:"eventId"`
	Role       string `json:"role"`
	CallID     string `json:"callId"`
	Digest     string `json:"digest"`
	SourceHash string `json:"sourceHash,omitempty"`
	// ArgHashes is a sorted JSON object of call ID to argument hash. A string keeps the struct comparable.
	ArgHashes string `json:"argHashes,omitempty"`
}

type RecoveryPage struct {
	Handle  string `json:"handle"`
	SHA256  string `json:"sha256"`
	Offset  int    `json:"offset"`
	Next    int    `json:"next"`
	Total   int    `json:"total"`
	Content string `json:"content"`
}

// RecoveryReferences reuses the branch snapshot already read for skill lifetimes.
// The session path is an internal namespace, never an accepted tool argument.
func RecoveryReferences(session string, entries []map[string]any) map[string]RecoveryRef {
	refs := map[string]RecoveryRef{}
	if session == "" {
		return refs
	}
	for _, entry := range entries {
		message, _ := entry["message"].(map[string]any)
		id, _ := entry["id"].(string)
		ref, ok := recoveryReference(session, id, message)
		if ok {
			refs[ref.Handle] = ref
		}
	}
	return refs
}

func recoveryReference(session, eventID string, message map[string]any) (RecoveryRef, bool) {
	role, _ := message["role"].(string)
	if eventID == "" || len(eventID) > 1024 || (role != "toolResult" && role != "assistant" && role != "user") {
		return RecoveryRef{}, false
	}
	id := ""
	if role == "toolResult" {
		id, _ = message["toolCallId"].(string)
		if id == "" || len(id) > 1024 {
			return RecoveryRef{}, false
		}
	}
	data, err := json.Marshal(message)
	if err != nil || len(data) > RecoveryRecordLimit/2 {
		return RecoveryRef{}, false
	}
	digest := sha256.Sum256(data)
	full := hex.EncodeToString(digest[:])
	hash := sha256.Sum256([]byte(session + "\x00" + eventID + "\x00" + full))
	sourceHash := ""
	if role == "toolResult" {
		sourceHash, _ = ResultSourceHash(message)
	}
	argHashes := ""
	if role == "assistant" {
		argHashes = encodeArgumentHashes(argumentHashes(message))
	}
	return RecoveryRef{Handle: "cr1_" + hex.EncodeToString(hash[:8]), EventID: eventID, Role: role, CallID: id, Digest: full, SourceHash: sourceHash, ArgHashes: argHashes}, true
}

func RecoveryHandles(refs map[string]RecoveryRef) map[string]string {
	handles, _ := RecoveryMaps(refs)
	return handles
}

// RecoveryMaps returns only unambiguous handles and verified text/status hashes.
func RecoveryMaps(refs map[string]RecoveryRef) (map[string]string, map[string]string) {
	handles := map[string]string{}
	sources := map[string]string{}
	ambiguous := map[string]bool{}
	for handle, ref := range refs {
		if ref.CallID == "" || ambiguous[ref.CallID] {
			continue
		}
		if existing, ok := handles[ref.CallID]; ok && existing != handle {
			delete(handles, ref.CallID)
			delete(sources, ref.CallID)
			ambiguous[ref.CallID] = true
			continue
		}
		handles[ref.CallID] = handle
		if ref.SourceHash != "" {
			sources[ref.CallID] = ref.SourceHash
		}
	}
	return handles, sources
}

func decodeRecoveryRecord(line []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	if !utf8.Valid(line) {
		return nil, fmt.Errorf("recovery record is not valid UTF-8")
	}
	var entry map[string]any
	if err := decoder.Decode(&entry); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("recovery record has trailing data: %v", err)
	}
	return entry, nil
}

func openRecoverySession(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect recovery session: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > RecoveryScanLimit {
		return nil, fmt.Errorf("recovery requires a regular session file of at most %d bytes", RecoveryScanLimit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		closeErr := file.Close()
		return nil, fmt.Errorf("inspect opened recovery session: %w (close: %v)", err, closeErr)
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Size() > RecoveryScanLimit {
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close changed recovery session: %w", err)
		}
		return nil, fmt.Errorf("recovery session changed while opening")
	}
	return file, nil
}

// ReadRecoveryPage only reads the host-selected session. It never executes a tool.
// Limits apply to physical reads, individual records, and returned UTF-8 bytes.
func ReadRecoveryPage(session string, refs map[string]RecoveryRef, handle string, offset, limit int) (RecoveryPage, error) {
	ref, ok := refs[handle]
	if !ok || !strings.HasPrefix(handle, "cr1_") || len(handle) != 20 {
		return RecoveryPage{}, fmt.Errorf("unknown recovery handle in the active branch")
	}
	if offset < 0 || limit < 1 || limit > RecoveryPageLimit {
		return RecoveryPage{}, fmt.Errorf("recovery offset must be nonnegative and limit must be 1-%d bytes", RecoveryPageLimit)
	}
	file, err := openRecoverySession(session)
	if err != nil {
		return RecoveryPage{}, fmt.Errorf("open recovery session: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReaderSize(io.LimitReader(file, RecoveryScanLimit+1), RecoveryRecordLimit)
	consumed := 0
	for records := 0; records < 100000; records++ {
		line, err := reader.ReadSlice('\n')
		consumed += len(line)
		if consumed > RecoveryScanLimit {
			return RecoveryPage{}, fmt.Errorf("recovery scan exceeds %d bytes", RecoveryScanLimit)
		}
		if err != nil && err != io.EOF {
			return RecoveryPage{}, fmt.Errorf("read recovery record (limit %d bytes): %w", RecoveryRecordLimit, err)
		}
		if len(line) > 0 {
			entry, decodeErr := decodeRecoveryRecord(line)
			if decodeErr != nil {
				return RecoveryPage{}, fmt.Errorf("decode recovery record %d: %w", records+1, decodeErr)
			}
			message, _ := entry["message"].(map[string]any)
			if entry["id"] == ref.EventID {
				actual, valid := recoveryReference(session, ref.EventID, message)
				if !valid || actual != ref {
					return RecoveryPage{}, fmt.Errorf("recovery source changed for %s", handle)
				}
				// Return the exact saved event bytes, including metadata and error status.
				text := string(line)
				if offset > len(text) || (offset < len(text) && !utf8.RuneStart(text[offset])) {
					return RecoveryPage{}, fmt.Errorf("recovery offset is outside the record or splits a UTF-8 character")
				}
				end := min(len(text), offset+limit)
				for end > offset && end < len(text) && !utf8.RuneStart(text[end]) {
					end--
				}
				if end == offset && offset < len(text) {
					return RecoveryPage{}, fmt.Errorf("recovery limit is smaller than the next UTF-8 character")
				}
				hash := sha256.Sum256(line)
				next := end
				if next == len(text) {
					next = -1
				}
				return RecoveryPage{Handle: handle, SHA256: hex.EncodeToString(hash[:]), Offset: offset, Next: next, Total: len(text), Content: text[offset:end]}, nil
			}
		}
		if err == io.EOF {
			return RecoveryPage{}, fmt.Errorf("recovery source not found for %s", handle)
		}
	}
	return RecoveryPage{}, fmt.Errorf("recovery scan exceeds 100000 events")
}
