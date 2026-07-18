package graphingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
)

// The structural-identity predicate gate (enforce-structural-invariants) rejects
// non-3-part predicates at the mutation-handler boundary. It is unconditionally
// FAIL-CLOSED — no bypass configuration exists: validateTriplePredicates returns
// a classified ErrorCodeStructuralInvalid so the write is rejected before any
// KV I/O. Metering happens exactly ONCE, in the meteredMutation wrapper, which
// meters every classified handler error by its code
// (mutation_rejections{reason=structural_invalid}) and logs a loud Warn whose
// detail names the predicate — the gate itself does not meter (a direct call
// would double-count the same rejection under a second reason label).
//
// Lane map (see validateTriplePredicates' doc comment):
//   - triple.add / triple.add_batch — this gate is the FIRST predicate authority.
//   - create_with_triples / update_with_triples / Graphable ingest — the
//     authoritative entity-state contract validation
//     (graph.ValidateEntityStateContract) fires first with the generic
//     invalid_request classification; the gate is a backstop.

const structuralGateEntity = "acme.ops.robotics.gcs.drone.001"

// structuralGateTestType is the synthetic envelope type for gate tests.
var structuralGateTestType = message.Type{Domain: "test", Category: "mutation", Version: "v1"}

// seedStructuralGateEntity creates a conforming entity and returns its stored
// baseline state (triples may include framework-injected ones), so tests assert
// "unchanged" against what the store actually holds post-seed.
func seedStructuralGateEntity(t *testing.T, comp *Component, entityID string) *graph.EntityState {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, comp.CreateEntity(ctx, &graph.EntityState{
		ID:          entityID,
		MessageType: structuralGateTestType,
		Version:     1,
		UpdatedAt:   now,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "sensor.temperature.celsius", Object: 22.5, Timestamp: now, Confidence: 1.0},
		},
	}))
	stored, _, err := comp.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	return stored
}

// TestValidateTriplePredicates_FailClosed_RejectsClassified pins the
// always-fail-closed contract: the gate rejects a non-3-part predicate with the
// classified structural code. No bypass configuration exists (the observe-only
// escape hatch prototyped during enforce-structural-invariants was removed
// pre-release as provably inert — the authoritative persistence seam rejects
// unconditionally, so the hatch could only swap the caller-visible error code).
func TestValidateTriplePredicates_FailClosed_RejectsClassified(t *testing.T) {
	comp := createTestComponentWithMockKV(t)

	triples := []message.Triple{
		{Subject: structuralGateEntity, Predicate: "agent.role", Object: "researcher"}, // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.role","reason":"arity"}
	}
	err := comp.validateTriplePredicates(triples)
	require.Error(t, err, "the gate must reject a non-3-part predicate")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeStructuralInvalid, ce.Code,
		"a structural rejection carries ErrorCodeStructuralInvalid")
}

func TestValidateTriplePredicates_ValidPredicate_Untouched(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	// A conforming predicate must pass the fail-closed gate untouched. (Metering
	// on rejection is the meteredMutation wrapper's job — asserted in the
	// handler-level tests below.)

	triples := []message.Triple{
		{Subject: structuralGateEntity, Predicate: "sensor.temperature.celsius", Object: 22.5},
		// gh#519 collision guard: a valid 3-part predicate whose last segment is
		// literally "value" MUST pass (it is a real predicate, not a suffix).
		{Subject: structuralGateEntity, Predicate: "sensorml.capability.value", Object: "50m"},
	}
	err := comp.validateTriplePredicates(triples)
	require.NoError(t, err, "conforming 3-part predicates pass the fail-closed gate")
}

