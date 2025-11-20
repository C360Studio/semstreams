// Package graph provides shared types and error definitions for graph processing
package graph

import "errors"

// Sentinel errors for graph processing operations.
// These are wrapped with behavioral classification (Transient/Fatal/Invalid)
// when returned from managers.

// Entity errors
var (
	// ErrEntityNotFound indicates the requested entity does not exist
	ErrEntityNotFound = errors.New("entity not found")

	// ErrEntityExists indicates an entity already exists (for create operations)
	ErrEntityExists = errors.New("entity already exists")

	// ErrInvalidEntityID indicates the entity ID format is invalid
	ErrInvalidEntityID = errors.New("invalid entity ID")

	// ErrInvalidEntityData indicates the entity data is malformed
	ErrInvalidEntityData = errors.New("invalid entity data")

	// ErrVersionConflict indicates concurrent modification conflict
	ErrVersionConflict = errors.New("entity version conflict")
)

// Index errors
var (
	// ErrIndexNotFound indicates the requested index does not exist
	ErrIndexNotFound = errors.New("index not found")

	// ErrIndexCorrupted indicates index data is corrupted
	ErrIndexCorrupted = errors.New("index corrupted")

	// ErrIndexUpdateFailed indicates index update operation failed
	ErrIndexUpdateFailed = errors.New("index update failed")

	// ErrInvalidIndexKey indicates the index key format is invalid
	ErrInvalidIndexKey = errors.New("invalid index key")
)

// Query errors
var (
	// ErrQueryTimeout indicates query execution exceeded timeout
	ErrQueryTimeout = errors.New("query timeout")

	// ErrQueryTooComplex indicates query exceeds complexity limits
	ErrQueryTooComplex = errors.New("query too complex")

	// ErrQueryDepthExceeded indicates traversal depth limit exceeded
	ErrQueryDepthExceeded = errors.New("query depth exceeded")

	// ErrInvalidQueryParams indicates query parameters are invalid
	ErrInvalidQueryParams = errors.New("invalid query parameters")
)

// Alias errors
var (
	// ErrAliasNotFound indicates the requested alias does not exist
	ErrAliasNotFound = errors.New("alias not found")

	// ErrAliasExists indicates an alias already exists
	ErrAliasExists = errors.New("alias already exists")

	// ErrInvalidAlias indicates the alias format is invalid
	ErrInvalidAlias = errors.New("invalid alias")
)

// Buffer/batch errors
var (
	// ErrBufferFull indicates write buffer is at capacity
	ErrBufferFull = errors.New("buffer full")

	// ErrBatchTooBig indicates batch size exceeds limits
	ErrBatchTooBig = errors.New("batch too big")

	// ErrFlushFailed indicates buffer flush operation failed
	ErrFlushFailed = errors.New("flush failed")
)

// Service lifecycle errors
var (
	// ErrNotStarted indicates service is not started
	ErrNotStarted = errors.New("service not started")

	// ErrAlreadyStarted indicates service is already started
	ErrAlreadyStarted = errors.New("service already started")

	// ErrShuttingDown indicates service is shutting down
	ErrShuttingDown = errors.New("service shutting down")
)
