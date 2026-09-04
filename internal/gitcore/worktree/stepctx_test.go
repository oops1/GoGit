package worktree

import (
	"context"
	"sync/atomic"
)

type stepContext struct {
	context.Context
	remaining atomic.Int64
}

func newStepContext(steps int64) *stepContext {
	ctx := &stepContext{Context: context.Background()}
	ctx.remaining.Store(steps)
	return ctx
}

func (c *stepContext) Err() error {
	if c.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}
