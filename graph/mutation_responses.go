// Package graph provides request/response types for the NATS mutation and query APIs.
// See mutation_requests.go for the full package documentation.
package graph

// Stable failure codes carried by *errs.ClassifiedError.Code on the mutation
// error path (ADR-060). Reached caller-side via errors.As(err, &ce) → ce.Code;
// revision_mismatch additionally matches the errs.ErrRevisionMismatch sentinel
// via errors.Is. The set is closed — adding a value requires updating both this
// declaration and the graph-ingest handler that emits it.
const (
	// ErrorCodeRevisionMismatch indicates the request specified an
	// ExpectedRevision that didn't match the entity's current KV
	// revision at the time of write. Callers (notably
	// pkg/lifecycle.Manager.Transition) use this to drive
	// read-validate-retry loops.
	ErrorCodeRevisionMismatch = "revision_mismatch"

	// ErrorCodeEntityNotFound indicates the request targeted an
	// entity ID that does not exist in ENTITY_STATES. For
	// update-style requests this means the entity was never created
	// or was deleted concurrently.
	ErrorCodeEntityNotFound = "entity_not_found"

	// ErrorCodeEntityExists indicates a create-style request hit an
	// entity ID that already exists. Used by entity.create to surface
	// strict-create conflicts.
	ErrorCodeEntityExists = "entity_already_exists"

	// ErrorCodeInvalidRequest indicates the request envelope failed
	// pre-write validation (nil entity, malformed JSON, etc.).
	// Callers should not retry without fixing the request.
	ErrorCodeInvalidRequest = "invalid_request"

	// ErrorCodeStructuralInvalid indicates a token in the mutation violated the
	// structural-identity contract — an entity ID that is not exactly 6 parts, or a
	// predicate that is not exactly 3 parts (domain.category.property). The token is
	// malformed at its source; callers must fix the token, not retry. Emitted by the
	// graph-ingest structural gate when enforcement is on.
	ErrorCodeStructuralInvalid = "structural_invalid"

	// ErrorCodeMessageTypeUnregistered indicates an entity.create stamped a
	// message_type the receiving binary's payload registry does not hold
	// (ADR-103: the registry is the single type authority). Class invalid; the
	// caller registers the type, it does not retry. Detail carries the key under
	// "message_type". Emitted by graph-ingest's registered-type gate on both
	// create paths (the RPC lane and the in-process CreateEntity).
	ErrorCodeMessageTypeUnregistered = "message_type_unregistered"

	// ErrorCodeInternal is the catch-all for handler-internal
	// failures (KV transport errors, unmarshal failures on stored
	// state, etc.). Callers may retry as appropriate.
	ErrorCodeInternal = "internal"

	// ErrorCodeResourceExhausted indicates a query would exceed a server-side read
	// budget — e.g. a byName lookup on a name shared by more entities than the
	// hydration cap (gh#474 Codex P2a interim guard). The caller should narrow the
	// query rather than retry unchanged; the full hot-key NAME redesign is gh#381.
	ErrorCodeResourceExhausted = "resource_exhausted"

	// ErrorCodeIndexNotReady indicates a query arrived while the serving index was not
	// SOUND to read from — the responder or its watcher is unavailable, the index is
	// degraded by an unresolved required write, or it has not finished its initial
	// build. The motivating case is the breaking composite-key format cutover on an
	// in-place upgrade (gh#474 Codex P1d), where old aggregate keys are inert and the
	// new keyset is still materialising: serving it would advertise a smaller graph
	// than exists.
	//
	// NOT emitted for ordinary revision lag (narrowed by ADR-084). A healthy index that
	// is merely behind SERVES, reporting its view age as staleness_ms on the readiness
	// envelope, because coverage was never evidence of soundness and gating on it made
	// every write burst look like a fault (#592). Probe health with
	// `nats kv get GRAPH_STATUS graph-index` and read `state` + `bootstrap_complete` —
	// NOT the `ready` bit, which answers coverage and is false on any busy index.
	//
	// Callers retrying it should retry with backoff against the health question, and a
	// caller that needs "is MY write visible?" should not use this code at all: compare
	// the kv_revision its mutation returned against the envelope's indexed_revision,
	// which is the one sound per-entity check (ADR-084).
	ErrorCodeIndexNotReady = "index_not_ready"

	// ErrorCodeEmbeddingUnavailable indicates a similarity/embedding query targeted
	// a SOURCE entity that has no usable embedding to query FROM — the entity has no
	// embedding record, its embedding is not yet generated (status != generated), or
	// its stored vector is empty. It is a PER-ENTITY miss, NOT a producer-wide index
	// fault: the embedding index itself is sound (contrast ErrorCodeIndexNotReady),
	// this one source entity simply has no vector yet (e.g. an aggregation/group
	// entity never projected through the embedder). Class is ErrorInvalid.
	//
	// Emitted by graph-embedding's handleQuerySimilarNATS. The semantic-edge cache
	// (B2 §7) recognizes it as a definitive "this entity has no semantic neighbors"
	// and fails open to an empty set, distinct from a malformed / unknown reply which
	// it must count toward its coverage-threshold abort rather than cache as a hollow
	// empty (#662 / Codex P1#2). Consumers classify by this stable code, never by the
	// message text.
	ErrorCodeEmbeddingUnavailable = "embedding_unavailable"
)

