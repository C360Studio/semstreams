// Package graphingest mutation handlers for triple and entity operations via NATS request/reply.
package graphingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/c360studio/semstreams/vocabulary"
)

const (
	// SubjectTripleAdd is the NATS subject for add triple requests
	SubjectTripleAdd = "graph.mutation.triple.add"
	// SubjectTripleAddBatch is the NATS subject for batched add-triple
	// requests. Optimised for tools that emit many triples in one call
	// (write_todos per ADR-036): triples sharing a Subject collapse to
	// one CAS round-trip on that entity.
	SubjectTripleAddBatch = "graph.mutation.triple.add_batch"
	// SubjectTripleRemove is the NATS subject for remove triple requests
	SubjectTripleRemove = "graph.mutation.triple.remove"

	// Entity-level mutation subjects. Triple-level subjects (above) treat
	// the entity as upsert-target — AddTriple's CAS path creates the
	// entity if it doesn't exist. The entity-level subjects below carry
	// stricter semantics so CS API gateways (semconnect) can map them to
	// HTTP create-or-conflict / must-exist / delete contracts without
	// app-side shims. See GH #98 + docs/operations/22-adr045-phase1-plan.md
	// (peer queue context).

	// SubjectEntityCreate is the NATS subject for create-or-fail entity
	// requests. Returns Success=false with "entity already exists" when
	// the entity ID is present — lets a CS API POST surface 409 Conflict
	// without a separate existence pre-check.
	SubjectEntityCreate = "graph.mutation.entity.create"

	// SubjectEntityCreateWithTriples is the NATS subject for atomic
	// entity+triples creation. Carries the full EntityState shape
	// (MessageType, Version, StorageRef) so provenance fields survive
	// — fields AddTriple synthesizes with defaults. Same create-or-fail
	// semantics as SubjectEntityCreate.
	SubjectEntityCreateWithTriples = "graph.mutation.entity.create_with_triples"

	// SubjectEntityUpdate is the NATS subject for must-exist entity
	// updates. Returns Success=false with "entity not found" when the
	// entity ID is absent — lets a CS API PUT/PATCH surface 404 without
	// silently upserting a new entity.
	SubjectEntityUpdate = "graph.mutation.entity.update"

	// SubjectEntityUpdateWithTriples is the NATS subject for must-exist
	// entity updates with triple-set deltas. AddTriples appends; the
	// RemoveTriples slice names predicates whose triples are removed.
	// PR-A applies the delta via read-modify-write (TOCTOU window
	// possible); PR-B will switch to a single CAS over the entity state
	// to close the partial-erasure window semconnect Stage 27 flagged.
	SubjectEntityUpdateWithTriples = "graph.mutation.entity.update_with_triples"

	// SubjectEntityDelete is the NATS subject for entity-scoped delete.
	// Idempotent — deleting a non-existent entity returns Success=true,
	// Deleted=false (the NATS KV Delete primitive is itself idempotent).
	SubjectEntityDelete = "graph.mutation.entity.delete"
)

