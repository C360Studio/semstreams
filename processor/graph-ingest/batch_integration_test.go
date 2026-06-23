//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startBatchTestComponent boots a graph-ingest Component against a real
// JetStream test cluster and returns it ready for use. Mirrors the
// CAS-integration-test setup so behavioural drift between the two paths
// is easy to spot at review time.
func startBatchTestComponent(t *testing.T) (context.Context, *Component) {
	t.Helper()
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	config := DefaultConfig()
	deps := component.Dependencies{NATSClient: natsClient}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, deps)
	require.NoError(t, err)

	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() {
		_ = c.Stop(5 * time.Second)
	})

	time.Sleep(100 * time.Millisecond)
	return ctx, c
}

// TestIntegration_AddTriples_SingleSubjectIsOneCAS pins the load-bearing
// optimisation behind ADR-036 §Stage 2: many triples sharing one Subject
// commit in a single CAS round-trip. Failure shows up as multiple
// version increments (one per triple) instead of one.
func TestIntegration_AddTriples_SingleSubjectIsOneCAS(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.batch.single.loop.001"
	now := time.Now()

	// ADR-055: pre-create the entity before adding triples (must-exist).
	require.NoError(t, c.MergeEntity(ctx, &graph.EntityState{ID: entityID}))

	// Capture version baseline immediately after pre-create so we can assert
	// AddTriples applies exactly one CAS increment (not one per triple).
	preEntry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)
	var preEntity graph.EntityState
	require.NoError(t, json.Unmarshal(preEntry.Value, &preEntity))
	baseVersion := preEntity.Version
	baseTripleCount := len(preEntity.Triples)

	triples := []message.Triple{
		{Subject: entityID, Predicate: "agent.todo.id", Object: "1", Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "agent.todo.content", Object: "Write code", Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "agent.todo.status", Object: "pending", Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "agent.todo.position", Object: 0, Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "agent.todo.updated_at", Object: now.Format(time.RFC3339), Timestamp: now, Confidence: 1.0},
	}

	written, failed, err := c.AddTriples(ctx, triples)
	require.NoError(t, err)
	require.Empty(t, failed)
	assert.Equal(t, 5, written)

	// Single CAS → version increments by exactly 1 (not 5 — one per triple).
	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)

	var entity graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &entity))

	assert.Equal(t, baseTripleCount+5, len(entity.Triples), "all 5 triples appended to pre-created entity")
	assert.Equal(t, baseVersion+1, entity.Version, "single CAS commit: version incremented by exactly 1 (not 5)")
}

// TestIntegration_AddTriples_MultiSubjectGroupsByEntity verifies multi-subject
// batches issue one CAS per entity, not one per triple. Two entities × N
// triples each → 2 CAS round-trips (1 create + 1 batch add per entity).
func TestIntegration_AddTriples_MultiSubjectGroupsByEntity(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const idA = "c360.test.batch.multi.loop.a01"
	const idB = "c360.test.batch.multi.loop.b02"
	now := time.Now()

	// ADR-055: pre-create both entities before adding triples (must-exist).
	require.NoError(t, c.MergeEntity(ctx, &graph.EntityState{ID: idA}))
	require.NoError(t, c.MergeEntity(ctx, &graph.EntityState{ID: idB}))

	// Capture baseline versions and triple counts after pre-create.
	preA, err := c.entityBucket.Get(ctx, idA)
	require.NoError(t, err)
	var preEntityA graph.EntityState
	require.NoError(t, json.Unmarshal(preA.Value, &preEntityA))
	preB, err := c.entityBucket.Get(ctx, idB)
	require.NoError(t, err)
	var preEntityB graph.EntityState
	require.NoError(t, json.Unmarshal(preB.Value, &preEntityB))

	triples := []message.Triple{
		{Subject: idA, Predicate: "agent.todo.id", Object: "1", Timestamp: now, Confidence: 1.0},
		{Subject: idB, Predicate: "agent.todo.id", Object: "1", Timestamp: now, Confidence: 1.0},
		{Subject: idA, Predicate: "agent.todo.status", Object: "pending", Timestamp: now, Confidence: 1.0},
		{Subject: idB, Predicate: "agent.todo.status", Object: "in_progress", Timestamp: now, Confidence: 1.0},
		{Subject: idA, Predicate: "agent.todo.position", Object: 0, Timestamp: now, Confidence: 1.0},
	}

	written, failed, err := c.AddTriples(ctx, triples)
	require.NoError(t, err)
	require.Empty(t, failed)
	assert.Equal(t, 5, written)

	entryA, err := c.entityBucket.Get(ctx, idA)
	require.NoError(t, err)
	var entityA graph.EntityState
	require.NoError(t, json.Unmarshal(entryA.Value, &entityA))
	assert.Equal(t, len(preEntityA.Triples)+3, len(entityA.Triples))
	// Each entity version incremented by exactly 1 (single batched CAS per entity).
	assert.Equal(t, preEntityA.Version+1, entityA.Version)

	entryB, err := c.entityBucket.Get(ctx, idB)
	require.NoError(t, err)
	var entityB graph.EntityState
	require.NoError(t, json.Unmarshal(entryB.Value, &entityB))
	assert.Equal(t, len(preEntityB.Triples)+2, len(entityB.Triples))
	assert.Equal(t, preEntityB.Version+1, entityB.Version)
}

