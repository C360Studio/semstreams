package projection

//revive:disable:max-public-structs The public mutation API uses explicit request, receipt, and typed-error structs.

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
)

// MutationClientConfig binds one immutable mutation client to an owner and its
// complete projection-contract set.
type MutationClientConfig struct {
	NATS        *natsclient.Client
	Registry    *ownership.Registry
	Heartbeater *ownership.Heartbeater
	Owner       string
	Contracts   []Contract
	Timeout     time.Duration
	Retry       natsclient.RetryConfig
}

// MutationMetadata carries stable correlation and provenance for one logical
// mutation across every transport attempt and authoritative verification.
type MutationMetadata struct {
	RequestID string
	TraceID   string
	Source    string
	Timestamp time.Time
}

// CreateMutation creates one authoritative entity with its complete initial
// projection facts.
type CreateMutation struct {
	Contract string
	Entity   *graph.EntityState
	Triples  []message.Triple
	Metadata MutationMetadata
}

// ReplaceOwnedMutation reconciles the complete replace-owned predicate set
// declared by one contract.
type ReplaceOwnedMutation struct {
	Contract string
	Group    string
	EntityID string
	Desired  []message.Triple
	Metadata MutationMetadata
}

// AppendEvidenceMutation appends evidence for exactly one existing entity.
type AppendEvidenceMutation struct {
	Contract string
	EntityID string
	Evidence []message.Triple
	Metadata MutationMetadata
}

// CommitState reports what the client can prove about the authoritative write.
type CommitState string

// Commit states describe progressively stronger knowledge about whether the
// authoritative mutation was applied.
const (
	CommitNotCommitted CommitState = "not-committed"
	CommitUnknown      CommitState = "unknown"
	CommitCommitted    CommitState = "committed"
	CommitVerified     CommitState = "verified"
)

// MutationReceipt is returned on both successful and commit-aware error paths.
type MutationReceipt struct {
	Entity     *graph.EntityState
	KVRevision uint64
	Commit     CommitState
	Degraded   bool
}

// MutationOperation identifies the public capability that produced an outcome.
type MutationOperation string

// Mutation operations identify each public mutation-client capability.
const (
	MutationOperationBind              MutationOperation = "bind"
	MutationOperationCreate            MutationOperation = "create-with-triples"
	MutationOperationReplaceOwned      MutationOperation = "replace-owned"
	MutationOperationAppendEvidence    MutationOperation = "append-evidence"
	MutationOperationDelete            MutationOperation = "delete"
	MutationOperationReadAuthoritative MutationOperation = "read-authoritative"
)

// MutationErrorKind is the stable caller-facing mutation failure taxonomy.
type MutationErrorKind string

// Mutation error kinds form the stable caller-facing failure taxonomy.
const (
	MutationInvalid             MutationErrorKind = "invalid"
	MutationNotFound            MutationErrorKind = "not-found"
	MutationConflict            MutationErrorKind = "conflict"
	MutationRevisionConflict    MutationErrorKind = "revision-conflict"
	MutationStaleOwnerToken     MutationErrorKind = "stale-owner-token"
	MutationUnavailable         MutationErrorKind = "unavailable"
	MutationCommitUnknown       MutationErrorKind = "commit-unknown"
	MutationCommittedUnverified MutationErrorKind = "committed-unverified"
	MutationInternal            MutationErrorKind = "internal"
)

// MutationError preserves the existing classified or sentinel cause while
// adding operation and commit knowledge.
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
	if e.Err != nil {
		return fmt.Sprintf("projection mutation %s failed (%s, %s): %v", e.Operation, e.Kind, e.Commit, e.Err)
	}
	return fmt.Sprintf("projection mutation %s failed (%s, %s)", e.Operation, e.Kind, e.Commit)
}

// Unwrap preserves existing errors.As and errors.Is behavior.
func (e *MutationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// EntityCreator is the least-privilege atomic entity-birth capability.
type EntityCreator interface {
	CreateWithTriples(context.Context, CreateMutation) (MutationReceipt, error)
}

// OwnedReplacer is the least-privilege owned-state reconciliation capability.
type OwnedReplacer interface {
	ReplaceOwned(context.Context, ReplaceOwnedMutation) (MutationReceipt, error)
}

// EvidenceAppender is the least-privilege append-only evidence capability.
type EvidenceAppender interface {
	AppendEvidence(context.Context, AppendEvidenceMutation) (MutationReceipt, error)
}

// AuthoritativeReader is the least-privilege graph-ingest source-of-truth read.
type AuthoritativeReader interface {
	ReadAuthoritative(context.Context, string) (*graph.ExactEntity, error)
}

//revive:enable:max-public-structs
