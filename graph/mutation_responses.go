// Package graph provides request/response types for the NATS mutation and query APIs.
// See mutation_requests.go for the full package documentation.
package graph

import (
	"github.com/c360studio/semstreams/message"
)

// Mutation Response Types

// MutationResponse is the base SUCCESS body for all mutations.
//
// ADR-060: a mutation reply is EITHER a success body (this type, with a nil
// Go error) OR a single typed error value (*errs.ClassifiedError, carrying the
// wire Class + Code) — never both. The in-body error signalling
// (Success/Error/ErrorCode) was removed; the failure path no longer returns a
// body. A caller branches on the `err` from RequestClassified /
// RequestWithRetryClassified and reaches the machine code via
// errors.As(err, &ce) → ce.Code (the ErrorCode* values below) and the
// control-flow sentinel via errors.Is(err, errs.ErrRevisionMismatch).
//
// Two SUCCESS shapes for entity-mutation handlers (#120):
//
//   - Degraded=false → write committed, payload fully populated (Entity,
//     KVRevision, Version, TriplesAdded all authoritative). The response is
//     the post-write source of truth.
//
//   - Degraded=true → write committed durably, but the post-write read-back
//     failed (context cancellation, JetStream node failover, bucket re-keyed).
//     Entity may be nil and KVRevision may be 0; DegradedReason carries the
//     read-back failure reason. **Callers MUST NOT retry** — a retry on create
//     returns entity_already_exists (the entity is there), and on update a CAS
//     mismatch (the revision moved). Either re-read through a separate read
//     path or accept the write-without-echo and continue. Gateways SHOULD
//     return 200 OK with the Degraded flag echoed; NOT 202 — the write is
//     COMMITTED, not pending.
//
// Partial-batch success is also a success body, not an error: see
// AddTriplesBatchResponse (FailedSubjects is the partial signal).
//
// Degraded is scoped to COMMITTED writes ONLY, on every handler. A failure and
// a no-op are neither, and flagging either one degraded is unsafe rather than
// merely imprecise: pkg/projection's AppendEvidence branches on Degraded BEFORE
// it inspects FailedSubjects, so a degraded-flagged failure enters committed
// verification and can be reported as committed.
//
// Triple-mutation handlers (AddTriple/RemoveTriple/AddTriplesBatch) therefore
// resolve KVRevision from whether the write committed:
//
//   - COMMITTED → the exact revision that write's own CAS produced. NOT a
//     value re-read afterwards: another writer can commit in between, and a
//     caller attributing the re-read revision to itself is then holding
//     someone else's. Never degraded on this path unless the commit somehow
//     yielded no revision at all.
//   - SUPPRESSED as a duplicate, or a removal that matched nothing → the
//     entity's live unchanged revision, so a read-your-writes check usually
//     still resolves. That live read can itself fail, in which case KVRevision
//     is 0: a no-op has no committed write to fall back on, and reporting it as
//     degraded would re-open the failure above. A caller seeing Deduplicated or
//     Removed together with KVRevision 0 therefore has NO read-your-writes
//     anchor and must read authoritative state if it needs one. Never degraded
//     on this path: nothing committed, so there is no echo to fail, and
//     Deduplicated / Removed already tell the caller not to claim the revision.
//   - FAILED → no revision, not degraded.
//   - A multi-subject AddTriplesBatch → no revision, not degraded: a batch
//     spanning entities has no single entity revision to report.
type MutationResponse struct {
	// Degraded is true when the write COMMITTED but the post-write read-back
	// failed. It is never set for a failure or a no-op. See the type docstring
	// for the full contract. Omitted in JSON when false.
	Degraded bool `json:"degraded,omitempty"`
	// DegradedReason carries the read-back failure reason when Degraded is
	// true (ADR-060: replaces the retired Error field on the degraded path).
	DegradedReason string `json:"degraded_reason,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	Timestamp      int64  `json:"timestamp"`             // Unix nano timestamp
	KVRevision     uint64 `json:"kv_revision,omitempty"` // KV bucket revision after write
}

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
	// entity ID that already exists. Used by
	// CreateEntityWithTriples to surface create-or-fail conflicts.
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

	// ErrorCodeInternal is the catch-all for handler-internal
	// failures (KV transport errors, unmarshal failures on stored
	// state, etc.). Callers may retry as appropriate.
	ErrorCodeInternal = "internal"

	// ErrorCodeOwnerLeaseStale indicates the request's OwnerToken does not
	// match the live owner recorded in the owner registry for the contested
	// (entity, predicate) cell — the writer is either a different process or
	// a revived-stale incarnation of the same owner id. Callers should NOT
	// retry without resolving the ownership conflict. Emitted by graph-ingest's
	// create_with_triples / update_with_triples handlers ONLY when the
	// enforce_owner_lease config is set (ADR-056 PR-5). The default observe-only
	// posture (PR-3) meters owner_lease_mismatch_total + Warn-logs the mismatch
	// and commits the write instead.
	ErrorCodeOwnerLeaseStale = "owner_lease_stale"

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

// CreateEntityResponse response for entity creation
type CreateEntityResponse struct {
	MutationResponse
	Entity *EntityState `json:"entity,omitempty"`
}

// UpdateEntityResponse response for entity update
type UpdateEntityResponse struct {
	MutationResponse
	Entity  *EntityState `json:"entity,omitempty"`
	Version int64        `json:"version,omitempty"`
}

// DeleteEntityResponse response for entity deletion
type DeleteEntityResponse struct {
	MutationResponse
	Deleted bool `json:"deleted"`
}

// CreateEntityWithTriplesResponse response for atomic entity+triples creation
type CreateEntityWithTriplesResponse struct {
	MutationResponse
	Entity       *EntityState `json:"entity,omitempty"`
	TriplesAdded int          `json:"triples_added"`
}

// UpdateEntityWithTriplesResponse response for atomic entity+triples update
type UpdateEntityWithTriplesResponse struct {
	MutationResponse
	Entity         *EntityState `json:"entity,omitempty"`
	TriplesAdded   int          `json:"triples_added"`
	TriplesRemoved int          `json:"triples_removed"`
	Version        int64        `json:"version,omitempty"`
}

// AddTripleResponse response for triple addition.
//
// Deduplicated=true means the entity already carried an identical six-field
// tuple (subject, predicate, object, datatype, source, context), so NOTHING was
// committed: the KV revision, entity version, and update timestamp are
// unchanged and no ENTITY_STATES watcher fired. It is a success, not a failure
// — a producer that re-derives its facts on restart is expected to hit it.
//
// KVRevision follows the rule in MutationResponse: on a COMMITTED add it is the
// revision this write's own CAS produced, and on a suppressed one it is the
// entity's live revision — which may be 0 if that live read failed. A caller
// that sees Deduplicated with KVRevision 0 has NO read-your-writes anchor from
// this response and must read authoritative state if it needs one. A caller
// tracking its own writes must not record the revision at all when
// Deduplicated is true: nothing was written, so the value belongs to whoever
// wrote last.
type AddTripleResponse struct {
	MutationResponse
	Triple       *message.Triple `json:"triple,omitempty"`
	Deduplicated bool            `json:"deduplicated,omitempty"`
}

// AddTriplesBatchResponse response for batched triple addition.
//
// ADR-060: a WHOLE-batch failure (nothing committed) is a typed error on the
// err channel, not a body. This body is returned (with a nil Go error) for full
// success AND for PARTIAL success — FailedSubjects is the partial signal:
//
//   - FailedSubjects empty → every requested subject was processed without
//     failure. WrittenCount may be ZERO on that path: the add lane suppresses a
//     triple whose six-field tuple the entity already carries, so an entirely
//     duplicate batch commits nothing and reports (WrittenCount 0, no failed
//     subjects, no error). Read Deduplicated to tell that apart from an empty
//     request — WrittenCount + Deduplicated accounts for every triple whose
//     subject committed. A suppressed subject NEVER appears in FailedSubjects.
//   - FailedSubjects non-empty → PARTIAL success: per-entity atomicity means
//     subjects NOT in FailedSubjects DID commit (durable); subjects in
//     FailedSubjects rolled back. The caller retries just the failed subset.
//     This is a success body (nil err), NOT a Go error — turning partial
//     success into an error would make a committed write look retryable.
//
// FailedSubjects maps the failing entity IDs to their per-subject error
// message. WrittenCount is the number of NEWLY APPENDED triples across all
// entities, excluding every suppressed duplicate.
type AddTriplesBatchResponse struct {
	MutationResponse
	WrittenCount int `json:"written_count"`
	// Deduplicated counts submitted triples that were NOT appended because the
	// target entity already carried an identical six-field tuple (subject,
	// predicate, object, datatype, source, context), or because the same tuple
	// appeared earlier in this same request. It exists so a caller can
	// distinguish "already present" from "nothing happened" without paying an
	// authoritative read-back.
	//
	// This is a COUNT, not a no-op flag — it is non-zero on batches that also
	// committed writes (a mixed batch reports written=1, deduplicated=2). The
	// "nothing committed" predicate for this response is
	// `WrittenCount == 0 && len(FailedSubjects) == 0`. Do NOT pattern-match it
	// against AddTripleResponse.Deduplicated, which IS a boolean no-op flag;
	// gating on `Deduplicated > 0` here reintroduces the revision-claiming bug
	// that flag exists to prevent.
	Deduplicated   int               `json:"deduplicated,omitempty"`
	FailedSubjects map[string]string `json:"failed_subjects,omitempty"`
}

// RemoveTripleResponse response for triple removal.
//
// Removed reports whether a write was actually COMMITTED. False means the call
// was a true no-op — the entity was absent, or no stored triple carried the
// predicate — so no revision advanced and no watcher fired. It is still a
// success: removal is idempotent.
//
// A caller that attributes KVRevision to its own write MUST consult Removed
// first. On a no-op the reported revision is whatever the last OTHER writer
// produced, and claiming it as your own (the rule engine's per-rule
// feedback-loop tracker does exactly this) makes you drop that writer's
// genuine change. That no-op revision is a live read, which can itself fail and
// leave KVRevision 0 — so Removed=false with KVRevision 0 carries no
// read-your-writes anchor either; read authoritative state if you need one.
type RemoveTripleResponse struct {
	MutationResponse
	Removed bool `json:"removed"`
}
