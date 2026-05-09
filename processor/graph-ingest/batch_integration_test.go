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

	// Single CAS → entity version increments by exactly 1, not 5.
	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)

	var entity graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &entity))

	assert.Equal(t, 5, len(entity.Triples), "all 5 triples present")
	assert.Equal(t, uint64(1), entity.Version, "single CAS commit, version=1 (not 5)")
}

// TestIntegration_AddTriples_MultiSubjectGroupsByEntity verifies multi-subject
// batches issue one CAS per entity, not one per triple. Two entities × N
// triples each → 2 CAS round-trips, each entity reaches version=1.
func TestIntegration_AddTriples_MultiSubjectGroupsByEntity(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const idA = "c360.test.batch.multi.loop.a01"
	const idB = "c360.test.batch.multi.loop.b02"
	now := time.Now()

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
	assert.Equal(t, 3, len(entityA.Triples))
	assert.Equal(t, uint64(1), entityA.Version)

	entryB, err := c.entityBucket.Get(ctx, idB)
	require.NoError(t, err)
	var entityB graph.EntityState
	require.NoError(t, json.Unmarshal(entryB.Value, &entityB))
	assert.Equal(t, 2, len(entityB.Triples))
	assert.Equal(t, uint64(1), entityB.Version)
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
	require.Len(t, entity.Triples, len(want))

	got := make([]string, len(entity.Triples))
	for i, tr := range entity.Triples {
		s, ok := tr.Object.(string)
		if !ok {
			t.Fatalf("triple[%d].Object expected string, got %T", i, tr.Object)
		}
		got[i] = s
	}
	assert.Equal(t, want, got, "ADR-036 Stage 4 ReconstructTodos parses by stride; input order must be preserved")
}

// TestIntegration_HandleTripleAddBatch_InvalidJSON pins the
// malformed-envelope behaviour: handler returns Success=false with a
// descriptive error, rather than crashing or silently dropping.
func TestIntegration_HandleTripleAddBatch_InvalidJSON(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	respBytes, err := c.handleTripleAddBatch(ctx, []byte("not json"))
	require.NoError(t, err)

	var resp graph.AddTriplesBatchResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "invalid request")
}
