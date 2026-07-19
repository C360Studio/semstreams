//go:build integration

// Package agentictools_test — integration tests for the LessonCurator (ADR-080
// gated lifecycle, task 4.1) driven against a REAL graph-ingest
// create_with_triples + update_with_triples + query.entity handler over live
// NATS/KV. These prove the validated promotion path end-to-end: evidence-
// existence resolution through the query lane, and the single-valued
// proposed→active REPLACE through the owned-fact update lane (reusing the OFW
// integration harness helpers in owned_fact_writer_integration_test.go).
package agentictools_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// birthLesson creates a proposed lesson entity citing the given evidence IDs,
// with the agent_lesson typed origin, through the real create lane.
func birthLesson(t *testing.T, gi *graphingest.Component, lessonID string, evidence ...string) {
	t.Helper()
	now := time.Now()
	triples := []message.Triple{
		{Subject: lessonID, Predicate: agvocab.LessonStatus, Object: "proposed", Confidence: 1.0, Timestamp: now},
		{Subject: lessonID, Predicate: agvocab.LessonCategory, Object: "retention-policy", Confidence: 1.0, Timestamp: now},
	}
	for _, ev := range evidence {
		triples = append(triples, message.Triple{Subject: lessonID, Predicate: agvocab.LessonEvidence, Object: ev, Confidence: 1.0, Timestamp: now})
	}
	require.NoError(t, gi.CreateEntity(context.Background(), &gtypes.EntityState{
		ID:          lessonID,
		MessageType: agentic.AgentLessonMessageType(),
		Triples:     triples,
		Version:     1,
		UpdatedAt:   now,
	}))
}

// birthEvidence creates a minimal entity so it EXISTS for evidence resolution.
func birthEvidence(t *testing.T, gi *graphingest.Component, entityID string) {
	t.Helper()
	require.NoError(t, gi.CreateEntity(context.Background(), &gtypes.EntityState{
		ID:        entityID,
		Triples:   []message.Triple{{Subject: entityID, Predicate: "agent.loop.outcome", Object: "success", Confidence: 1.0, Timestamp: time.Now()}},
		Version:   1,
		UpdatedAt: time.Now(),
	}))
}