// TestHandleTripleAdd_InvalidPredicate_FailClosedNothingPersisted drives the
// production handler through the meteredMutation wrapper (the exact chain
// SubscribeForRequests registers) and pins the full fail-closed contract on the
// triple.add lane: classified structural_invalid error, rejection metric, loud
// Warn naming the token, and NOTHING persisted. The target entity is absent, so
// the gate must fire BEFORE the must-exist check (structural_invalid, not
// entity_not_found) and before any KV write.
func TestHandleTripleAdd_InvalidPredicate_FailClosedNothingPersisted(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	var logBuf bytes.Buffer
	comp.logger = slog.New(slog.NewTextHandler(&logBuf, nil))

	const absentID = "acme.ops.robotics.gcs.drone.404"
	// The meteredMutation wrapper is the SINGLE metering point: it meters the
	// classified error by its code (reason=structural_invalid). The gate's old
	// direct-meter cell must stay untouched (double-metering regression guard).
	counter := comp.mutationRejections.WithLabelValues(SubjectTripleAdd, graph.ErrorCodeStructuralInvalid)
	before := testutil.ToFloat64(counter)
	gateDirectCell := comp.mutationRejections.WithLabelValues(SubjectTripleAdd, "structural_predicate_invalid")
	gateDirectBefore := testutil.ToFloat64(gateDirectCell)

	handler := comp.meteredMutation(SubjectTripleAdd, comp.handleTripleAdd)
	reqBytes, err := json.Marshal(graph.AddTripleRequest{Triple: message.Triple{
		Subject: absentID, Predicate: "agent.role", Object: "researcher", // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.role","reason":"arity"}
		Timestamp: time.Now(), Confidence: 1.0,
	}})
	require.NoError(t, err)

	resp, err := handler(ctx, reqBytes)
	require.Error(t, err, "fail-closed gate must reject the mutation")
	assert.Nil(t, resp, "a hard rejection carries no success body (ADR-060)")

	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeStructuralInvalid, ce.Code,
		"the gate fires before the must-exist check: structural_invalid, not entity_not_found")
	assert.True(t, errs.IsInvalid(err), "structural rejection classifies invalid (do-not-retry)")

	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"mutation_rejections{subject=triple.add, reason=structural_invalid} must increment exactly once")
	assert.InDelta(t, gateDirectBefore, testutil.ToFloat64(gateDirectCell), 0.0001,
		"the gate must NOT meter directly — one rejection, one metric cell (no double-metering)")

	_, getErr := comp.entityBucket.Get(ctx, absentID)
	require.Error(t, getErr, "nothing may be persisted for the rejected mutation")
	assert.True(t, errors.Is(getErr, natsclient.ErrKVKeyNotFound),
		"the target entity must remain absent, got: %v", getErr)

	logged := logBuf.String()
	assert.Contains(t, logged, "graph mutation rejected", "a loud Warn must be emitted")
	assert.Contains(t, logged, "agent.role", "the Warn must name the offending predicate")
	assert.Contains(t, logged, SubjectTripleAdd, "the Warn must name the source subject")
	assert.Contains(t, logged, graph.ErrorCodeStructuralInvalid, "the Warn must name the reason")
}

// TestHandleTripleAddBatch_InvalidPredicate_WholeBatchRejected pins D2's
// reject-the-whole-mutation decision on the batch lane: one malformed predicate
// rejects the ENTIRE batch — the conforming triple in the same batch must NOT
// be persisted either.
func TestHandleTripleAddBatch_InvalidPredicate_WholeBatchRejected(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	baseline := seedStructuralGateEntity(t, comp, structuralGateEntity)
	now := time.Now()

	handler := comp.meteredMutation(SubjectTripleAddBatch, comp.handleTripleAddBatch)
	reqBytes, err := json.Marshal(graph.AddTriplesBatchRequest{Triples: []message.Triple{
		{Subject: structuralGateEntity, Predicate: "sensorml.capability.value", Object: "50m", Timestamp: now, Confidence: 1.0},
		{Subject: structuralGateEntity, Predicate: "agent.role", Object: "researcher", Timestamp: now, Confidence: 1.0}, // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.role","reason":"arity"}
	}})
	require.NoError(t, err)

	resp, err := handler(ctx, reqBytes)
	require.Error(t, err, "one malformed predicate rejects the whole batch")
	assert.Nil(t, resp)

	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeStructuralInvalid, ce.Code)

	stored, _, err := comp.fetchEntityState(ctx, structuralGateEntity)
	require.NoError(t, err)
	assert.Equal(t, len(baseline.Triples), len(stored.Triples),
		"the conforming triple in the rejected batch must not be persisted")
	assert.Equal(t, baseline.Version, stored.Version, "entity version must be untouched")
}

