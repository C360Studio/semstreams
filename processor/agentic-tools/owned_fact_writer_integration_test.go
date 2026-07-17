//go:build integration

// Package agentictools_test — integration tests for OwnedFactWriter (gh#425)
// driven against a REAL graph-ingest update_with_triples + query.entity handler.
// These exercise the atomic replace-by-predicate contract, the shrinking-package
// clear (the exact emit_change retry bug that motivated the ask), owned-prefix
// read-back scoping, and must-exist through live NATS/KV.
package agentictools_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
)

// startGraphIngestForOFW stands up a real graph-ingest Component bound to the
// test NATS client (already Started). It owns the update_with_triples mutation
// and query.entity read-back handlers the OwnedFactWriter drives.
func startGraphIngestForOFW(t *testing.T, natsClient *natsclient.Client) *graphingest.Component {
	t.Helper()
	cfg := graphingest.DefaultConfig()
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)

	gi := comp.(*graphingest.Component)
	require.NoError(t, gi.Initialize())
	require.NoError(t, gi.Start(context.Background()))
	t.Cleanup(func() { _ = gi.Stop(5 * time.Second) })

	// Let the mutation/query subscriptions propagate.
	time.Sleep(150 * time.Millisecond)
	return gi
}

// readEntityKV reads the stored EntityState straight from the ENTITY_STATES KV
// bucket — a durable post-write probe independent of any read-cache.
func readEntityKV(t *testing.T, natsClient *natsclient.Client, entityID string) *gtypes.EntityState {
	t.Helper()
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(context.Background(), gtypes.BucketEntityStates)
	require.NoError(t, err)
	entry, err := kv.Get(context.Background(), entityID)
	require.NoError(t, err)
	var es gtypes.EntityState
	require.NoError(t, json.Unmarshal(entry.Value(), &es))
	return &es
}

func predicatesPresent(es *gtypes.EntityState, predicate string) int {
	n := 0
	for _, tr := range es.Triples {
		if tr.Predicate == predicate {
			n++
		}
	}
	return n
}

func ofwTestClient(t *testing.T) *natsclient.Client {
	t.Helper()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithStreams(natsclient.TestStreamConfig{Name: "ENTITY", Subjects: []string{"entity.>"}}),
	)
	return tc.Client
}

// --- The motivating bug: a re-plan that SHRINKS the package must not leave
// stale same-prefix task facts behind, and must not touch sibling owners. ---

func TestIntegration_OwnedFactWriter_ReplaceShrinksPackage(t *testing.T) {
	ctx := context.Background()
	natsClient := ofwTestClient(t)
	gi := startGraphIngestForOFW(t, natsClient)

	const entityID = "acme.spec.openspec.run.change.001"
	const prefix = "change.demo.task-"

	// Seed a 3-task package plus a sibling predicate owned by lifecycle. Stamp
	// a birth envelope so the replace is proven to PRESERVE it (never clobber).
	birthType := message.Type{Domain: "spec", Category: "change", Version: "v1"}
	require.NoError(t, gi.CreateEntity(ctx, &gtypes.EntityState{
		ID:          entityID,
		MessageType: birthType,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "demo", "task-1"), Object: "alpha", Confidence: 1.0, Timestamp: time.Now()},
			{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "demo", "task-2"), Object: "beta", Confidence: 1.0, Timestamp: time.Now()},
			{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "demo", "task-3"), Object: "gamma", Confidence: 1.0, Timestamp: time.Now()},
			{Subject: entityID, Predicate: semantictest.Predicate(t, "lifecycle", "status", "phase"), Object: "planning", Confidence: 1.0, Timestamp: time.Now()}, // sibling, must survive
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}))

	w := agentictools.NewNATSOwnedFactWriter(natsClient)

	// A retry shrinks the package to a single, changed task.
	owned, err := w.ReadOwnedPredicates(ctx, entityID, prefix)
	require.NoError(t, err)
	assert.Equal(t, []string{"change.demo.task-1", "change.demo.task-2", "change.demo.task-3"}, owned,
		"read-back must return exactly the owned-prefix predicates, sorted")

	revised := []message.Triple{{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "demo", "task-1"), Object: "alpha-revised", Confidence: 1.0, Timestamp: time.Now()}}
	require.NoError(t, w.ReplaceTriples(ctx, entityID, revised, owned),
		"clear the whole owned prefix, then write the revised package")

	es := readEntityKV(t, natsClient, entityID)
	assert.Equal(t, 1, predicatesPresent(es, "change.demo.task-1"), "task-1 present exactly once")
	assert.Equal(t, 0, predicatesPresent(es, "change.demo.task-2"), "stale task-2 must be cleared")
	assert.Equal(t, 0, predicatesPresent(es, "change.demo.task-3"), "stale task-3 must be cleared")
	assert.Equal(t, 1, predicatesPresent(es, "lifecycle.status.phase"), "sibling owner's predicate must survive")
	assert.Equal(t, birthType, es.MessageType, "replace must PRESERVE the birth envelope, never re-stamp it")

	// The surviving value is the revised one (replace-by-predicate, not append).
	for _, tr := range es.Triples {
		if tr.Predicate == "change.demo.task-1" {
			assert.Equal(t, "alpha-revised", tr.Object, "task-1 must carry the revised value, not the stale one")
		}
	}
}