// setupMutationHandlers registers NATS request handlers for triple mutations.
// These handlers allow the rule processor (and other components) to modify
// entity triples via NATS request/reply.
func (c *Component) setupMutationHandlers(ctx context.Context) error {
	sub, err := c.natsClient.SubscribeForRequests(ctx, SubjectTripleAdd, c.meteredMutation(SubjectTripleAdd, c.handleTripleAdd))
	if err != nil {
		return fmt.Errorf("subscribe triple add: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectTripleAddBatch, c.meteredMutation(SubjectTripleAddBatch, c.handleTripleAddBatch))
	if err != nil {
		return fmt.Errorf("subscribe triple add_batch: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectTripleRemove, c.meteredMutation(SubjectTripleRemove, c.handleTripleRemove))
	if err != nil {
		return fmt.Errorf("subscribe triple remove: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityCreate, c.meteredMutation(SubjectEntityCreate, c.handleEntityCreate))
	if err != nil {
		return fmt.Errorf("subscribe entity create: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityCreateWithTriples, c.meteredMutation(SubjectEntityCreateWithTriples, c.handleEntityCreateWithTriples))
	if err != nil {
		return fmt.Errorf("subscribe entity create_with_triples: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityUpdate, c.meteredMutation(SubjectEntityUpdate, c.handleEntityUpdate))
	if err != nil {
		return fmt.Errorf("subscribe entity update: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityUpdateWithTriples, c.meteredMutation(SubjectEntityUpdateWithTriples, c.handleEntityUpdateWithTriples))
	if err != nil {
		return fmt.Errorf("subscribe entity update_with_triples: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityDelete, c.meteredMutation(SubjectEntityDelete, c.handleEntityDelete))
	if err != nil {
		return fmt.Errorf("subscribe entity delete: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	c.logger.Info("mutation handlers registered",
		"subjects", []string{
			SubjectTripleAdd, SubjectTripleAddBatch, SubjectTripleRemove,
			SubjectEntityCreate, SubjectEntityCreateWithTriples,
			SubjectEntityUpdate, SubjectEntityUpdateWithTriples,
			SubjectEntityDelete,
		})
	return nil
}

// mutationHandler is the NATS request/reply handler shape for graph mutations.
type mutationHandler = func(ctx context.Context, data []byte) ([]byte, error)

// meteredMutation wraps a mutation request handler to meter rejections
// (ADR-055 §3 observability). It increments mutation_rejections_total{subject,
// reason} and logs the reason at Warn. The handler's (response, error) is passed
// through UNCHANGED — metering never alters the verdict.
//
// ADR-060: a hard mutation failure arrives as a non-nil (*errs.ClassifiedError)
// carrying the stable Code — metered by ce.Code (the bulk of rejections). The
// only non-success signal left in a (body, nil) reply is a PARTIAL batch
// (handleTripleAddBatch with FailedSubjects); it is a partial success, metered
// for continuity under reason="partial_batch" (the per-subject codes live in the
// response FailedSubjects map). Full + degraded success have no failure signal
// in the body and skip the unmarshal.
func (c *Component) meteredMutation(subject string, h mutationHandler) mutationHandler {
	return func(ctx context.Context, data []byte) ([]byte, error) {
		resp, err := h(ctx, data)
		if err != nil {
			reason := graph.ErrorCodeInternal
			var ce *errs.ClassifiedError
			if errors.As(err, &ce) && ce.Code != "" {
				reason = ce.Code
			}
			c.recordMutationRejection(subject, reason, err.Error())
			return resp, err
		}
		// FailedSubjects (omitempty) is present only on a partial batch, so this
		// cheap substring gate skips the unmarshal on the full-success path.
		if bytes.Contains(resp, []byte(`"failed_subjects"`)) {
			var r struct {
				FailedSubjects map[string]string `json:"failed_subjects"`
			}
			if json.Unmarshal(resp, &r) == nil && len(r.FailedSubjects) > 0 {
				c.recordMutationRejection(subject, "partial_batch", fmt.Sprintf("%v", r.FailedSubjects))
			}
		}
		return resp, err
	}
}

// recordMutationRejection increments the rejection counter and logs the
// rejection so the reason is visible even before the must-exist flip. Both the
// counter and the logger are nil-guarded so it is safe on a hand-built Component
// (the test-construction pattern) that reaches it without full wiring.
func (c *Component) recordMutationRejection(subject, reason, detail string) {
	if c.mutationRejections != nil {
		c.mutationRejections.WithLabelValues(subject, reason).Inc()
	}
	if c.logger != nil {
		c.logger.Warn("graph mutation rejected",
			slog.String("subject", subject),
			slog.String("reason", reason),
			slog.String("error", detail))
	}
}

// handleTripleAdd handles add triple requests from rule processor and other components
func (c *Component) handleTripleAdd(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.AddTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}

	// AddTriple uses triple.Subject as entity ID
	if err := c.AddTriple(ctx, req.Triple); err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": req.Triple.Subject}, err)
		}
		// A malformed triple is WrapInvalid (do-not-retry); a KV blip is
		// transient. Classify by the error so the wire class is honest.
		return nil, rejectFromError(err)
	}

	// Get revision after successful mutation for feedback loop prevention
	var kvRevision uint64
	if entry, err := c.entityBucket.Get(ctx, req.Triple.Subject); err == nil {
		kvRevision = entry.Revision
	}

	return json.Marshal(graph.AddTripleResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp:  time.Now().UnixNano(),
			KVRevision: kvRevision,
		},
		Triple: &req.Triple,
	})
}

// handleTripleAddBatch handles batched add-triple requests. The
// implementation groups triples by Subject and issues one CAS per
// entity, so a tool emitting many triples on the same loop entity
// (write_todos, ADR-036) sees one round-trip to graph-ingest instead
// of N. On partial failure across multiple entities, the response
// surfaces FailedSubjects so the caller can decide whether to retry
// the failed subset.
func (c *Component) handleTripleAddBatch(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.AddTriplesBatchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}

	if len(req.Triples) == 0 {
		return json.Marshal(graph.AddTriplesBatchResponse{
			MutationResponse: graph.MutationResponse{
				Timestamp: time.Now().UnixNano(),
			},
			WrittenCount: 0,
		})
	}

	written, failed, err := c.AddTriples(ctx, req.Triples)
	if err != nil && len(failed) == 0 {
		// Whole-batch failure: nothing committed. ADR-060: hard failure →
		// typed error. Classify by the error's nature — pre-CAS validation
		// (empty subject/predicate, WrapInvalid) is invalid_request; a context
		// cancellation/deadline is transient. A PARTIAL batch (some subjects
		// committed) is handled below as a success-with-FailedSubjects body, NOT
		// an error. (ErrKVKeyNotFound always lands in failedSubjects → the
		// partial path, so it cannot reach here; mapped defensively.)
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound, nil, err)
		}
		return nil, rejectFromError(err)
	}

	// Partial (or full) success: FailedSubjects names any subjects that rolled
	// back, mapped to their per-subject error; empty FailedSubjects means full
	// success. ADR-060: this is a success body with a nil Go error — a partial
	// batch committed the subjects not listed, so it must NOT look like a
	// retryable failure. (Whole-batch failure returned a typed error above.)
	return json.Marshal(graph.AddTriplesBatchResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp: time.Now().UnixNano(),
		},
		WrittenCount:   written,
		FailedSubjects: failed,
	})
}

// handleTripleRemove handles remove triple requests from rule processor and other components
func (c *Component) handleTripleRemove(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.RemoveTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}

	// RemoveTriple takes subject (entity ID) and predicate. Removing from a
	// missing entity is an idempotent no-op success on the handler side, so a
	// non-nil error here is a genuine internal failure (transient).
	if err := c.RemoveTriple(ctx, req.Subject, req.Predicate); err != nil {
		return nil, rejectInternal(err)
	}

	// Get revision after successful mutation for feedback loop prevention
	var kvRevision uint64
	if entry, err := c.entityBucket.Get(ctx, req.Subject); err == nil {
		kvRevision = entry.Revision
	}

	return json.Marshal(graph.RemoveTripleResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp:  time.Now().UnixNano(),
			KVRevision: kvRevision,
		},
		Removed: true,
	})
}

// entityExists reports whether the named entity is present in the
// entity bucket. Returns (true, nil) if present, (false, nil) if not,
// or (false, err) for any KV error other than "key not found".
//
// Per feedback_jetstream_sentinel_set_coverage: ErrKeyNotFound and
// ErrNoKeysFound are different sentinels. This helper only collapses
// the single-key not-found path; callers querying key sets must
// handle both.
func (c *Component) entityExists(ctx context.Context, entityID string) (bool, error) {
	_, err := c.entityBucket.Get(ctx, entityID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, natsclient.ErrKVKeyNotFound) {
		return false, nil
	}
	return false, err
}

// handleEntityCreate enforces create-or-fail semantics atomically:
// returns Success=false with an "entity already exists" error if the
// entity ID is already present. CS API §7.6 (POST is strictly create)
// maps that error to HTTP 409 Conflict — the path semconnect Stage 8
// had to fall back from when only triple.add_batch (upsert) was wired.
//
// PR-B: uses natsclient.KVStore.Create via Component.CreateEntityStrict
// for atomic create-or-fail. Concurrent creates of the same ID see
// exactly one winner; losers get ErrKVKeyExists. The PR-A
// exists-check + Put pattern is gone; no TOCTOU window remains.
//
// The Entity field in the success response is read back from storage
// so it reflects framework-injected triples (hierarchy / referential
// integrity stubs); the caller-supplied req.Entity is NOT echoed
// because CreateEntityStrict may mutate it in place (entity.Triples
// append in component.go's hierarchy inference path).
func (c *Component) handleEntityCreate(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.CreateEntityRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if req.Entity == nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("entity cannot be nil"))
	}

	if err := c.CreateEntityStrict(ctx, req.Entity); err != nil {
		if errors.Is(err, natsclient.ErrKVKeyExists) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityExists,
				map[string]any{"entity": req.Entity.ID},
				fmt.Errorf("entity already exists: %s", req.Entity.ID))
		}
		return nil, rejectInternal(err)
	}

	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		// Write committed; read-back failed. #120 contract: degraded
		// success rather than Success=false, so callers don't retry
		// into a 409 Conflict on what's already there.
		return marshalCreateEntityDegraded(req.TraceID, req.RequestID, err.Error())
	}

	return json.Marshal(graph.CreateEntityResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp:  time.Now().UnixNano(),
			KVRevision: rev,
			TraceID:    req.TraceID,
			RequestID:  req.RequestID,
		},
		Entity: stored,
	})
}

