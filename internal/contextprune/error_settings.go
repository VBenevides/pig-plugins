package contextprune

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

func ErrorPolicy(file string) (int, int, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultErrorBudgetBytes, DefaultErrorExcerptBytes, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read error policy %s: %w", file, err)
	}
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return 0, 0, fmt.Errorf("decode error policy %s: %w", file, err)
	}
	budget, excerpt, err := errorPolicyFromObject(object)
	if err != nil {
		return 0, 0, fmt.Errorf("error policy %s: %w", file, err)
	}
	return budget, excerpt, nil
}

func errorPolicyFromObject(object map[string]any) (int, int, error) {
	budget, excerpt := DefaultErrorBudgetBytes, DefaultErrorExcerptBytes
	for _, field := range []struct {
		key              string
		target           *int
		minimum, maximum int
	}{
		{"historicalErrorBytes", &budget, 4096, 16 << 20}, {"errorExcerptBytes", &excerpt, minimumFailureBytes, RecoveryPageLimit},
	} {
		value, exists := object[field.key]
		if !exists {
			continue
		}
		number, ok := value.(json.Number)
		if !ok {
			return 0, 0, fmt.Errorf("%s must be an integer between %d and %d", field.key, field.minimum, field.maximum)
		}
		integer, err := number.Int64()
		if err != nil || integer < int64(field.minimum) || integer > int64(field.maximum) {
			return 0, 0, fmt.Errorf("%s must be an integer between %d and %d", field.key, field.minimum, field.maximum)
		}
		*field.target = int(integer)
	}
	return budget, excerpt, nil
}