// TestHandleEntityCreateWithTriples_InvalidPredicate_NothingPersisted pins the
// create_with_triples lane. On this lane the AUTHORITATIVE entity-state contract
// validation (validateMutationEntityState → graph.ValidateEntityStateContract)
// fires before the handler-level structural gate, so the caller-visible
// classification is the generic invalid_request — still a classified validation
// error, still nothing persisted, and the specific structural reason is metered
// via predicate_contract_rejections{lane,reason}.
func TestHandleEntityCreateWithTriples_InvalidPredicate_NothingPersisted(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	const createID = "acme.ops.robotics.gcs.drone.777"
	contractCounter := comp.predicateContractRejections.WithLabelValues(
		SubjectEntityCreateWithTriples, string(vocabulary.PredicateReasonArity))
	before := testutil.ToFloat64(contractCounter)

	now := time.Now()
	handler := comp.meteredMutation(SubjectEntityCreateWithTriples, comp.handleEntityCreateWithTriples)
	reqBytes, err := json.Marshal(graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: createID, MessageType: structuralGateTestType, Version: 1, UpdatedAt: now},
		Triples: []message.Triple{
			{Subject: createID, Predicate: "agent.role", Object: "researcher", Timestamp: now, Confidence: 1.0}, // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.role","reason":"arity"}
		},
	})
	require.NoError(t, err)

	resp, err := handler(ctx, reqBytes)
	require.Error(t, err, "a malformed predicate rejects the create in full")
	assert.Nil(t, resp)

	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeInvalidRequest, ce.Code,
		"the authoritative contract seam precedes the gate on this lane (invalid_request)")
	assert.True(t, errs.IsInvalid(err))
	assert.Contains(t, err.Error(), "agent.role", "the error must name the offending predicate")

	assert.InDelta(t, before+1, testutil.ToFloat64(contractCounter), 0.0001,
		"predicate_contract_rejections{lane=create_with_triples, reason=arity} must increment")

	_, getErr := comp.entityBucket.Get(ctx, createID)
	require.Error(t, getErr, "nothing may be persisted for the rejected create")
	assert.True(t, errors.Is(getErr, natsclient.ErrKVKeyNotFound))
}

// TestHandleEntityUpdateWithTriples_InvalidPredicate_EntityUnchanged pins the
// update_with_triples lane: a malformed predicate in the AddTriples delta
// rejects the update as a classified validation error and leaves the stored
// entity byte-identical (existing merge state untouched).
func TestHandleEntityUpdateWithTriples_InvalidPredicate_EntityUnchanged(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	baseline := seedStructuralGateEntity(t, comp, structuralGateEntity)
	now := time.Now()

	handler := comp.meteredMutation(SubjectEntityUpdateWithTriples, comp.handleEntityUpdateWithTriples)
	reqBytes, err := json.Marshal(graph.UpdateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: structuralGateEntity, MessageType: structuralGateTestType, Version: baseline.Version + 1},
		AddTriples: []message.Triple{
			{Subject: structuralGateEntity, Predicate: "agent.role", Object: "researcher", Timestamp: now, Confidence: 1.0}, // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.role","reason":"arity"}
		},
	})
	require.NoError(t, err)

	resp, err := handler(ctx, reqBytes)
	require.Error(t, err, "a malformed predicate rejects the update in full")
	assert.Nil(t, resp)

	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeInvalidRequest, ce.Code,
		"the authoritative contract seam precedes the gate on this lane (invalid_request)")
	assert.True(t, errs.IsInvalid(err))

	stored, _, err := comp.fetchEntityState(ctx, structuralGateEntity)
	require.NoError(t, err)
	assert.Equal(t, baseline.Triples, stored.Triples, "stored triples must be unchanged")
	assert.Equal(t, baseline.Version, stored.Version, "stored version must be unchanged")
}