// restampStubOnCreate resolves a create_with_triples ID collision (gh#435). If
// the existing entity is a bare referential-integrity stub (graph.StubMessageType)
// and the incoming entity carries a real, VALID, non-stub envelope, it merges the
// incoming entity over the stub — the stub's true birth — via the same
// update-lane MergeEntity the downstream would otherwise fall back to, returning
// restamped=true with a Debug (no reject WARN) and a distinct stub_restamps_total
// counter tick. A non-stub collision (a real entity), or an incoming stub / zero /
// invalid envelope, is NOT a stub birth: restamped=false, and the caller rejects
// as before — which PRESERVES the stub. (A zero/invalid envelope must not reach
// MergeEntity: that path overwrites MessageType unconditionally, so it would
// demote the stub to a typeless non-stub entity — re-creating the gh#429
// dispatchable-non-real class — and would violate graph.StubMessageType's
// preserve-when-zero contract.) A transient read/merge failure returns a
// classified error.
//
// Under a genuine double-birth race (two real create_with_triples on the same
// stub) both callers may read the stub and both MergeEntity: convergent
// (CAS-merged, last-writer-wins on MessageType), ticking the counter twice for one
// entity — accepted over reject-one, and ill-defined for a well-formed graph.
func (c *Component) restampStubOnCreate(ctx context.Context, entity *graph.EntityState) (bool, error) {
	existing, _, err := c.fetchEntityState(ctx, entity.ID)
	if err != nil {
		return false, rejectInternal(fmt.Errorf("stub-restamp read-back for %s: %w", entity.ID, err))
	}
	// Restamp ONLY for a real, valid, non-stub incoming envelope. !IsValid() rejects
	// a zero/partial type (MergeEntity would overwrite the stub envelope to typeless
	// — the gh#429 class); Equal(StubMessageType) rejects a validly-typed-as-stub
	// incoming. Either falls through to the caller's reject, leaving the stub intact.
	if existing == nil || !existing.IsStub() ||
		!entity.MessageType.IsValid() || entity.MessageType.Equal(graph.StubMessageType) {
		return false, nil
	}
	if merr := c.MergeEntity(ctx, entity); merr != nil {
		return false, rejectInternal(merr)
	}
	if c.stubRestamps != nil {
		c.stubRestamps.Inc()
	}
	if c.logger != nil {
		c.logger.Debug("referential stub re-stamped as real birth on create_with_triples (gh#435)",
			slog.String("entity_id", entity.ID),
			slog.String("subject", SubjectEntityCreateWithTriples))
	}
	return true, nil
}

