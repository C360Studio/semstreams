//go:build integration

package agentictools_test

import (
	"context"
	"log/slog"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/projection"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

var lessonBirthPredicates = []string{
	agvocab.LessonCategory,
	agvocab.LessonPolarity,
	agvocab.LessonSeverity,
	agvocab.LessonCreatedAt,
	agvocab.LessonSummary,
	agvocab.LessonDetail,
	agvocab.LessonInjectionForm,
	agvocab.LessonEvidence,
	agvocab.LessonAppliesTo,
	agvocab.LessonObservedRole,
	agvocab.ActionExecutedBy,
}

func TestIntegration_LessonCuratorFromPublicProjectionContractPreservesBirthPredicates(t *testing.T) {
	ctx := context.Background()
	client := graphMutationTestClient(t)
	ingest := startGraphIngestForMutationTest(t, client)
	builtins.Register()

	const evidenceID = "acme.ops.agent.agentic-loop.execution.loop-abc"
	now := time.Now().UTC()
	require.NoError(t, ingest.CreateEntity(ctx, &graph.EntityState{
		ID:          evidenceID,
		MessageType: agentic.LoopExecutionMessageType(),
		Triples: []message.Triple{{
			Subject: evidenceID, Predicate: agvocab.LoopRole, Object: "ops",
			Source: "lesson-curator-integration", Timestamp: now, Confidence: 1,
		}},
		Version:   1,
		UpdatedAt: now,
	}))

	lessonContract := agentictools.LessonProjectionContract()
	require.Len(t, lessonContract.Groups, 1)
	mutations, err := projection.NewMutationClient(projection.MutationClientConfig{
		NATS:      client,
		Contracts: []projection.Contract{lessonContract},
	})
	require.NoError(t, err)
	curator := agentictools.NewLessonCurator(mutations, mutations, slog.Default())

	executor := agentictools.NewEmitLessonExecutor(
		agentictools.NewNATSLessonStore(client),
		types.PlatformMeta{Org: "acme", Platform: "ops"},
		slog.Default(),
	)

	call := validLessonToolCall("birth-curator")
	call.LoopID = "loop-curator"
	call.Arguments["category"] = "curator-lifecycle"
	call.Arguments["summary"] = "curator transitions preserve lesson birth identity"
	born, execErr := executor.Execute(ctx, *call)
	require.NoError(t, execErr)
	require.Empty(t, born.Error)
	require.Equal(t, true, born.Metadata["lesson_created"])
	entityID, ok := born.Metadata["lesson_id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, entityID)
	birthBefore := birthObjectSets(t, readEntityKV(t, client, entityID))

	const supersedingID = "acme.ops.agent.lesson.record.99999999-9999-5999-8999-999999999999"
	staleRetiredAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	seedLifecycleGroup(t, ctx, mutations, lessonContract.Name, lessonContract.Groups[0].Name, entityID, []message.Triple{
		lifecycleSeedTriple(entityID, agvocab.LessonStatus, "proposed", staleRetiredAt),
		lifecycleSeedTriple(entityID, agvocab.LessonSupersededBy, supersedingID, staleRetiredAt),
		lifecycleSeedTriple(entityID, agvocab.LessonRetiredAt, staleRetiredAt.Format(time.RFC3339), staleRetiredAt),
	})
	seeded := readEntityKV(t, client, entityID)
	require.Equal(t, []string{"proposed"}, sortedObjects(seeded, agvocab.LessonStatus))
	require.Equal(t, []string{supersedingID}, sortedObjects(seeded, agvocab.LessonSupersededBy))
	require.Equal(t, []string{staleRetiredAt.Format(time.RFC3339)}, sortedObjects(seeded, agvocab.LessonRetiredAt))

	require.NoError(t, curator.Promote(ctx, entityID))
	promoted := readEntityKV(t, client, entityID)
	assertBirthObjectSetsUnchanged(t, birthBefore, promoted)
	assertLifecycleGroup(t, promoted, "active", "", false)

	require.NoError(t, curator.Supersede(ctx, entityID, supersedingID))
	superseded := readEntityKV(t, client, entityID)
	assertBirthObjectSetsUnchanged(t, birthBefore, superseded)
	assertLifecycleGroup(t, superseded, "superseded", supersedingID, false)

	require.NoError(t, curator.Retire(ctx, entityID))
	retired := readEntityKV(t, client, entityID)
	assertBirthObjectSetsUnchanged(t, birthBefore, retired)
	assertLifecycleGroup(t, retired, "retired", "", true)

	require.NoError(t, curator.Supersede(ctx, entityID, supersedingID))
	supersededAgain := readEntityKV(t, client, entityID)
	assertBirthObjectSetsUnchanged(t, birthBefore, supersededAgain)
	assertLifecycleGroup(t, supersededAgain, "superseded", supersedingID, false)

	call.ID = "reemit-curator"
	reemitted, reemitErr := executor.Execute(ctx, *call)
	require.NoError(t, reemitErr)
	require.Empty(t, reemitted.Error,
		"an identical strict create must verify the retained identity instead of reporting a collision")
	require.Equal(t, false, reemitted.Metadata["lesson_created"])
	require.Equal(t, "superseded", reemitted.Metadata["lesson_status"])
}

func seedLifecycleGroup(
	t *testing.T,
	ctx context.Context,
	writer projection.PredicateReconciler,
	contractName string,
	groupName string,
	entityID string,
	desired []message.Triple,
) {
	t.Helper()
	_, err := writer.Reconcile(ctx, projection.ReconcileMutation{
		Contract: contractName,
		Group:    groupName,
		EntityID: entityID,
		Desired:  desired,
		Metadata: projection.MutationMetadata{
			RequestID: "seed-dirty-lesson-lifecycle",
			Source:    "lesson-curator-integration",
			Timestamp: desired[0].Timestamp,
		},
	})
	require.NoError(t, err)
}

func lifecycleSeedTriple(entityID, predicate, object string, timestamp time.Time) message.Triple {
	return message.Triple{
		Subject:    entityID,
		Predicate:  predicate,
		Object:     object,
		Source:     "lesson-curator-integration",
		Timestamp:  timestamp,
		Confidence: 1,
	}
}

func assertBirthObjectSetsUnchanged(
	t *testing.T,
	want map[string][]string,
	entity *graph.EntityState,
) {
	t.Helper()
	require.Equal(t, want, birthObjectSets(t, entity),
		"every official birth predicate must retain its exact object set")
}

func assertLifecycleGroup(
	t *testing.T,
	entity *graph.EntityState,
	status string,
	supersededBy string,
	wantRetiredAt bool,
) {
	t.Helper()
	require.Equal(t, []string{status}, sortedObjects(entity, agvocab.LessonStatus))
	require.Equal(t, optionalObject(supersededBy), sortedObjects(entity, agvocab.LessonSupersededBy))
	retiredAt := sortedObjects(entity, agvocab.LessonRetiredAt)
	if !wantRetiredAt {
		require.Empty(t, retiredAt)
		return
	}
	require.Len(t, retiredAt, 1)
	_, err := time.Parse(time.RFC3339, retiredAt[0])
	require.NoError(t, err)
}

func birthObjectSets(t *testing.T, entity *graph.EntityState) map[string][]string {
	t.Helper()
	sets := make(map[string][]string, len(lessonBirthPredicates))
	for _, predicate := range lessonBirthPredicates {
		objects := sortedObjects(entity, predicate)
		require.NotEmpty(t, objects, "birth predicate %s must be present", predicate)
		sets[predicate] = objects
	}
	return sets
}

func sortedObjects(entity *graph.EntityState, predicate string) []string {
	var objects []string
	for _, triple := range entity.Triples {
		if triple.Predicate != predicate {
			continue
		}
		if object, ok := triple.Object.(string); ok {
			objects = append(objects, object)
		}
	}
	sort.Strings(objects)
	return objects
}

func optionalObject(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
