package datamanager

import (
	stderrors "errors"
	"fmt"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

// Local error types specific to data manager
// (Using shared sentinel errors from types/graph package for common cases)
var (
	// DataManager-specific errors (ones not in shared types/graph package)
	ErrInvalidOperation = fmt.Errorf("invalid operation")
	ErrBufferOverflow   = fmt.Errorf("write buffer overflow")
	ErrBatchChannelFull = fmt.Errorf("batch channel full")
)

// All wrapper functions removed - use direct errors.Wrap* calls instead
// This ensures consistency across all managers and reduces code bloat

// IsEntityError checks if an error is related to entity operations
func IsEntityError(err error) bool {
	return stderrors.Is(err, gtypes.ErrEntityNotFound) ||
		stderrors.Is(err, gtypes.ErrEntityExists) ||
		stderrors.Is(err, gtypes.ErrInvalidEntityID) ||
		stderrors.Is(err, gtypes.ErrInvalidEntityData) ||
		stderrors.Is(err, gtypes.ErrVersionConflict)
}

// IsBufferError checks if an error is related to buffer operations
func IsBufferError(err error) bool {
	return stderrors.Is(err, gtypes.ErrBufferFull) ||
		stderrors.Is(err, ErrBufferOverflow) ||
		stderrors.Is(err, ErrBatchChannelFull) ||
		stderrors.Is(err, gtypes.ErrFlushFailed)
}

// IsLifecycleError checks if an error is related to service lifecycle
func IsLifecycleError(err error) bool {
	return stderrors.Is(err, gtypes.ErrNotStarted) ||
		stderrors.Is(err, gtypes.ErrAlreadyStarted) ||
		stderrors.Is(err, gtypes.ErrShuttingDown)
}

// IsRetryableError determines if an error should be retried
// Using proper error type checking instead of string matching
func IsRetryableError(err error) bool {
	// Check if it's marked as transient
	if errors.IsTransient(err) {
		return true
	}

	// Check for specific retryable error types using errors.As
	var netErr interface{ Timeout() bool }
	if stderrors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Buffer errors are often temporary
	if IsBufferError(err) && !stderrors.Is(err, gtypes.ErrFlushFailed) {
		return true
	}

	return false
}