// handleEntityCreateWithTriples is handleEntityCreate that also accepts
// a Triples slice in the request envelope. When Triples is non-empty
// it REPLACES Entity.Triples before the write (the canonical set is
// the request's Triples; Entity carries provenance metadata). When
// Triples is nil/empty, Entity.Triples is written as-is.
//
// Atomicity matches handleEntityCreate: CreateEntityStrict is used
// for atomic create-or-fail (returns ErrKVKeyExists on duplicate ID).
//
// TriplesAdded in the response is the count on the *stored* entity
// after the write, which may exceed len(req.Triples) when the
// framework injects hierarchy / referential-integrity triples per
// component.go:createEntity. Callers reasoning about provenance
// should compare req.Triples against stored.Triples explicitly rather
// than treating TriplesAdded as authoritative.
func (c *Component) handleEntityCreateWithTriples(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.CreateEntityWithTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if req.Entity == nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("entity cannot be nil"))
	}

	if len(req.Triples) > 0 {
		req.Entity.Triples = req.Triples
	}

	// Shared projection-normalization seam (ADR-056 Decision 4): the mutation API
	// is a graph write API, not a bypass — split off any foreign-subject edge
	// (Subject != Entity.ID) before committing the primary, so it is classified
	// and routed to its own subject instead of being misfiled onto Entity.ID.
	// foreign is empty for every current caller (single-subject batches; cs-api's
	// singleSubject guard blocks multi-subject today).
	own, foreign := c.normalizeProjection(ctx, req.Entity.ID, req.Entity.MessageType, req.Entity.Triples)
	req.Entity.Triples = own

	// ADR-056 owner-lease check. Observe-only by default (PR-3: meter + Warn,
	// write commits); when enforce_owner_lease is set (PR-5) a confirmed mismatch
	// on an owned predicate rejects the write BEFORE CreateEntityStrict commits,
	// with ErrorCodeOwnerLeaseStale. The owned-predicate set is the own-subject
	// triples after normalizeProjection (the non-foreign portion committed to the
	// primary).
	if v := c.checkOwnerLease(ctx, req.Entity.ID, labelForMessageType(req.Entity.MessageType), req.OwnerToken,
		predicatesOf(own)); v != nil {
		return nil, rejectInvalidDetail(graph.ErrorCodeOwnerLeaseStale,
			map[string]any{"entity": req.Entity.ID}, errors.New(v.detail()))
	}

	// ADR-054 channel (b): stamp an explicitly declared indexing profile from
	// the mutation envelope (highest precedence). Empty/invalid is ignored and
	// the createEntity seam applies the fallback floor instead.
	stampExplicitIndexingProfile(req.Entity, req.IndexingProfile)

	if err := c.CreateEntityStrict(ctx, req.Entity); err != nil {
		if !errors.Is(err, natsclient.ErrKVKeyExists) {
			return nil, rejectInternal(err)
		}
		// gh#435: the ID may exist only as a referential-integrity stub — a
		// placeholder minted when something referenced it before its real birth.
		// A create_with_triples carrying a real (non-stub) envelope IS that birth,
		// so re-stamp the stub via merge instead of rejecting: a healthy stub→real
		// path must not surface as a graph-write reject to paid-run monitors. A
		// non-stub collision is a genuine conflict and still rejects.
		restamped, rerr := c.restampStubOnCreate(ctx, req.Entity)
		if rerr != nil {
			return nil, rerr
		}
		if !restamped {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityExists,
				map[string]any{"entity": req.Entity.ID},
				fmt.Errorf("entity already exists: %s", req.Entity.ID))
		}
		// Re-stamped: the stub is now born. Fall through to the shared success
		// path (foreign-edge routing + read-back + response), same as a fresh create.
	}

	// Primary committed — route foreign edges onto their own subjects (after the
	// commit so a failed create never orphans a routed edge).
	c.routeForeignEdges(ctx, req.Entity.ID, req.Entity.MessageType, foreign)

	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		// #120: write committed, read-back failed → degraded success.
		return marshalCreateEntityWithTriplesDegraded(req.TraceID, req.RequestID, err.Error())
	}

	return json.Marshal(graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp:  time.Now().UnixNano(),
			KVRevision: rev,
			TraceID:    req.TraceID,
			RequestID:  req.RequestID,
		},
		Entity:       stored,
		TriplesAdded: len(stored.Triples),
	})
}

// handleEntityUpdate enforces must-exist semantics with a CAS-protected
// write: a concurrent DeleteEntity that lands between the read and the
// write turns the CAS into ErrKVRevisionMismatch instead of silently
// resurrecting the entity (the bug an exists-check + Put pair would
// have). CS API PUT/PATCH maps Success=false with "entity not found"
// to HTTP 404; the same error message covers both "absent at read"
// and "concurrent modification or delete during write" because
// downstream gateways (semconnect) currently map both to the same
// status. A future PR may want to disambiguate "deleted concurrently"
// vs "modified concurrently" with distinct error classes — for now
// they share the "(concurrent modification or delete)" suffix so
// callers can grep for it without having to peek inside ErrKVRevisionMismatch.
func (c *Component) handleEntityUpdate(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.UpdateEntityRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if req.Entity == nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("entity cannot be nil"))
	}

	_, currentRev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": req.Entity.ID},
				fmt.Errorf("entity not found: %s", req.Entity.ID))
		}
		return nil, rejectInternal(fmt.Errorf("fetch current entity failed: %w", err))
	}

	if err := c.updateEntityAtRevision(ctx, req.Entity, currentRev); err != nil {
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			// A concurrent delete/modify between read and write on this
			// must-exist (non-CAS) update surfaces as "entity not found" — the
			// 404 contract semconnect maps. This is NOT the revision_mismatch
			// CAS-retry sentinel (that is the ExpectedRevision path below).
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": req.Entity.ID},
				fmt.Errorf("entity not found: %s (concurrent modification or delete)", req.Entity.ID))
		}
		return nil, rejectInternal(err)
	}

	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		// #120: write committed, read-back failed → degraded success.
		// CAS revision moved; a retry would mismatch.
		return marshalUpdateEntityDegraded(req.TraceID, req.RequestID, err.Error())
	}

	return json.Marshal(graph.UpdateEntityResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp:  time.Now().UnixNano(),
			KVRevision: rev,
			TraceID:    req.TraceID,
			RequestID:  req.RequestID,
		},
		Entity:  stored,
		Version: int64(stored.Version),
	})
}

// preserveStoredEntityMetadata backfills entity metadata the caller left unset
// from the stored state, so a triple-delta update never silently erases the
// entity envelope.
//
// update_with_triples writes req.Entity wholesale (only its triples are
// replaced by the merge). A caller that sends a bare EntityState{ID} — carrying
// just the triple delta, which is a natural shape for "append/replace some
// predicates" — would otherwise CLOBBER the stored semantic envelope:
// MessageType (the ADR-055 provenance + ADR-054 indexing-profile registry key)
// would reset to the zero Type, Version would reset to 0 (breaking
// optimistic-concurrency monotonicity), and StorageRef (offloaded ContentStorable
// content) would be dropped. Treat a zero MessageType / zero Version / nil
// StorageRef on the request as "keep the stored value". Callers that genuinely
// set these (e.g. pkg/lifecycle.Manager, which passes an explicit MessageType and
// Version+1 on every transition) are unaffected — their non-zero values win.
//
// KNOWN LIMITATION (gh#260): this overloads the zero value to mean "preserve",
// so there is NO way to deliberately CLEAR these fields to their zero value
// through update_with_triples — sending zero means "leave as-is". That is the
// desired invariant for MessageType (the envelope must persist — ADR-055) and
// Version (monotonic). The only field with a plausible deliberate-clear case is
// StorageRef (e.g. its offloaded content was GC'd); no caller needs it today. If
// one ever does, add an explicit signal (a ClearFields list / pointer-optional
// fields) rather than un-overloading zero. (Triples are unaffected — clear a
// predicate via UpdateEntityWithTriplesRequest.RemoveTriples.)
func preserveStoredEntityMetadata(newEntity, stored *graph.EntityState) {
	if newEntity == nil || stored == nil {
		return
	}
	if newEntity.MessageType == (message.Type{}) {
		newEntity.MessageType = stored.MessageType
	}
	if newEntity.Version == 0 {
		newEntity.Version = stored.Version
	}
	if newEntity.StorageRef == nil {
		newEntity.StorageRef = stored.StorageRef
	}
}

