package projection

//revive:disable:max-public-structs Explicit request and result structs keep the API legible.

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// MutationClientConfig configures one immutable contract-validating client.
type MutationClientConfig struct {
	NATS      *natsclient.Client
	Contracts []Contract
	Timeout   time.Duration
}

// MutationMetadata carries correlation and triple provenance. Create and Append
// require RequestID and Source. A caller retrying one logical Create or Append
// must set Timestamp once and reuse the complete Metadata; regenerating it would
// change tuple identity. Other fields remain operation-specific.
type MutationMetadata struct {
	RequestID string
	TraceID   string
	Source    string
	Timestamp time.Time
}

// CreateMutation requests strict creation of one entity.
type CreateMutation struct {
	Contract string
	Entity   *graph.EntityState
	Triples  []message.Triple
	Metadata MutationMetadata
}

// ReconcileMutation requests replacement of one declared predicate group.
type ReconcileMutation struct {
	Contract string
	Group    string
	EntityID string
	Desired  []message.Triple
	Metadata MutationMetadata
}

// AppendMutation requests set-valued addition to one declared predicate group.
type AppendMutation struct {
	Contract string
	Group    string
	EntityID string
	Triples  []message.Triple
	Metadata MutationMetadata
}

// DeleteMutation requests revision-fenced deletion of one entity.
type DeleteMutation struct {
	EntityID         string
	ExpectedRevision uint64
	Metadata         MutationMetadata
}

// CommitState reports what the client can prove about mutation commitment.
type CommitState string

const (
	// CommitNotCommitted proves the mutation did not commit.
	CommitNotCommitted CommitState = "not-committed"
	// CommitUnknown reports that delivery occurred without a valid reply.
	CommitUnknown CommitState = "unknown"
	// CommitVerified reports a valid authoritative mutation response.
	CommitVerified CommitState = "verified"
)

// MutationReceipt contains the authoritative result and commitment evidence.
type MutationReceipt struct {
	Entity     *graph.EntityState
	KVRevision uint64
	Commit     CommitState
}

// MutationOperation identifies the client operation that produced a result.
type MutationOperation string

const (
	// MutationOperationCreate identifies strict entity creation.
	MutationOperationCreate MutationOperation = "create"
	// MutationOperationReconcile identifies predicate-set reconciliation.
	MutationOperationReconcile MutationOperation = "reconcile"
	// MutationOperationAppend identifies set-valued triple addition.
	MutationOperationAppend MutationOperation = "append"
	// MutationOperationDelete identifies revision-fenced entity deletion.
	MutationOperationDelete MutationOperation = "delete"
	// MutationOperationReadAuthoritative identifies an exact authority read.
	MutationOperationReadAuthoritative MutationOperation = "read-authoritative"
)

// MutationErrorKind is the stable caller-facing mutation failure category.
type MutationErrorKind string

const (
	// MutationInvalid reports a request rejected before mutation.
	MutationInvalid MutationErrorKind = "invalid"
	// MutationNotFound reports a required entity that does not exist.
	MutationNotFound MutationErrorKind = "not-found"
	// MutationConflict reports strict-create or semantic conflict.
	MutationConflict MutationErrorKind = "conflict"
	// MutationRevisionConflict reports a stale expected authority revision.
	MutationRevisionConflict MutationErrorKind = "revision-conflict"
	// MutationUnavailable reports no available mutation responder.
	MutationUnavailable MutationErrorKind = "unavailable"
	// MutationCommitUnknown reports delivery without a valid response.
	MutationCommitUnknown MutationErrorKind = "commit-unknown"
	// MutationInternal reports an unclassified framework failure.
	MutationInternal MutationErrorKind = "internal"
)

// MutationError preserves operation, classification, and commitment evidence.
type MutationError struct {
	Operation MutationOperation
	Kind      MutationErrorKind
	Code      string
	Class     errs.ErrorClass
	Commit    CommitState
	Detail    map[string]any
	Err       error
}

func (e *MutationError) Error() string {
	if e == nil {
		return "projection mutation failed"
	}
	return fmt.Sprintf("projection mutation %s failed (%s, %s): %v", e.Operation, e.Kind, e.Commit, e.Err)
}

func (e *MutationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// EntityCreator creates one entity through the canonical mutation port.
type EntityCreator interface {
	Create(context.Context, CreateMutation) (MutationReceipt, error)
}

// PredicateReconciler reconciles one declared predicate group.
type PredicateReconciler interface {
	Reconcile(context.Context, ReconcileMutation) (MutationReceipt, error)
}

// TripleAppender appends set-valued triples to an existing entity.
type TripleAppender interface {
	Append(context.Context, AppendMutation) (MutationReceipt, error)
}

// EntityDeleter deletes one entity at an expected authority revision.
type EntityDeleter interface {
	Delete(context.Context, DeleteMutation) (MutationReceipt, error)
}

// AuthoritativeReader reads entity bytes and their same-entry KV revision.
type AuthoritativeReader interface {
	ReadAuthoritative(context.Context, string) (*graph.ExactEntity, error)
}

//revive:enable:max-public-structs
