package contextprune

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// SkillMaxAge reads the optional expiry for explicitly released invocations.
// Zero disables expiry. Protected and unknown invocations never expire.
func SkillMaxAge(file string) (int, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read skill expiry: %w", err)
	}
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return 0, fmt.Errorf("read skill expiry: %w", err)
	}
	raw, exists := object["skillMaxAge"]
	if !exists {
		return 0, nil
	}
	number, ok := raw.(json.Number)
	if !ok {
		return 0, fmt.Errorf("skillMaxAge must be an integer from 0 to 10000")
	}
	age, err := number.Int64()
	if err != nil || age < 0 || age > 10000 {
		return 0, fmt.Errorf("skillMaxAge must be an integer from 0 to 10000")
	}
	return int(age), nil
}

// SetSkillMaxAge persists expiry without changing the pruning switch or other keys.
func SetSkillMaxAge(file string, age int) error {
	if age < 0 || age > 10000 {
		return fmt.Errorf("skill expiry must be from 0 to 10000")
	}
	object := map[string]any{}
	data, err := os.ReadFile(file)
	if err == nil {
		object, err = fsutil.DecodeObject(data)
	} else if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return fmt.Errorf("read skill expiry settings: %w", err)
	}
	object["skillMaxAge"] = age
	return fsutil.WriteJSONFileAtomic(file, object, 0o600)
}