// handleEntityUpdateWithTriples applies a triple-set delta to an
// existing entity: appends AddTriples, removes triples whose Predicate
// is named in RemoveTriples, and writes the result via CAS-with-retry.
// Entity metadata (MessageType, Version, StorageRef) from the request is
// written, but any field the caller leaves at its zero value is preserved
// from the stored entity (see preserveStoredEntityMetadata) so a bare
// triple-delta update does not erase the entity envelope. The merged triple
// set replaces what's in req.Entity.Triples.
//
// PR-B: the delta apply runs inside natsclient.KVStore.UpdateWithRetry,
// so concurrent writes that bump the entity revision between read
// and write trigger a re-fetch + re-apply of the delta against the
// fresh state. Closes the partial-erasure window semconnect Stage 27
// flagged in the cs-api delete-all + re-add shim: a concurrent
// triple add from another writer no longer makes the delta drop work
// silently.
//
// Must-exist contract is preserved: if the entity is absent on the
// first attempt OR gets deleted between attempts, the update
// function returns retry.NonRetryable(ErrKVKeyNotFound) and the
// handler surfaces "entity not found" — same shape semconnect maps
// to HTTP 404. CAS-conflict-without-delete (another writer modified
// the entity) is silently retried up to the bucket's configured
// MaxAttempts; the caller sees the final successful state in the
// response.
//
// Unknown predicates in RemoveTriples are silently ignored (not
// "404'd"); the contract is "remove if present" not "predicate must
// exist".
//
// Retry exhaustion: on the unlikely path where every retry attempt
// hits a CAS conflict and MaxAttempts is reached, UpdateWithRetry
// returns natsclient.ErrKVMaxRetriesExceeded. The handler surfaces
// it verbatim through the body-prefix error convention; downstream
// gateways may retry the whole request or escalate. Distinct from
// "entity not found", which is the NonRetryable absence path.
func (c *Component) handleEntityUpdateWithTriples(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.UpdateEntityWithTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if req.Entity == nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("entity cannot be nil"))
	}

	// ADR-054 §5: the indexing profile is immutable after creation EXCEPT via
	// an explicit envelope override here. Inject it as a replace-by-predicate
	// delta (RemoveTriples drops the prior value; AddTriples/MergeTriples adds
	// the new one) so BOTH the CAS and UpdateWithRetry sub-paths apply it and
	// the single-valued predicate stays unique. A later triple.add by a
	// different writer must NOT change the profile — only this envelope does.
	if req.IndexingProfile != "" {
		if !vocabulary.IsValidIndexingProfile(req.IndexingProfile) {
			return nil, rejectInvalid(graph.ErrorCodeInvalidRequest,
				fmt.Errorf("invalid indexing_profile %q (want content|control|signal|trace)", req.IndexingProfile))
		}
		req.RemoveTriples = append(req.RemoveTriples, vocabulary.EntityIndexingProfile)
		req.AddTriples = append(req.AddTriples, message.Triple{
			Subject:    req.Entity.ID,
			Predicate:  vocabulary.EntityIndexingProfile,
			Object:     req.IndexingProfile,
			Source:     "graph-ingest-indexing-profile",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		})
	}

	// Shared projection-normalization seam (ADR-056 Decision 4): split off any
	// foreign-subject edge from the AddTriples delta so it is classified + routed
	// to its own subject instead of merged onto Entity.ID. Both the CAS and the
	// UpdateWithRetry sub-paths apply req.AddTriples, so normalize once here.
	// foreign is empty for every current caller (the lifecycle Manager's CAS
	// deltas are single-subject; cs-api's singleSubject guard blocks multi-subject).
	addOwn, foreign := c.normalizeProjection(ctx, req.Entity.ID, req.Entity.MessageType, req.AddTriples)
	req.AddTriples = addOwn

	// ADR-056 owner-lease check (observe-only by default; rejects with
	// ErrorCodeOwnerLeaseStale when enforce_owner_lease is set — PR-5). This runs
	// BEFORE the ExpectedRevision CAS dispatch below, so a SINGLE check covers
	// BOTH the CAS and the UpdateWithRetry sub-paths (the lifecycle-transition CAS
	// lane included) — reject once, no double-count, and no orphaned foreign edge
	// (we return before routeForeignEdges). The owned-predicate set is the union
	// of own add-triples predicates (the replace-merge set) and remove-triples
	// predicates (a predicate named only in RemoveTriples is the owned cell being
	// cleared — the write targets that owned predicate even though no new triple
	// is added for it). Deduped by checkOwnerLease's loop.
	if v := c.checkOwnerLease(ctx, req.Entity.ID, labelForMessageType(req.Entity.MessageType), req.OwnerToken,
		dedupePredicates(predicatesOf(addOwn), req.RemoveTriples)); v != nil {
		return nil, rejectInvalidDetail(graph.ErrorCodeOwnerLeaseStale,
			map[string]any{"entity": req.Entity.ID}, errors.New(v.detail()))
	}

	// ADR-049: CAS-on-condition path. Non-zero ExpectedRevision means
	// the caller requires the entity's current KV revision to match
	// exactly; mismatch fails the request without silently merging.
	// pkg/lifecycle.Manager.Transition uses this to preserve
	// state-machine correctness under concurrent writes. Existing
	// callers pass zero and get the UpdateWithRetry merge behavior
	// unchanged.
	if req.ExpectedRevision > 0 {
		resp, err := c.handleEntityUpdateWithTriplesCAS(ctx, &req)
		// Route foreign edges ONLY when the CAS primary write actually COMMITTED.
		// ADR-060: a logical CAS failure (revision mismatch) now returns
		// (nil, *errs.ClassifiedError), so err != nil captures every non-commit
		// outcome; an err==nil reply is always a committed (full or degraded)
		// success. The decoded-Success gate this used to need is gone with the
		// in-body Success flag.
		if err == nil && len(foreign) > 0 {
			c.routeForeignEdges(ctx, req.Entity.ID, req.Entity.MessageType, foreign)
		}
		return resp, err
	}

	// PR-B: the delta apply runs *inside* UpdateWithRetry's update
	// function, so each retry re-applies AddTriples/RemoveTriples
	// against the fresh current state. PR-A read once then CAS'd,
	// losing concurrent triples on the retry path.
	//
	// triplesRemoved is captured by the closure and assigned on each
	// attempt; the LAST attempt (the one that succeeded or NonRetryable'd)
	// owns the final value. UpdateWithRetry invokes updateFn at least
	// once and at most retryConfig.MaxAttempts times.
	var triplesRemoved int
	err := c.entityBucket.UpdateWithRetry(ctx, req.Entity.ID, func(current []byte) ([]byte, error) {
		// Must-exist contract: an empty current value means the entity
		// is absent (either never created OR deleted between attempts).
		// retry.NonRetryable stops the retry loop and surfaces the
		// sentinel for the handler to map to "entity not found".
		if len(current) == 0 {
			return nil, retry.NonRetryable(natsclient.ErrKVKeyNotFound)
		}

		var currentState graph.EntityState
		if err := json.Unmarshal(current, &currentState); err != nil {
			return nil, retry.NonRetryable(
				fmt.Errorf("unmarshal current entity: %w", err))
		}

		// Re-apply the delta on each attempt: drop triples whose
		// Predicate appears in RemoveTriples, then merge AddTriples in.
		// triplesRemoved is reset each attempt so it reflects the
		// final successful apply.
		triplesRemoved = 0
		merged := currentState.Triples
		if len(req.RemoveTriples) > 0 {
			removeSet := make(map[string]struct{}, len(req.RemoveTriples))
			for _, p := range req.RemoveTriples {
				removeSet[p] = struct{}{}
			}
			kept := make([]message.Triple, 0, len(currentState.Triples))
			for _, t := range currentState.Triples {
				if _, drop := removeSet[t.Predicate]; drop {
					triplesRemoved++
					continue
				}
				kept = append(kept, t)
			}
			merged = kept
		}
		if len(req.AddTriples) > 0 {
			// MergeTriples = replace-by-(subject,predicate): a predicate
			// present in AddTriples fully replaces its prior values
			// (upsert), matching the DataManager UpdateEntityWithTriples
			// contract (graph/datamanager/manager.go) — gh#244. This is
			// NOT incremental append; a caller that wants to preserve
			// existing values of a predicate and add more uses
			// triple.add / triple.add_batch. Replace-by-predicate also
			// keeps single-valued predicates unique, which is what the
			// lifecycle replace-not-append fix relies on.
			merged = graph.MergeTriples(merged, req.AddTriples)
		}

		// Build the new entity: caller's metadata (req.Entity) +
		// merged triples. Shallow copy so we don't mutate the
		// caller-owned pointer (see PR-A go-reviewer F2 finding).
		newEntity := *req.Entity
		newEntity.Triples = merged
		preserveStoredEntityMetadata(&newEntity, &currentState)

		return json.Marshal(&newEntity)
	})
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": req.Entity.ID},
				fmt.Errorf("entity not found: %s", req.Entity.ID))
		}
		return nil, rejectInternal(err)
	}

	// Primary update committed — invalidate the entity-query cache so a
	// read-after-write via graph.ingest.query.* sees the new triples (the
	// gated-DAG executor's claim-then-reread relies on this), then route any
	// foreign edges onto their own subjects.
	c.invalidateEntityCacheEntry(req.Entity.ID)
	c.routeForeignEdges(ctx, req.Entity.ID, req.Entity.MessageType, foreign)

	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		// #120: write committed, read-back failed → degraded success.
		return marshalUpdateEntityWithTriplesDegraded(req.TraceID, req.RequestID, err.Error())
	}

	return json.Marshal(graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp:  time.Now().UnixNano(),
			KVRevision: rev,
			TraceID:    req.TraceID,
			RequestID:  req.RequestID,
		},
		Entity:         stored,
		TriplesAdded:   len(req.AddTriples),
		TriplesRemoved: triplesRemoved,
		Version:        int64(stored.Version),
	})
}