// TestIngestEntity_InvalidPredicate_NothingPersisted pins the Graphable
// fact-arrival lane (the primary vector for product/source-authored
// predicates): prepareFactProjection's authoritative candidate validation
// rejects the malformed predicate before the merge, and nothing is persisted.
func TestIngestEntity_InvalidPredicate_NothingPersisted(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	const ingestID = "acme.ops.robotics.gcs.drone.888"
	now := time.Now()
	err := comp.ingestEntity(ctx, &graph.EntityState{
		ID:          ingestID,
		MessageType: structuralGateTestType,
		Version:     1,
		UpdatedAt:   now,
		Triples: []message.Triple{
			{Subject: ingestID, Predicate: "agent.role", Object: "researcher", Timestamp: now, Confidence: 1.0}, // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.role","reason":"arity"}
		},
	})
	require.Error(t, err, "Graphable ingest must reject a malformed predicate")
	assert.True(t, errs.IsInvalid(err), "classified invalid (do-not-retry)")
	assert.Contains(t, err.Error(), "agent.role", "the error must name the offending predicate")

	_, getErr := comp.entityBucket.Get(ctx, ingestID)
	require.Error(t, getErr, "nothing may be persisted for the rejected ingest")
	assert.True(t, errors.Is(getErr, natsclient.ErrKVKeyNotFound))
}

// TestHandleTripleAdd_ValidPredicate_PersistsMergeIntact is the regression
// guard: a fully-conforming mutation passes the structural gate and persists
// with the existing merge semantics intact (append on the entity, version
// bump, prior triples preserved). Includes the gh#519 collision case: a real
// 3-part predicate ending in the literal segment "value".
func TestHandleTripleAdd_ValidPredicate_PersistsMergeIntact(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	baseline := seedStructuralGateEntity(t, comp, structuralGateEntity)

	counter := comp.mutationRejections.WithLabelValues(SubjectTripleAdd, graph.ErrorCodeStructuralInvalid)
	before := testutil.ToFloat64(counter)

	handler := comp.meteredMutation(SubjectTripleAdd, comp.handleTripleAdd)
	reqBytes, err := json.Marshal(graph.AddTripleRequest{Triple: message.Triple{
		Subject: structuralGateEntity, Predicate: "sensorml.capability.value", Object: "50m",
		Timestamp: time.Now(), Confidence: 1.0,
	}})
	require.NoError(t, err)

	respBytes, err := handler(ctx, reqBytes)
	require.NoError(t, err, "a conforming mutation must pass the gate and persist")

	var resp graph.AddTripleResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.NotZero(t, resp.KVRevision, "success response carries the post-write revision")

	stored, _, err := comp.fetchEntityState(ctx, structuralGateEntity)
	require.NoError(t, err)
	assert.Equal(t, len(baseline.Triples)+1, len(stored.Triples),
		"the new triple appends; prior triples are preserved (merge semantics intact)")
	var seedPreserved, newPersisted bool
	for _, tr := range stored.Triples {
		if tr.Predicate == "sensor.temperature.celsius" {
			seedPreserved = true
		}
		if tr.Predicate == "sensorml.capability.value" {
			newPersisted = true
		}
	}
	assert.True(t, seedPreserved, "seed triple preserved")
	assert.True(t, newPersisted, "new triple persisted")
	assert.Greater(t, stored.Version, baseline.Version, "version bumps on write")

	assert.InDelta(t, before, testutil.ToFloat64(counter), 0.0001,
		"no structural rejection metered for a conforming mutation")
}