// TestIntegration_AddTriples_ValidationRejectsWholeBatch ensures a single
// malformed triple fails the entire batch before any CAS — partial
// validation would be surprising.
func TestIntegration_AddTriples_ValidationRejectsWholeBatch(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.batch.invalid.loop.001"
	now := time.Now()

	triples := []message.Triple{
		{Subject: entityID, Predicate: "agent.todo.id", Object: "1", Timestamp: now, Confidence: 1.0},
		{Subject: "", Predicate: "agent.todo.status", Object: "pending", Timestamp: now, Confidence: 1.0}, // bad
	}

	written, failed, err := c.AddTriples(ctx, triples)
	require.Error(t, err)
	assert.Empty(t, failed, "pre-CAS validation failure has no FailedSubjects")
	assert.Equal(t, 0, written)

	// Confirm the valid triple was NOT silently committed.
	_, err = c.entityBucket.Get(ctx, entityID)
	assert.Error(t, err, "no partial commit on validation failure")
}

// TestIntegration_AddTriples_EmptyBatchIsNoop covers the degenerate case.
func TestIntegration_AddTriples_EmptyBatchIsNoop(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	written, failed, err := c.AddTriples(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, failed)
	assert.Equal(t, 0, written)
}

// TestIntegration_HandleTripleAddBatch_RoundTrip exercises the
// NATS-handler path end-to-end via direct invocation of the handler
// callback (the same shape natsclient.SubscribeForRequests delivers).
// Validates the wire-format envelope, success/failure flag, and
// FailedSubjects population.
func TestIntegration_HandleTripleAddBatch_RoundTrip(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.batch.handler.loop.001"
	now := time.Now()

	// ADR-055: pre-create the entity before adding triples (must-exist).
	require.NoError(t, c.MergeEntity(ctx, &graph.EntityState{ID: entityID}))

	req := graph.AddTriplesBatchRequest{
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "agent.todo.id", Object: "1", Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: "agent.todo.status", Object: "pending", Timestamp: now, Confidence: 1.0},
		},
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleTripleAddBatch(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.AddTriplesBatchResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.True(t, resp.Success)
	assert.Empty(t, resp.Error)
	assert.Equal(t, 2, resp.WrittenCount)
	assert.Empty(t, resp.FailedSubjects)
}

// TestIntegration_AddTriples_PreservesInputOrderWithinSubject pins the
// invariant that ADR-036 Stage 4's prompt assembler depends on:
// triples emitted in input order are stored in input order on the
// entity. write_todos writes [id, content, status, position,
// updated_at] interleaved per item; ReconstructTodos parses the
// stored slice in stride-of-5 to recover items. If a future
// graph-ingest change reorders, dedups, or sorts triples on write
// (e.g. for query efficiency), this test fails loudly so the change
// also needs an ADR-036 Stage 4 update.
func TestIntegration_AddTriples_PreservesInputOrderWithinSubject(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.batch.order.loop.001"
	now := time.Now()

	// ADR-055: pre-create the entity before adding triples (must-exist).
	require.NoError(t, c.MergeEntity(ctx, &graph.EntityState{ID: entityID}))

	// Emit a known sequence: A, B, C, D, E across the same subject.
	// The retrieved Triples slice must come back in this exact order.
	want := []string{"A", "B", "C", "D", "E"}
	triples := make([]message.Triple, 0, len(want))
	for _, label := range want {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  "test.order.label",
			Object:     label,
			Timestamp:  now,
			Confidence: 1.0,
		})
	}

	written, failed, err := c.AddTriples(ctx, triples)
	require.NoError(t, err)
	require.Empty(t, failed)
	assert.Equal(t, len(want), written)

	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)

	var entity graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &entity))

	// Collect only the test.order.label triples in their stored order.
	// MergeEntity may have stamped additional framework triples (e.g. indexing
	// profile); we filter to the predicate we care about to stay robust.
	got := make([]string, 0, len(want))
	for _, tr := range entity.Triples {
		if tr.Predicate != "test.order.label" {
			continue
		}
		s, ok := tr.Object.(string)
		if !ok {
			t.Fatalf("triple with predicate test.order.label: Object expected string, got %T", tr.Object)
		}
		got = append(got, s)
	}
	require.Len(t, got, len(want), "exactly %d test.order.label triples must be present", len(want))
	assert.Equal(t, want, got, "ADR-036 Stage 4 ReconstructTodos parses by stride; input order must be preserved")
}