// handleEntityUpdateWithTriplesCAS is the CAS-on-condition branch of
// handleEntityUpdateWithTriples (ADR-049). Reads the entity's current
// state + revision, rejects if the revision doesn't match the caller's
// ExpectedRevision, applies the delta, and writes single-pass via
// updateEntityAtRevision so a concurrent writer between read and
// write fails the request rather than silently merging.
//
// Distinct from the default UpdateWithRetry path because the caller
// requires "current state is what I expected" semantics — this is
// the primitive pkg/lifecycle.Manager.Transition uses to preserve
// state-machine correctness. The default path is correct for the
// graph's facts-accumulate model; this path is correct for the
// state-machine-transition model.
//
// On revision mismatch (either at read-time or write-time CAS), the
// error message includes the expected vs actual revision so callers
// can decide whether to retry the whole read-validate-emit loop
// (Manager.Transition does this) or surface to the user.
func (c *Component) handleEntityUpdateWithTriplesCAS(ctx context.Context, req *graph.UpdateEntityWithTriplesRequest) ([]byte, error) {
	// Read current state with revision.
	current, currentRev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, rejectInvalidDetail(graph.ErrorCodeEntityNotFound,
				map[string]any{"entity": req.Entity.ID},
				fmt.Errorf("entity not found: %s", req.Entity.ID))
		}
		return nil, rejectInternal(fmt.Errorf("fetch current entity failed: %w", err))
	}

	// CAS-on-condition: caller's expected rev must match what we just read.
	// ADR-060: a revision mismatch is the ONE control-flow sentinel — it carries
	// Code revision_mismatch so the caller's errors.Is(err, errs.ErrRevisionMismatch)
	// CAS-retry loop fires (Manager.Transition). NOT a hard failure.
	if currentRev != req.ExpectedRevision {
		return nil, rejectRevisionMismatch(
			map[string]any{"entity": req.Entity.ID, "expected_revision": req.ExpectedRevision, "current_revision": currentRev},
			fmt.Errorf("revision mismatch: expected %d, current %d", req.ExpectedRevision, currentRev))
	}

	// Apply delta against current state. Same merge logic as the
	// UpdateWithRetry path; lifted here so this path doesn't depend
	// on the retry closure.
	triplesRemoved := 0
	merged := current.Triples
	if len(req.RemoveTriples) > 0 {
		removeSet := make(map[string]struct{}, len(req.RemoveTriples))
		for _, p := range req.RemoveTriples {
			removeSet[p] = struct{}{}
		}
		kept := make([]message.Triple, 0, len(current.Triples))
		for _, t := range current.Triples {
			if _, drop := removeSet[t.Predicate]; drop {
				triplesRemoved++
				continue
			}
			kept = append(kept, t)
		}
		merged = kept
	}
	if len(req.AddTriples) > 0 {
		// Replace-by-(subject,predicate), matching the UpdateWithRetry
		// path and the DataManager contract (gh#244) — not append.
		merged = graph.MergeTriples(merged, req.AddTriples)
	}

	// Build the new entity: caller's metadata + merged triples.
	// Shallow-copy so we don't mutate the caller-owned pointer.
	newEntity := *req.Entity
	newEntity.Triples = merged
	preserveStoredEntityMetadata(&newEntity, current)

	// Single-pass CAS via the existing updateEntityAtRevision
	// primitive. A concurrent writer between our read and write
	// trips ErrKVRevisionMismatch here even though the read-time
	// check passed.
	if err := c.updateEntityAtRevision(ctx, &newEntity, req.ExpectedRevision); err != nil {
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return nil, rejectRevisionMismatch(
				map[string]any{"entity": req.Entity.ID, "expected_revision": req.ExpectedRevision},
				fmt.Errorf("revision mismatch: concurrent modification of %s (expected revision %d)",
					req.Entity.ID, req.ExpectedRevision))
		}
		return nil, rejectInternal(err)
	}
	// CAS-path cache invalidation is owned by updateEntityAtRevision (it Deletes
	// the cache entry after a successful CAS commit), so no invalidate is needed
	// here — unlike the non-CAS UpdateWithRetry path above, which writes the
	// bucket directly and must invalidate itself.

	// Read back for response. Degraded path handles read-back failure
	// (write committed, read-back failed → operator sees Success +
	// Degraded flag per #120).
	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		return marshalUpdateEntityWithTriplesDegraded(req.TraceID, req.RequestID, err.Error())
	}

	return json.Marshal(graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp:  time.Now().UnixNano(),
			KVRevision: rev,
			TraceID:    req.TraceID,
			RequestID:  req.RequestID,
		},
		Entity:         stored,
		TriplesAdded:   len(req.AddTriples),
		TriplesRemoved: triplesRemoved,
		Version:        int64(stored.Version),
	})
}

