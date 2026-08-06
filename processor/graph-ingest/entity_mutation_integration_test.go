//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testMutationType = message.Type{Domain: "test", Category: "mutation", Version: "v1"}

func TestIntegration_CanonicalMutationLifecycle(t *testing.T) {
	ctx, c := startBatchTestComponent(t)
	const id = "c360.test.mutation.system.widget.001"
	now := time.Now()

	create := graph.CreateEntityRequest{
		Entity:  &graph.EntityState{ID: id, MessageType: testMutationType, Version: 1, UpdatedAt: now},
		Triples: []message.Triple{{Subject: id, Predicate: "test.state.value", Object: "created", Timestamp: now, Confidence: 1}},
	}
	createData, err := json.Marshal(create)
	require.NoError(t, err)
	body, err := c.handleCanonicalCreate(ctx, createData)
	require.NoError(t, err)
	var created graph.CreateEntityResponse
	require.NoError(t, json.Unmarshal(body, &created))
	assert.Equal(t, graph.MutationApplied, created.Outcome)
	require.NotZero(t, created.KVRevision)

	_, err = c.handleCanonicalCreate(ctx, createData)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, graph.ErrorCodeEntityExists, classified.Code)

	reconcile := graph.ReconcilePredicatesRequest{
		EntityID: id, ExpectedRevision: created.KVRevision,
		Predicates: []string{"test.state.value"},
		Desired:    []message.Triple{{Subject: id, Predicate: "test.state.value", Object: "reconciled", Timestamp: now, Confidence: 1}},
	}
	reconcileData, err := json.Marshal(reconcile)
	require.NoError(t, err)
	body, err = c.handleCanonicalReconcile(ctx, reconcileData)
	require.NoError(t, err)
	var reconciled graph.ReconcilePredicatesResponse
	require.NoError(t, json.Unmarshal(body, &reconciled))
	assert.Equal(t, graph.MutationApplied, reconciled.Outcome)
	assert.Greater(t, reconciled.KVRevision, created.KVRevision)

	_, err = c.handleCanonicalReconcile(ctx, reconcileData)
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, graph.ErrorCodeRevisionMismatch, classified.Code)

	appendRequest, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{{
		Subject: id, Predicate: "test.event.value", Object: "appended", Timestamp: now, Confidence: 1,
	}}})
	require.NoError(t, err)
	body, err = c.handleCanonicalAppend(ctx, appendRequest)
	require.NoError(t, err)
	var appended graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(body, &appended))
	require.Len(t, appended.Results, 1)
	assert.Equal(t, graph.MutationApplied, appended.Results[0].Outcome)

	deleteRequest, err := json.Marshal(graph.DeleteEntityRequest{
		EntityID: id, ExpectedRevision: appended.Results[0].KVRevision,
	})
	require.NoError(t, err)
	body, err = c.handleCanonicalDelete(context.Background(), deleteRequest)
	require.NoError(t, err)
	var deleted graph.DeleteEntityResponse
	require.NoError(t, json.Unmarshal(body, &deleted))
	assert.Equal(t, graph.MutationApplied, deleted.Outcome)
}

func TestIntegration_CanonicalAppend_ReportsIndependentMissingSubject(t *testing.T) {
	ctx, c := startBatchTestComponent(t)
	const missing = "c360.test.mutation.system.widget.missing"
	request, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{{
		Subject: missing, Predicate: "test.event.value", Object: "not-written",
	}}})
	require.NoError(t, err)
	body, err := c.handleCanonicalAppend(ctx, request)
	require.NoError(t, err)
	var response graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(body, &response))
	require.Len(t, response.Results, 1)
	assert.Equal(t, graph.MutationEntityNotFound, response.Results[0].Outcome)
	assert.Zero(t, response.Results[0].KVRevision)
}