// MutationOutcome is the closed graph-mutation server vocabulary. Transport
// uncertainty is classified by the typed requester and is never asserted by a
// server response.
type MutationOutcome string

const (
	// MutationApplied reports that the authority entry changed.
	MutationApplied MutationOutcome = "applied"
	// MutationUnchanged reports convergence without a new authority revision.
	MutationUnchanged MutationOutcome = "unchanged"
	// MutationEntityNotFound reports a missing required authority entry.
	MutationEntityNotFound MutationOutcome = "entity_not_found"
	// MutationEntityAlreadyExists reports strict-create conflict.
	MutationEntityAlreadyExists MutationOutcome = "entity_already_exists"
	// MutationRevisionMismatch reports a stale expected authority revision.
	MutationRevisionMismatch MutationOutcome = "revision_mismatch"
	// MutationInvalid reports a structurally invalid request.
	MutationInvalid MutationOutcome = "invalid"
	// MutationFailed reports a classified per-subject append failure.
	MutationFailed MutationOutcome = "failed"
)

// isServerMutationOutcome reports whether outcome belongs to the closed wire vocabulary.
func isServerMutationOutcome(outcome MutationOutcome) bool {
	switch outcome {
	case MutationApplied, MutationUnchanged,
		MutationEntityNotFound, MutationEntityAlreadyExists,
		MutationRevisionMismatch, MutationInvalid, MutationFailed:
		return true
	default:
		return false
	}
}

// CreateEntityResponse reports one atomic birth.
type CreateEntityResponse struct {
	Outcome    MutationOutcome `json:"outcome"`
	Entity     *EntityState    `json:"entity"`
	KVRevision uint64          `json:"kv_revision"`
	TraceID    string          `json:"trace_id,omitempty"`
	RequestID  string          `json:"request_id,omitempty"`
}

// ReconcilePredicatesResponse reports the exact current or committed entity.
type ReconcilePredicatesResponse struct {
	Outcome    MutationOutcome `json:"outcome"`
	Entity     *EntityState    `json:"entity"`
	KVRevision uint64          `json:"kv_revision"`
	TraceID    string          `json:"trace_id,omitempty"`
	RequestID  string          `json:"request_id,omitempty"`
}

// AppendSubjectResult reports one subject's independent append result. Outcome
// is the discriminator: failed requires Error; every other outcome forbids it.
// A failed result is definite; transport ambiguity is never encoded here.
type AppendSubjectResult struct {
	EntityID   string           `json:"entity_id"`
	Outcome    MutationOutcome  `json:"outcome"`
	KVRevision uint64           `json:"kv_revision,omitempty"`
	Error      *MutationFailure `json:"error,omitempty"`
}

// MutationFailure carries a definite classified subject-local failure without
// erasing receipts for other subjects that already committed.
type MutationFailure struct {
	Class string `json:"class"`
	Code  string `json:"code"`
}

// AppendTriplesResponse reports explicit partial results by distinct subject.
type AppendTriplesResponse struct {
	Results   []AppendSubjectResult `json:"results"`
	TraceID   string                `json:"trace_id,omitempty"`
	RequestID string                `json:"request_id,omitempty"`
}

// DeleteEntityResponse response for entity deletion
type DeleteEntityResponse struct {
	EntityID         string          `json:"entity_id"`
	Outcome          MutationOutcome `json:"outcome"`
	ExpectedRevision uint64          `json:"expected_revision"`
	TraceID          string          `json:"trace_id,omitempty"`
	RequestID        string          `json:"request_id,omitempty"`
}