// handleEntityDelete is entity-scoped delete. Idempotent: deleting a
// non-existent entity returns Success=true, Deleted=false. The NATS
// KV Delete primitive itself is idempotent, so this handler just
// distinguishes "the call succeeded" from "the entity actually was
// present" via the Deleted bool.
func (c *Component) handleEntityDelete(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.DeleteEntityRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, fmt.Errorf("invalid request: %w", err))
	}
	if req.EntityID == "" {
		return nil, rejectInvalid(graph.ErrorCodeInvalidRequest, errors.New("entity_id cannot be empty"))
	}

	existed, err := c.entityExists(ctx, req.EntityID)
	if err != nil {
		return nil, rejectInternal(fmt.Errorf("existence check failed: %w", err))
	}

	if err := c.DeleteEntity(ctx, req.EntityID); err != nil {
		return nil, rejectInternal(err)
	}

	return json.Marshal(graph.DeleteEntityResponse{
		MutationResponse: graph.MutationResponse{
			Timestamp: time.Now().UnixNano(),
			TraceID:   req.TraceID,
			RequestID: req.RequestID,
		},
		Deleted: existed,
	})
}

// fetchEntityState reads + decodes the entity from the KV bucket and
// returns its current KV revision alongside the decoded state. The
// not-found error is returned verbatim so callers can branch on
// natsclient.ErrKVKeyNotFound via errors.Is. A zero-length entry is
// also treated as "not found" — a tombstone replay or a half-finished
// put can leave a key present with an empty body, and unmarshaling
// "" would otherwise surface a confusing "unexpected end of JSON
// input" instead of the clean not-found classification callers expect.
func (c *Component) fetchEntityState(ctx context.Context, entityID string) (*graph.EntityState, uint64, error) {
	entry, err := c.entityBucket.Get(ctx, entityID)
	if err != nil {
		return nil, 0, err
	}
	if len(entry.Value) == 0 {
		return nil, 0, natsclient.ErrKVKeyNotFound
	}
	var state graph.EntityState
	if err := json.Unmarshal(entry.Value, &state); err != nil {
		return nil, 0, fmt.Errorf("unmarshal entity state: %w", err)
	}
	return &state, entry.Revision, nil
}

// predicatesOf extracts the Predicate strings from a triple slice, for use in
// the owner-lease check's owned-predicate set computation.
func predicatesOf(triples []message.Triple) []string {
	if len(triples) == 0 {
		return nil
	}
	out := make([]string, 0, len(triples))
	for _, t := range triples {
		if t.Predicate != "" {
			out = append(out, t.Predicate)
		}
	}
	return out
}

