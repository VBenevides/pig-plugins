package guard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// AllowlistFileName is the file in the agent directory that keeps the "Always allow" answers.
const AllowlistFileName = "smart-approve-lancet-allow.json"

// AllowlistFile returns the allow list file path.
func AllowlistFile(getenv func(string) string) string {
	return agentdir.File(getenv, AllowlistFileName)
}

// Grant is one "Always allow" answer: this exact command, or this write, on this exact affected item.
type Grant struct {
	Tool    string `json:"tool"`
	Command string `json:"command"`
	Item    string `json:"item"`
}

type allowlistFile struct {
	Version int     `json:"version"`
	Allow   []Grant `json:"allow"`
}

// Allowlist stores grants on disk. It reads the file on every lookup, so a grant made in one session counts in the
// others. A missing file is an empty list; an unreadable or damaged file is an error and grants nothing.
type Allowlist struct {
	file string
	mu   sync.Mutex
}

// NewAllowlist returns the list kept in file.
func NewAllowlist(file string) *Allowlist { return &Allowlist{file: file} }

func (a *Allowlist) load() (allowlistFile, error) {
	data, err := os.ReadFile(a.file)
	if errors.Is(err, fs.ErrNotExist) {
		return allowlistFile{Version: 1}, nil
	}
	if err != nil {
		return allowlistFile{}, fmt.Errorf("read %s: %w", a.file, err)
	}
	var list allowlistFile
	if err := json.Unmarshal(data, &list); err != nil {
		return allowlistFile{}, fmt.Errorf("%s is invalid: %w", a.file, err)
	}
	list.Version = 1
	return list, nil
}

// Allows reports whether grant was granted before.
func (a *Allowlist) Allows(grant Grant) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	list, err := a.load()
	if err != nil {
		return false, err
	}
	for _, existing := range list.Allow {
		if existing == grant {
			return true, nil
		}
	}
	return false, nil
}

// Add records grant. It refuses to replace a file that cannot be read, and adding a known grant changes nothing.
// The write is atomic and the file mode is 0600.
func (a *Allowlist) Add(grant Grant) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	list, err := a.load()
	if err != nil {
		return err
	}
	for _, existing := range list.Allow {
		if existing == grant {
			return nil
		}
	}
	list.Allow = append(list.Allow, grant)
	return fsutil.WriteJSONFileAtomic(a.file, list, 0o600)
}