// --- ReadOwnedPredicates scopes strictly to the owned prefix. ---

func TestIntegration_OwnedFactWriter_ReadOwnedPredicatesPrefixScoped(t *testing.T) {
	ctx := context.Background()
	natsClient := ofwTestClient(t)
	gi := startGraphIngestForOFW(t, natsClient)

	const entityID = "acme.spec.openspec.run.change.002"
	require.NoError(t, gi.CreateEntity(ctx, &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "demo", "design"), Object: "d", Confidence: 1.0, Timestamp: time.Now()},
			{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "demo", "task-1"), Object: "a", Confidence: 1.0, Timestamp: time.Now()},
			{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "other", "task-1"), Object: "x", Confidence: 1.0, Timestamp: time.Now()}, // different slug
			{Subject: entityID, Predicate: semantictest.Predicate(t, "core", "identity", "type"), Object: "run", Confidence: 1.0, Timestamp: time.Now()},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}))

	w := agentictools.NewNATSOwnedFactWriter(natsClient)
	owned, err := w.ReadOwnedPredicates(ctx, entityID, "change.demo.")
	require.NoError(t, err)
	assert.Equal(t, []string{"change.demo.design", "change.demo.task-1"}, owned,
		"only the change.demo.* predicates, never change.other.* or core.*")
}

// --- Must-exist: replacing a never-created entity errors (no auto-vivify). ---

func TestIntegration_OwnedFactWriter_ReplaceMustExist(t *testing.T) {
	ctx := context.Background()
	natsClient := ofwTestClient(t)
	gi := startGraphIngestForOFW(t, natsClient)
	_ = gi // handler live; entity deliberately absent.

	const missing = "acme.spec.openspec.run.change.404"
	w := agentictools.NewNATSOwnedFactWriter(natsClient)
	err := w.ReplaceTriples(ctx, missing,
		[]message.Triple{{Subject: missing, Predicate: semantictest.Predicate(t, "change", "demo", "task-1"), Object: "a", Confidence: 1.0, Timestamp: time.Now()}}, nil)
	require.Error(t, err, "replace on a non-existent entity must error (no auto-vivify)")
	assert.Contains(t, err.Error(), gtypes.ErrorCodeEntityNotFound,
		"the handler's entity_not_found code must surface to the caller")
}

// --- ReadOwnedPredicates on a never-created entity surfaces entity_not_found. ---

func TestIntegration_OwnedFactWriter_ReadMissingEntity(t *testing.T) {
	ctx := context.Background()
	natsClient := ofwTestClient(t)
	_ = startGraphIngestForOFW(t, natsClient)

	const missing = "acme.spec.openspec.run.change.405"
	w := agentictools.NewNATSOwnedFactWriter(natsClient)
	_, err := w.ReadOwnedPredicates(ctx, missing, "change.demo.")
	require.Error(t, err, "read-back on a non-existent entity must error")
	assert.Contains(t, err.Error(), gtypes.ErrorCodeEntityNotFound,
		"the handler's entity_not_found code must surface to the caller")
}