// TestIntegration_HandleTripleAddBatch_InvalidJSON pins the
// malformed-envelope behaviour: handler returns Success=false with a
// descriptive error, rather than crashing or silently dropping.
func TestIntegration_HandleTripleAddBatch_InvalidJSON(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	respBytes, err := c.handleTripleAddBatch(ctx, []byte("not json"))
	// ADR-060: a malformed envelope is a typed invalid_request reject (no body).
	requireClassifiedReject(t, respBytes, err, graph.ErrorCodeInvalidRequest, "invalid request")
}

// TestIntegration_HandleTripleAdd_AbsentEntityRejects verifies ADR-055
// must-exist: a triple targeting a never-created entity is rejected with
// ErrorCodeEntityNotFound. The entity bucket must remain empty (no
// auto-vivification).
func TestIntegration_HandleTripleAdd_AbsentEntityRejects(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const subject = "c360.test.absent.single.entity.001"
	req := graph.AddTripleRequest{
		Triple: message.Triple{
			Subject:    subject,
			Predicate:  "evidence.note",
			Object:     "should-not-land",
			Confidence: 1.0,
		},
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, handlerErr := c.handleTripleAdd(ctx, reqBytes)
	// ADR-060: a single must-exist add on an absent entity is a typed
	// entity_not_found reject (invalid class), no longer an in-body Success=false.
	requireClassifiedReject(t, respBytes, handlerErr, graph.ErrorCodeEntityNotFound, "")

	// State must be unchanged: entity must still not exist.
	_, getErr := c.entityBucket.Get(ctx, subject)
	assert.True(t, natsclient.IsKVNotFoundError(getErr),
		"entity bucket must remain empty — no auto-vivification (ADR-055)")
}

// TestIntegration_HandleTripleAddBatch_AbsentEntityRejects verifies ADR-055
// must-exist for the batch handler: an all-absent batch is rejected with
// ErrorCodeEntityNotFound, FailedSubjects names the subject, WrittenCount is 0,
// and the entity bucket remains empty.
func TestIntegration_HandleTripleAddBatch_AbsentEntityRejects(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const subject = "c360.test.absent.batch.entity.001"
	req := graph.AddTriplesBatchRequest{
		Triples: []message.Triple{
			{Subject: subject, Predicate: "evidence.note", Object: "should-not-land", Confidence: 1.0},
		},
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, handlerErr := c.handleTripleAddBatch(ctx, reqBytes)
	require.NoError(t, handlerErr, "handler must not return a Go error; rejections are in the response body")

	var resp graph.AddTriplesBatchResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.False(t, resp.Success, "absent entity must produce Success=false")
	assert.Equal(t, graph.ErrorCodeEntityNotFound, resp.ErrorCode,
		"all-absent batch must produce ErrorCode=entity_not_found")
	assert.Equal(t, 0, resp.WrittenCount, "no triples must be written for an absent entity")
	require.Len(t, resp.FailedSubjects, 1, "FailedSubjects must name the one failing subject")
	assert.Contains(t, resp.FailedSubjects, subject, "FailedSubjects must include the absent subject")

	// State must be unchanged: entity must still not exist.
	_, getErr := c.entityBucket.Get(ctx, subject)
	assert.True(t, natsclient.IsKVNotFoundError(getErr),
		"entity bucket must remain empty — no auto-vivification (ADR-055)")
}
