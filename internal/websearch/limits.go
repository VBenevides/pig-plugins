package websearch

import "errors"

// Native search normally returns tens of sources. Reject oversized metadata before quadratic deduplication.
func boundedEvent(value any) error {
	nodes := 0
	var visit func(any, int) error
	visit = func(v any, depth int) error {
		nodes++
		if nodes > 4096 || depth > 32 {
			return errors.New("provider search event exceeds metadata complexity limits")
		}
		switch it := v.(type) {
		case map[string]any:
			if len(it) > 256 {
				return errors.New("provider search object exceeds 256 fields")
			}
			for _, child := range it {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			if len(it) > 256 {
				return errors.New("provider search array exceeds 256 items")
			}
			for _, child := range it {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(value, 0)
}