// dedupePredicates merges two predicate slices and returns a deduped result,
// preserving the first appearance of each predicate. Used by checkOwnerLease to
// build the union of add-triples and remove-triples predicate sets.
func dedupePredicates(a, b []string) []string {
	total := len(a) + len(b)
	if total == 0 {
		return nil
	}
	seen := make(map[string]struct{}, total)
	out := make([]string, 0, total)
	for _, s := range a {
		if s == "" {
			continue
		}
		if _, dup := seen[s]; !dup {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range b {
		if s == "" {
			continue
		}
		if _, dup := seen[s]; !dup {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// labelForMessageType returns a metric-safe label for the given MessageType:
// the dotted Key() when valid, or "_invalid" when zero/unregistered (cardinality
// guard matching the existing foreign-edge metric convention).
func labelForMessageType(mt message.Type) string {
	if mt.IsValid() {
		return mt.Key()
	}
	return invalidMessageTypeLabel
}

// ADR-060 producer helpers — a hard graph-ingest mutation failure travels as
// (nil, *errs.ClassifiedError), which the SubscribeForRequests wrapper turns
// into a header-classified reply (X-Error-Class + X-Error-Code) via
// natsclient.RespondError. This replaces the per-response-shape Success=false
// body marshalers: a failure carries no success body, so the response-shape
// specificity (and the TraceID/RequestID echo) no longer applies on the failure
// path — the reply inbox already correlates the response, and in-tree callers
// branch on err, not a body field.
//
// Code is the stable graph.ErrorCode* discriminator; Detail carries structured
// context (entity id, revisions). Detail is carried on the error value now and
// serialized into the standard error body by the PR-D breaking change — until
// then it does not cross the wire (callers see Code via the header).

// rejectInvalid builds an invalid-class (400-like) classified failure with the
// given stable Code. err's text is preserved verbatim as the wire message.
func rejectInvalid(code string, err error) error {
	return errs.ClassifiedCode(errs.ErrorInvalid, code, err)
}

// rejectInvalidDetail is rejectInvalid plus structured Detail (entity id, ...).
func rejectInvalidDetail(code string, detail map[string]any, err error) error {
	return errs.ClassifiedCodeDetail(errs.ErrorInvalid, code, detail, err)
}

// rejectInternal builds a transient-class (retryable, 5xx-like) internal
// failure. Matches the query handlers' choice that "internal" is transient.
func rejectInternal(err error) error {
	return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, err)
}

// rejectFromError classifies a GENERIC handler-internal error by the error's own
// nature, for the fallback paths where the failure could be either a do-not-retry
// validation error or a retryable transient one. An already-invalid error (e.g.
// AddTriple/AddTriples' WrapInvalid on a malformed triple) becomes
// invalid_request; everything else (KV blips, context cancel/deadline) becomes
// internal/transient (retryable). Without this, a fixed per-call-site class would
// advertise a malformed-triple as retryable, or a context cancellation as a hard
// 400 — wrong now that the class rides the wire (ADR-060).
func rejectFromError(err error) error {
	if errs.IsInvalid(err) {
		return rejectInvalid(graph.ErrorCodeInvalidRequest, err)
	}
	return rejectInternal(err)
}

// rejectRevisionMismatch builds the ONE control-flow sentinel — a CAS
// revision_mismatch. It is invalid-class but carries Code revision_mismatch so a
// CAS-retry caller resolves errors.Is(err, errs.ErrRevisionMismatch) and loops
// (Manager.Transition), rather than treating it as a hard 400.
func rejectRevisionMismatch(detail map[string]any, err error) error {
	return errs.ClassifiedCodeDetail(errs.ErrorInvalid, graph.ErrorCodeRevisionMismatch, detail, err)
}

// Per-response-shape "degraded success" marshalers (#120). Emitted
// when the write committed durably but the post-write read-back
// failed (context cancellation, JetStream node failover, bucket
// re-keyed). ADR-060 caller-visible contract: a (body, nil) success with
// Degraded=true, Entity=nil, KVRevision=0, and DegradedReason carrying the
// read-back failure reason prefixed by degradedReadbackErrPrefix. The
// triple-rich response shapes also zero out TriplesAdded/TriplesRemoved/Version
// since those are derived from the same stored entity we couldn't read back.

// degradedReadbackErrPrefix is the canonical prefix on the DegradedReason field
// of any degraded-success mutation response. Extracted as a constant
// so the four marshalers below and the regression test in
// mutations_degraded_test.go can both reference the same source of
// truth — a future refactor of the wording lands in one place rather
// than five.
const degradedReadbackErrPrefix = "post-write read-back failed: "

func marshalCreateEntityDegraded(traceID, requestID, readbackErr string) ([]byte, error) {
	return json.Marshal(graph.CreateEntityResponse{
		MutationResponse: graph.MutationResponse{
			Degraded:       true,
			DegradedReason: degradedReadbackErrPrefix + readbackErr,
			Timestamp:      time.Now().UnixNano(),
			TraceID:        traceID,
			RequestID:      requestID,
		},
	})
}

func marshalCreateEntityWithTriplesDegraded(traceID, requestID, readbackErr string) ([]byte, error) {
	return json.Marshal(graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Degraded:       true,
			DegradedReason: degradedReadbackErrPrefix + readbackErr,
			Timestamp:      time.Now().UnixNano(),
			TraceID:        traceID,
			RequestID:      requestID,
		},
	})
}

func marshalUpdateEntityDegraded(traceID, requestID, readbackErr string) ([]byte, error) {
	return json.Marshal(graph.UpdateEntityResponse{
		MutationResponse: graph.MutationResponse{
			Degraded:       true,
			DegradedReason: degradedReadbackErrPrefix + readbackErr,
			Timestamp:      time.Now().UnixNano(),
			TraceID:        traceID,
			RequestID:      requestID,
		},
	})
}

func marshalUpdateEntityWithTriplesDegraded(traceID, requestID, readbackErr string) ([]byte, error) {
	return json.Marshal(graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Degraded:       true,
			DegradedReason: degradedReadbackErrPrefix + readbackErr,
			Timestamp:      time.Now().UnixNano(),
			TraceID:        traceID,
			RequestID:      requestID,
		},
	})
}
