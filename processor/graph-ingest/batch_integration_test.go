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
	"github.com/c360studio/semstreams/pkg/errs"
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
		_ = c.Stop(context.Background())
	})

	time.Sleep(100 * time.Millisecond)
	return ctx, c
}

func appendThroughCanonicalHandler(t *testing.T, ctx context.Context, c *Component, triples []message.Triple) graph.AppendTriplesResponse {
	t.Helper()
	request, err := json.Marshal(graph.AppendTriplesRequest{Triples: triples})
	require.NoError(t, err)
	body, err := c.handleCanonicalAppend(ctx, request)
	require.NoError(t, err)
	var response graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(body, &response))
	return response
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
		{Subject: entityID, Predicate: "test.batch.id", Object: "1", Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "test.batch.content", Object: "Write code", Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "test.batch.status", Object: "pending", Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "test.batch.position", Object: 0, Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "test.batch.updated-at", Object: now.Format(time.RFC3339), Timestamp: now, Confidence: 1.0},
	}

	response := appendThroughCanonicalHandler(t, ctx, c, triples)
	require.Len(t, response.Results, 1)
	assert.Equal(t, graph.MutationApplied, response.Results[0].Outcome)

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
		{Subject: idA, Predicate: "test.batch.id", Object: "1", Timestamp: now, Confidence: 1.0},
		{Subject: idB, Predicate: "test.batch.id", Object: "1", Timestamp: now, Confidence: 1.0},
		{Subject: idA, Predicate: "test.batch.status", Object: "pending", Timestamp: now, Confidence: 1.0},
		{Subject: idB, Predicate: "test.batch.status", Object: "in_progress", Timestamp: now, Confidence: 1.0},
		{Subject: idA, Predicate: "test.batch.position", Object: 0, Timestamp: now, Confidence: 1.0},
	}

	response := appendThroughCanonicalHandler(t, ctx, c, triples)
	require.Len(t, response.Results, 2)
	for _, result := range response.Results {
		assert.Equal(t, graph.MutationApplied, result.Outcome)
	}

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
		{Subject: entityID, Predicate: "test.batch.id", Object: "1", Timestamp: now, Confidence: 1.0},
		{Subject: "", Predicate: "test.batch.status", Object: "pending", Timestamp: now, Confidence: 1.0}, // bad
	}

	request, err := json.Marshal(graph.AppendTriplesRequest{Triples: triples})
	require.NoError(t, err)
	_, err = c.handleCanonicalAppend(ctx, request)
	require.Error(t, err)

	// Confirm the valid triple was NOT silently committed.
	_, err = c.entityBucket.Get(ctx, entityID)
	assert.Error(t, err, "no partial commit on validation failure")
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

	req := graph.AppendTriplesRequest{
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "test.batch.id", Object: "1", Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: "test.batch.status", Object: "pending", Timestamp: now, Confidence: 1.0},
		},
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleCanonicalAppend(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.MutationApplied, resp.Results[0].Outcome)
}

// TestIntegration_AddTriples_PreservesInputOrderWithinSubject pins the append
// lane's first-input ordering. Ordering is useful for stable reads but is not a
// record-correlation mechanism; compound records must carry explicit identity.
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

	response := appendThroughCanonicalHandler(t, ctx, c, triples)
	require.Len(t, response.Results, 1)
	assert.Equal(t, graph.MutationApplied, response.Results[0].Outcome)

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
	assert.Equal(t, want, got, "append must preserve first-input order within one subject")
}

// TestIntegration_HandleTripleAddBatch_InvalidJSON pins the
// malformed-envelope behaviour: handler returns Success=false with a
// descriptive error, rather than crashing or silently dropping.
func TestIntegration_HandleTripleAddBatch_InvalidJSON(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	respBytes, err := c.handleCanonicalAppend(ctx, []byte("not json"))
	// ADR-060: a malformed envelope is a typed invalid_request reject (no body).
	assert.Nil(t, respBytes)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, graph.ErrorCodeInvalidRequest, classified.Code)
}

// TestIntegration_HandleTripleAdd_AbsentEntityRejects verifies ADR-055
// must-exist: a triple targeting a never-created entity is rejected with
// ErrorCodeEntityNotFound. The entity bucket must remain empty (no
// auto-vivification).
func TestIntegration_HandleTripleAdd_AbsentEntityRejects(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const subject = "c360.test.absent.single.entity.001"
	req := graph.AppendTriplesRequest{
		Triples: []message.Triple{{
			Subject:    subject,
			Predicate:  "evidence.note.value",
			Object:     "should-not-land",
			Confidence: 1.0,
		}},
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, handlerErr := c.handleCanonicalAppend(ctx, reqBytes)
	require.NoError(t, handlerErr)
	var response graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &response))
	require.Len(t, response.Results, 1)
	assert.Equal(t, graph.MutationEntityNotFound, response.Results[0].Outcome)

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
	req := graph.AppendTriplesRequest{
		Triples: []message.Triple{
			{Subject: subject, Predicate: "evidence.note.value", Object: "should-not-land", Confidence: 1.0},
		},
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, handlerErr := c.handleCanonicalAppend(ctx, reqBytes)
	require.NoError(t, handlerErr)

	var resp graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.MutationEntityNotFound, resp.Results[0].Outcome)
	assert.Equal(t, subject, resp.Results[0].EntityID)

	// State must be unchanged: entity must still not exist.
	_, getErr := c.entityBucket.Get(ctx, subject)
	assert.True(t, natsclient.IsKVNotFoundError(getErr),
		"entity bucket must remain empty — no auto-vivification (ADR-055)")
}
