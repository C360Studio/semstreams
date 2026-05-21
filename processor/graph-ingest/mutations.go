// Package graphingest mutation handlers for triple and entity operations via NATS request/reply.
package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
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
	sub, err := c.natsClient.SubscribeForRequests(ctx, SubjectTripleAdd, c.handleTripleAdd)
	if err != nil {
		return fmt.Errorf("subscribe triple add: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectTripleAddBatch, c.handleTripleAddBatch)
	if err != nil {
		return fmt.Errorf("subscribe triple add_batch: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectTripleRemove, c.handleTripleRemove)
	if err != nil {
		return fmt.Errorf("subscribe triple remove: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityCreate, c.handleEntityCreate)
	if err != nil {
		return fmt.Errorf("subscribe entity create: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityCreateWithTriples, c.handleEntityCreateWithTriples)
	if err != nil {
		return fmt.Errorf("subscribe entity create_with_triples: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityUpdate, c.handleEntityUpdate)
	if err != nil {
		return fmt.Errorf("subscribe entity update: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityUpdateWithTriples, c.handleEntityUpdateWithTriples)
	if err != nil {
		return fmt.Errorf("subscribe entity update_with_triples: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectEntityDelete, c.handleEntityDelete)
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

// handleTripleAdd handles add triple requests from rule processor and other components
func (c *Component) handleTripleAdd(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.AddTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(graph.AddTripleResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     fmt.Sprintf("invalid request: %v", err),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	// AddTriple uses triple.Subject as entity ID
	err := c.AddTriple(ctx, req.Triple)
	if err != nil {
		return json.Marshal(graph.AddTripleResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     err.Error(),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	// Get revision after successful mutation for feedback loop prevention
	var kvRevision uint64
	if entry, err := c.entityBucket.Get(ctx, req.Triple.Subject); err == nil {
		kvRevision = entry.Revision
	}

	return json.Marshal(graph.AddTripleResponse{
		MutationResponse: graph.MutationResponse{
			Success:    true,
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
		return json.Marshal(graph.AddTriplesBatchResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     fmt.Sprintf("invalid request: %v", err),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	if len(req.Triples) == 0 {
		return json.Marshal(graph.AddTriplesBatchResponse{
			MutationResponse: graph.MutationResponse{
				Success:   true,
				Timestamp: time.Now().UnixNano(),
			},
			WrittenCount: 0,
		})
	}

	written, failed, err := c.AddTriples(ctx, req.Triples)
	if err != nil && len(failed) == 0 {
		// Pre-CAS validation failure (e.g. empty subject/predicate).
		// Whole batch rejected.
		return json.Marshal(graph.AddTriplesBatchResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     err.Error(),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	resp := graph.AddTriplesBatchResponse{
		MutationResponse: graph.MutationResponse{
			Success:   len(failed) == 0,
			Timestamp: time.Now().UnixNano(),
		},
		WrittenCount:   written,
		FailedSubjects: failed,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	return json.Marshal(resp)
}

// handleTripleRemove handles remove triple requests from rule processor and other components
func (c *Component) handleTripleRemove(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.RemoveTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(graph.RemoveTripleResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     fmt.Sprintf("invalid request: %v", err),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	// RemoveTriple takes subject (entity ID) and predicate
	err := c.RemoveTriple(ctx, req.Subject, req.Predicate)
	if err != nil {
		return json.Marshal(graph.RemoveTripleResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     err.Error(),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	// Get revision after successful mutation for feedback loop prevention
	var kvRevision uint64
	if entry, err := c.entityBucket.Get(ctx, req.Subject); err == nil {
		kvRevision = entry.Revision
	}

	return json.Marshal(graph.RemoveTripleResponse{
		MutationResponse: graph.MutationResponse{
			Success:    true,
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

// handleEntityCreate enforces create-or-fail semantics: returns
// Success=false with an "entity already exists" error if the entity
// ID is already present. CS API §7.6 (POST is strictly create) maps
// that error to HTTP 409 Conflict — the path semconnect Stage 8 had
// to fall back from when only triple.add_batch (upsert) was wired.
//
// PR-A: existence check is a Get followed by CreateEntity (Put). The
// TOCTOU window between the two means concurrent creates of the same
// ID are last-writer-wins instead of strict create-or-fail. PR-B will
// switch to the atomic KV Create primitive (natsclient.KVStore.Create
// returns ErrKVKeyExists) which closes the window.
//
// The Entity field in the success response is read back from storage
// so it reflects framework-injected triples (hierarchy / referential
// integrity stubs); the caller-supplied req.Entity is NOT echoed
// because CreateEntity may mutate it in place (entity.Triples append
// in component.go).
func (c *Component) handleEntityCreate(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.CreateEntityRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return marshalCreateEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("invalid request: %v", err))
	}
	if req.Entity == nil {
		return marshalCreateEntityError(req.TraceID, req.RequestID, "entity cannot be nil")
	}

	exists, err := c.entityExists(ctx, req.Entity.ID)
	if err != nil {
		return marshalCreateEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("existence check failed: %v", err))
	}
	if exists {
		return marshalCreateEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("entity already exists: %s", req.Entity.ID))
	}

	if err := c.CreateEntity(ctx, req.Entity); err != nil {
		return marshalCreateEntityError(req.TraceID, req.RequestID, err.Error())
	}

	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		return marshalCreateEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("post-write read-back failed: %v", err))
	}

	return json.Marshal(graph.CreateEntityResponse{
		MutationResponse: graph.MutationResponse{
			Success:    true,
			Timestamp:  time.Now().UnixNano(),
			KVRevision: rev,
			TraceID:    req.TraceID,
			RequestID:  req.RequestID,
		},
		Entity: stored,
	})
}

// handleEntityCreateWithTriples is handleEntityCreate that also accepts
// a Triples slice in the request envelope. When Triples is non-empty
// it REPLACES Entity.Triples before the write (the canonical set is
// the request's Triples; Entity carries provenance metadata). When
// Triples is nil/empty, Entity.Triples is written as-is.
//
// TriplesAdded in the response is the count on the *stored* entity
// after the write, which may exceed len(req.Triples) when the
// framework injects hierarchy / referential-integrity triples per
// component.go:CreateEntity. Callers reasoning about provenance
// should compare req.Triples against stored.Triples explicitly rather
// than treating TriplesAdded as authoritative.
func (c *Component) handleEntityCreateWithTriples(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.CreateEntityWithTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return marshalCreateEntityWithTriplesError(req.TraceID, req.RequestID,
			fmt.Sprintf("invalid request: %v", err))
	}
	if req.Entity == nil {
		return marshalCreateEntityWithTriplesError(req.TraceID, req.RequestID, "entity cannot be nil")
	}

	exists, err := c.entityExists(ctx, req.Entity.ID)
	if err != nil {
		return marshalCreateEntityWithTriplesError(req.TraceID, req.RequestID,
			fmt.Sprintf("existence check failed: %v", err))
	}
	if exists {
		return marshalCreateEntityWithTriplesError(req.TraceID, req.RequestID,
			fmt.Sprintf("entity already exists: %s", req.Entity.ID))
	}

	if len(req.Triples) > 0 {
		req.Entity.Triples = req.Triples
	}

	if err := c.CreateEntity(ctx, req.Entity); err != nil {
		return marshalCreateEntityWithTriplesError(req.TraceID, req.RequestID, err.Error())
	}

	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		return marshalCreateEntityWithTriplesError(req.TraceID, req.RequestID,
			fmt.Sprintf("post-write read-back failed: %v", err))
	}

	return json.Marshal(graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:    true,
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
		return marshalUpdateEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("invalid request: %v", err))
	}
	if req.Entity == nil {
		return marshalUpdateEntityError(req.TraceID, req.RequestID, "entity cannot be nil")
	}

	_, currentRev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return marshalUpdateEntityError(req.TraceID, req.RequestID,
				fmt.Sprintf("entity not found: %s", req.Entity.ID))
		}
		return marshalUpdateEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("fetch current entity failed: %v", err))
	}

	if err := c.updateEntityAtRevision(ctx, req.Entity, currentRev); err != nil {
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return marshalUpdateEntityError(req.TraceID, req.RequestID,
				fmt.Sprintf("entity not found: %s (concurrent modification or delete)", req.Entity.ID))
		}
		return marshalUpdateEntityError(req.TraceID, req.RequestID, err.Error())
	}

	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		return marshalUpdateEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("post-write read-back failed: %v", err))
	}

	return json.Marshal(graph.UpdateEntityResponse{
		MutationResponse: graph.MutationResponse{
			Success:    true,
			Timestamp:  time.Now().UnixNano(),
			KVRevision: rev,
			TraceID:    req.TraceID,
			RequestID:  req.RequestID,
		},
		Entity:  stored,
		Version: int64(stored.Version),
	})
}

// handleEntityUpdateWithTriples applies a triple-set delta to an
// existing entity: appends AddTriples, removes triples whose Predicate
// is named in RemoveTriples, and writes the result via CAS. Entity
// metadata (MessageType, Version, StorageRef) from the request also
// flows through.
//
// PR-A: the read-modify-write sequence is CAS-protected against
// concurrent delete (Update at the read revision fails with
// ErrKVRevisionMismatch if the entity was modified or deleted
// in-between). A concurrent triple add/remove from another writer
// also surfaces as the same conflict, which the caller may retry.
// The merge logic itself runs on stale data when a conflict happens
// — that's the residual scope semconnect Stage 27 flagged and PR-B
// will close by moving the delta apply inside an UpdateWithRetry
// loop, eliminating the partial-erasure window in the
// delete-all + re-add downstream shim.
//
// Unknown predicates in RemoveTriples are silently ignored (not
// "404'd"); the contract is "remove if present" not "predicate must
// exist".
func (c *Component) handleEntityUpdateWithTriples(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.UpdateEntityWithTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return marshalUpdateEntityWithTriplesError(req.TraceID, req.RequestID,
			fmt.Sprintf("invalid request: %v", err))
	}
	if req.Entity == nil {
		return marshalUpdateEntityWithTriplesError(req.TraceID, req.RequestID, "entity cannot be nil")
	}

	current, currentRev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return marshalUpdateEntityWithTriplesError(req.TraceID, req.RequestID,
				fmt.Sprintf("entity not found: %s", req.Entity.ID))
		}
		return marshalUpdateEntityWithTriplesError(req.TraceID, req.RequestID,
			fmt.Sprintf("fetch current entity failed: %v", err))
	}

	// Build the merged triple set: start from current.Triples, drop
	// any whose Predicate appears in RemoveTriples, then append
	// req.AddTriples. The merged set replaces req.Entity.Triples so
	// metadata fields (MessageType, Version, StorageRef) flow through.
	removed := 0
	if len(req.RemoveTriples) > 0 {
		removeSet := make(map[string]struct{}, len(req.RemoveTriples))
		for _, p := range req.RemoveTriples {
			removeSet[p] = struct{}{}
		}
		kept := make([]message.Triple, 0, len(current.Triples))
		for _, t := range current.Triples {
			if _, drop := removeSet[t.Predicate]; drop {
				removed++
				continue
			}
			kept = append(kept, t)
		}
		current.Triples = kept
	}
	if len(req.AddTriples) > 0 {
		current.Triples = append(current.Triples, req.AddTriples...)
	}

	req.Entity.Triples = current.Triples

	if err := c.updateEntityAtRevision(ctx, req.Entity, currentRev); err != nil {
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return marshalUpdateEntityWithTriplesError(req.TraceID, req.RequestID,
				fmt.Sprintf("entity not found: %s (concurrent modification or delete)", req.Entity.ID))
		}
		return marshalUpdateEntityWithTriplesError(req.TraceID, req.RequestID, err.Error())
	}

	stored, rev, err := c.fetchEntityState(ctx, req.Entity.ID)
	if err != nil {
		return marshalUpdateEntityWithTriplesError(req.TraceID, req.RequestID,
			fmt.Sprintf("post-write read-back failed: %v", err))
	}

	return json.Marshal(graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:    true,
			Timestamp:  time.Now().UnixNano(),
			KVRevision: rev,
			TraceID:    req.TraceID,
			RequestID:  req.RequestID,
		},
		Entity:         stored,
		TriplesAdded:   len(req.AddTriples),
		TriplesRemoved: removed,
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
		return marshalDeleteEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("invalid request: %v", err))
	}
	if req.EntityID == "" {
		return marshalDeleteEntityError(req.TraceID, req.RequestID, "entity_id cannot be empty")
	}

	existed, err := c.entityExists(ctx, req.EntityID)
	if err != nil {
		return marshalDeleteEntityError(req.TraceID, req.RequestID,
			fmt.Sprintf("existence check failed: %v", err))
	}

	if err := c.DeleteEntity(ctx, req.EntityID); err != nil {
		return marshalDeleteEntityError(req.TraceID, req.RequestID, err.Error())
	}

	return json.Marshal(graph.DeleteEntityResponse{
		MutationResponse: graph.MutationResponse{
			Success:   true,
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

// Per-response-shape error marshalers. The body-prefix error
// convention (feedback_natsclient_error_payload_convention) lives in
// these helpers — they each emit a Success=false response with the
// caller's TraceID/RequestID preserved.

func marshalCreateEntityError(traceID, requestID, msg string) ([]byte, error) {
	return json.Marshal(graph.CreateEntityResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			Error:     msg,
			Timestamp: time.Now().UnixNano(),
			TraceID:   traceID,
			RequestID: requestID,
		},
	})
}

func marshalCreateEntityWithTriplesError(traceID, requestID, msg string) ([]byte, error) {
	return json.Marshal(graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			Error:     msg,
			Timestamp: time.Now().UnixNano(),
			TraceID:   traceID,
			RequestID: requestID,
		},
	})
}

func marshalUpdateEntityError(traceID, requestID, msg string) ([]byte, error) {
	return json.Marshal(graph.UpdateEntityResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			Error:     msg,
			Timestamp: time.Now().UnixNano(),
			TraceID:   traceID,
			RequestID: requestID,
		},
	})
}

func marshalUpdateEntityWithTriplesError(traceID, requestID, msg string) ([]byte, error) {
	return json.Marshal(graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			Error:     msg,
			Timestamp: time.Now().UnixNano(),
			TraceID:   traceID,
			RequestID: requestID,
		},
	})
}

func marshalDeleteEntityError(traceID, requestID, msg string) ([]byte, error) {
	return json.Marshal(graph.DeleteEntityResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			Error:     msg,
			Timestamp: time.Now().UnixNano(),
			TraceID:   traceID,
			RequestID: requestID,
		},
	})
}
