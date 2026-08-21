// Package lifecyclecleanup contains the one stateless policy shared by
// lifecycle owners when Start fails after acquiring cleanup obligations.
package lifecyclecleanup

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const failedStartRollbackTimeout = 5 * time.Second

// RollbackFailedStart synchronously runs rollback after removing only the
// parent's cancellation and deadline. The fixed timeout keeps terminal cleanup
// bounded while preserving parent values.
func RollbackFailedStart(parent context.Context, rollback func(context.Context) error) error {
	return rollbackFailedStart(parent, failedStartRollbackTimeout, rollback)
}

func rollbackFailedStart(
	parent context.Context,
	budget time.Duration,
	rollback func(context.Context) error,
) error {
	if parent == nil {
		return fmt.Errorf("lifecyclecleanup: nil failed-Start parent")
	}
	if rollback == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)
	defer cancel()

	rollbackErr := rollback(ctx)
	return errors.Join(rollbackErr, ctx.Err())
}
