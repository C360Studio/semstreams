package querymanager

import (
	stderrors "errors"
	"fmt"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

// Local error types specific to query manager
// (Using shared sentinel errors from graph package for common cases)
var (

	// Cache-specific errors (local to query manager)
	ErrCacheUnavailable = fmt.Errorf("cache unavailable")
	ErrCacheCorrupted   = fmt.Errorf("cache corrupted")
	ErrCacheTimeout     = fmt.Errorf("cache timeout")

	// KV-specific errors
	ErrKVUnavailable = fmt.Errorf("KV bucket unavailable")
	ErrKVTimeout     = fmt.Errorf("KV operation timeout")

	// Query-specific errors (ones not in shared graph package)
	ErrQueryResultsLimit = fmt.Errorf("query results limit exceeded")

	// Cache invalidation errors
	ErrInvalidationFailed = fmt.Errorf("cache invalidation failed")
	ErrWatcherFailed      = fmt.Errorf("KV watcher failed")

	// Configuration errors
	ErrInvalidConfig     = fmt.Errorf("invalid configuration")
	ErrMissingDependency = fmt.Errorf("missing required dependency")

	// Index manager dependency errors
	ErrIndexManagerUnavailable = fmt.Errorf("index manager unavailable")
	ErrIndexManagerTimeout     = fmt.Errorf("index manager timeout")
)

// All wrapper functions removed - use direct errors.Wrap* calls instead
// This ensures consistency across all managers and reduces code bloat

// IsEntityNotFound checks if an error is an entity not found error
func IsEntityNotFound(err error) bool {
	return errors.IsInvalid(err) && (stderrors.Is(err, gtypes.ErrEntityNotFound) ||
		stderrors.Is(err, gtypes.ErrAliasNotFound))
}

// IsCacheError checks if an error is a cache-related error
func IsCacheError(err error) bool {
	return stderrors.Is(err, ErrCacheUnavailable) ||
		stderrors.Is(err, ErrCacheCorrupted) ||
		stderrors.Is(err, ErrCacheTimeout)
}

// IsKVError checks if an error is a KV-related error
func IsKVError(err error) bool {
	return stderrors.Is(err, ErrKVUnavailable) ||
		stderrors.Is(err, ErrKVTimeout)
}

// IsQueryError checks if an error is a query-related error
func IsQueryError(err error) bool {
	return stderrors.Is(err, gtypes.ErrQueryTooComplex) ||
		stderrors.Is(err, gtypes.ErrQueryTimeout) ||
		stderrors.Is(err, gtypes.ErrQueryDepthExceeded) ||
		stderrors.Is(err, ErrQueryResultsLimit)
}

// IsRetryable checks if an error should be retried
func IsRetryable(err error) bool {
	return errors.IsTransient(err) && !IsQueryError(err)
}

// IsTimeout checks if an error is a timeout error
func IsTimeout(err error) bool {
	return stderrors.Is(err, ErrCacheTimeout) ||
		stderrors.Is(err, ErrKVTimeout) ||
		stderrors.Is(err, gtypes.ErrQueryTimeout) ||
		stderrors.Is(err, ErrIndexManagerTimeout)
}