func objectsOf(es *gtypes.EntityState, predicate string) []string {
	var out []string
	for _, tr := range es.Triples {
		if tr.Predicate == predicate {
			if s, ok := tr.Object.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// TestIntegration_LessonCurator_PromoteResolvesEvidence proves the happy path:
// a proposed lesson whose every cited evidence entity EXISTS is flipped active
// via the real update lane, and exactly one status triple survives (replace,
// not append).
func TestIntegration_LessonCurator_PromoteResolvesEvidence(t *testing.T) {
	ctx := context.Background()
	natsClient := ofwTestClient(t)
	gi := startGraphIngestForOFW(t, natsClient)

	const lessonID = "acme.ops.agent.lesson.record.aaaaaaaa-0000-5000-8000-000000000001"
	const ev1 = "acme.ops.agent.agentic-loop.execution.loop-alpha"
	const ev2 = "acme.ops.agent.agentic-loop.execution.loop-beta"
	birthEvidence(t, gi, ev1)
	birthEvidence(t, gi, ev2)
	birthLesson(t, gi, lessonID, ev1, ev2)

	c := agentictools.NewNATSLessonCurator(natsClient, slog.Default())
	require.NoError(t, c.Promote(ctx, lessonID), "promotion must succeed when all evidence exists")

	es := readEntityKV(t, natsClient, lessonID)
	status := objectsOf(es, agvocab.LessonStatus)
	require.Len(t, status, 1, "single-valued replace: exactly one status triple")
	assert.Equal(t, "active", status[0], "status flipped proposed→active")
	assert.Equal(t, 1, predicatesPresent(es, agvocab.LessonStatus), "no appended second status triple")

	// The replace targets ONLY the lifecycle predicate: immutable birth
	// predicates (evidence, category) must survive untouched (MergeTriples
	// replaces by (subject,predicate), it does not clobber siblings).
	assert.ElementsMatch(t, []string{ev1, ev2}, objectsOf(es, agvocab.LessonEvidence),
		"promotion must preserve the cited evidence")
	assert.Equal(t, []string{"retention-policy"}, objectsOf(es, agvocab.LessonCategory),
		"promotion must preserve the immutable category")
}

// TestIntegration_LessonCurator_PromoteRefusedMissingEvidence proves the
// evidence-existence gate: a lesson citing an entity that was never created is
// REFUSED and stays proposed (nothing written).
func TestIntegration_LessonCurator_PromoteRefusedMissingEvidence(t *testing.T) {
	ctx := context.Background()
	natsClient := ofwTestClient(t)
	gi := startGraphIngestForOFW(t, natsClient)

	const lessonID = "acme.ops.agent.lesson.record.aaaaaaaa-0000-5000-8000-000000000002"
	const present = "acme.ops.agent.agentic-loop.execution.loop-present"
	const missing = "acme.ops.agent.agentic-loop.execution.loop-missing"
	birthEvidence(t, gi, present)
	// `missing` deliberately never created.
	birthLesson(t, gi, lessonID, present, missing)

	c := agentictools.NewNATSLessonCurator(natsClient, slog.Default())
	err := c.Promote(ctx, lessonID)
	require.Error(t, err, "promotion must be refused when a cited evidence entity is absent")
	assert.Contains(t, err.Error(), missing, "error names the missing evidence entity")

	es := readEntityKV(t, natsClient, lessonID)
	status := objectsOf(es, agvocab.LessonStatus)
	require.Len(t, status, 1)
	assert.Equal(t, "proposed", status[0], "refused promotion leaves the lesson proposed")
}

// TestIntegration_LessonCurator_RetireAndSupersede proves the two other
// transitions ride the same single-valued replace lane and remain durable.
func TestIntegration_LessonCurator_RetireAndSupersede(t *testing.T) {
	ctx := context.Background()
	natsClient := ofwTestClient(t)
	gi := startGraphIngestForOFW(t, natsClient)
	c := agentictools.NewNATSLessonCurator(natsClient, slog.Default())

	t.Run("retire", func(t *testing.T) {
		const lessonID = "acme.ops.agent.lesson.record.bbbbbbbb-0000-5000-8000-000000000001"
		birthLesson(t, gi, lessonID, "acme.ops.agent.agentic-loop.execution.loop-x")
		require.NoError(t, c.Retire(ctx, lessonID))

		es := readEntityKV(t, natsClient, lessonID)
		status := objectsOf(es, agvocab.LessonStatus)
		require.Len(t, status, 1)
		assert.Equal(t, "retired", status[0])
		assert.Len(t, objectsOf(es, agvocab.LessonRetiredAt), 1, "retired-at stamped exactly once")
	})

	t.Run("supersede", func(t *testing.T) {
		const lessonID = "acme.ops.agent.lesson.record.bbbbbbbb-0000-5000-8000-000000000002"
		const byID = "acme.ops.agent.lesson.record.cccccccc-0000-5000-8000-000000000001"
		birthLesson(t, gi, lessonID, "acme.ops.agent.agentic-loop.execution.loop-y")
		require.NoError(t, c.Supersede(ctx, lessonID, byID))

		es := readEntityKV(t, natsClient, lessonID)
		status := objectsOf(es, agvocab.LessonStatus)
		require.Len(t, status, 1)
		assert.Equal(t, "superseded", status[0])
		assert.Equal(t, []string{byID}, objectsOf(es, agvocab.LessonSupersededBy))
	})
}

// TestIntegration_LessonCurator_RetireMustExist proves must-exist: retiring a
// never-created lesson surfaces the handler's entity_not_found.
func TestIntegration_LessonCurator_RetireMustExist(t *testing.T) {
	ctx := context.Background()
	natsClient := ofwTestClient(t)
	_ = startGraphIngestForOFW(t, natsClient)

	const missing = "acme.ops.agent.lesson.record.dddddddd-0000-5000-8000-000000000001"
	c := agentictools.NewNATSLessonCurator(natsClient, slog.Default())
	err := c.Retire(ctx, missing)
	require.Error(t, err, "retire of a never-created lesson must error (no auto-vivify)")
	assert.Contains(t, err.Error(), gtypes.ErrorCodeEntityNotFound)
}
