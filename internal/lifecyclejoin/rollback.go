package lifecyclejoin

import (
	"context"
	"time"
)

const partialStartRollbackTimeout = 5 * time.Second

// RunPartialStartRollback synchronously undoes resources allocated by a failed
// Start under the one approved, bounded framework-owned root. No independent
// Stop caller exists for an uncommitted generation.
func RunPartialStartRollback(rollback func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), partialStartRollbackTimeout)
	defer cancel()
	if rollback == nil {
		return nil
	}
	return rollback(ctx)
}
