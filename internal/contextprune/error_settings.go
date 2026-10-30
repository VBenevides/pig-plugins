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

// ResultPolicy reads the optional budget for old successful results.
func ResultPolicy(file string) (int, int, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultResultBudgetBytes, DefaultResultExcerptBytes, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read result policy %s: %w", file, err)
	}
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return 0, 0, fmt.Errorf("decode result policy %s: %w", file, err)
	}
	budget, excerpt, err := resultPolicyFromObject(object)
	if err != nil {
		return 0, 0, fmt.Errorf("result policy %s: %w", file, err)
	}
	return budget, excerpt, nil
}

func resultPolicyFromObject(object map[string]any) (int, int, error) {
	budget, excerpt := DefaultResultBudgetBytes, DefaultResultExcerptBytes
	for _, field := range []struct {
		key              string
		target           *int
		minimum, maximum int
	}{
		{"historicalResultBytes", &budget, minimumResultBudgetBytes, maximumResultBudgetBytes},
		{"resultExcerptBytes", &excerpt, 0, maxResultExcerptBytes},
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

// ArgumentBudget reads the optional budget for old edit and write payloads.
func ArgumentBudget(file string) (int, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultArgumentBudgetBytes, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read argument policy %s: %w", file, err)
	}
	object, err := fsutil.DecodeObject(data)
	if err != nil {
		return 0, fmt.Errorf("decode argument policy %s: %w", file, err)
	}
	budget, err := ArgumentPolicy(object)
	if err != nil {
		return 0, fmt.Errorf("argument policy %s: %w", file, err)
	}
	return budget, nil
}
