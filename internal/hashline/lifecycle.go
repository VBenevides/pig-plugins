package hashline

import "context"

// Invalidate revokes authority without waiting for an active tool or queue slot.
// Active requests observe revocation at their next pre-commit checkpoint.
func (engine *Engine) Invalidate() { engine.epoch.Add(1) }

type lifecycleCancellation struct {
	Cancellation
	engine     *Engine
	generation uint64
}

func (request lifecycleCancellation) Err() error {
	if err := request.Cancellation.Err(); err != nil {
		return err
	}
	if request.engine.epoch.Load() != request.generation {
		return context.Canceled
	}
	return nil
}
