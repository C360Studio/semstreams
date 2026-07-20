//go:build integration

package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	clusteringdata "github.com/c360studio/semstreams/graph/clustering"
	graphquery "github.com/c360studio/semstreams/graph/query"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	graphclustering "github.com/c360studio/semstreams/processor/graph-clustering"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegration_ReplacementWatcherWatermarkPublicParityAndRestart(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	js, err := nc.JetStream()
	require.NoError(t, err)
	states, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	index := startReplacementIntegrationIndex(t, ctx, nc, 4)
	entityID := "acme.ops.robotics.gcs.drone.001"
	targetA := "acme.ops.robotics.gcs.mission.001"
	targetB := "acme.ops.robotics.gcs.mission.002"

	revisionA, err := states.Put(ctx, entityID, replacementStateData(t, entityID, "Alpha", "robotics.status.armed", targetA))
	require.NoError(t, err)
	waitPublicWatermark(t, ctx, nc, revisionA)
	assertReplacementPublicState(t, ctx, nc, entityID, "Alpha", "robotics.status.armed", targetA)

	revisionB, err := states.Put(ctx, entityID, replacementStateData(t, entityID, "Beta", "robotics.status.disarmed", targetB))
	require.NoError(t, err)
	waitPublicWatermark(t, ctx, nc, revisionB)
	assertReplacementPublicState(t, ctx, nc, entityID, "Beta", "robotics.status.disarmed", targetB)
	assertPredicateEntitiesPublic(t, ctx, nc, "robotics.status.armed", nil)
	assertNameEntitiesPublic(t, ctx, nc, "Alpha", nil)
	assertIncomingPublic(t, ctx, nc, targetA, nil)

	revisionEmpty, err := states.Put(ctx, entityID, replacementStateData(t, entityID, "", "", ""))
	require.NoError(t, err)
	waitPublicWatermark(t, ctx, nc, revisionEmpty)
	assertReplacementPublicEmpty(t, ctx, nc, entityID, []string{
		"robotics.status.armed", "robotics.status.disarmed",
	}, []string{"Alpha", "Beta"}, []string{targetA, targetB})
	require.NoError(t, index.Stop(5*time.Second))

	// Seed in reverse lexical order while stopped. The next component replays B then A,
	// but every public result must retain its declared total order and match live state.
	entityA := "acme.ops.robotics.gcs.drone.010"
	entityB := "acme.ops.robotics.gcs.drone.020"
	sharedTarget := "acme.ops.robotics.gcs.mission.010"
	_, err = states.Put(ctx, entityB, replacementStateData(t, entityB, "Shared", "robotics.status.ready", sharedTarget))
	require.NoError(t, err)
	lastRevision, err := states.Put(ctx, entityA, replacementStateData(t, entityA, "Shared", "robotics.status.ready", sharedTarget))
	require.NoError(t, err)

	replayed := startReplacementIntegrationIndex(t, ctx, nc, 4)
	defer replayed.Stop(5 * time.Second)
	waitPublicWatermark(t, ctx, nc, lastRevision)
	assertPredicateEntitiesPublic(t, ctx, nc, "robotics.status.ready", []string{entityA, entityB})
	assertNameEntitiesPublic(t, ctx, nc, "Shared", []string{entityA, entityB})
	assertIncomingPublic(t, ctx, nc, sharedTarget, []graph.IncomingEntry{
		{FromEntityID: entityA, Predicate: "robotics.assigned.mission"},
		{FromEntityID: entityB, Predicate: "robotics.assigned.mission"},
	})
	assertPredicateListPublic(t, ctx, nc, "robotics.status", []string{"robotics.status.ready"})
}

func TestIntegration_ReplacementPartialFailureWithholdsUntilOrderedRepair(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	js, err := nc.JetStream()
	require.NoError(t, err)
	states, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	index := startReplacementIntegrationIndex(t, ctx, nc, 4)
	defer index.Stop(5 * time.Second)

	realPredicate, err := js.KeyValue(ctx, graph.BucketPredicateIndex)
	require.NoError(t, err)
	fault := &failPutKeyValue{KeyValue: realPredicate}
	fault.failing.Store(true)
	index.predicateBucket = nc.NewKVStore(fault)

	entityID := "acme.ops.robotics.gcs.drone.030"
	targetID := "acme.ops.robotics.gcs.mission.030"
	revision, err := states.Put(ctx, entityID,
		replacementStateData(t, entityID, "Repairable", "robotics.status.failed", targetID))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return index.failedCount.Load() == 1 && index.watermark.Indexed() >= revision
	}, 5*time.Second, 20*time.Millisecond, "partial failure did not reach the fail-closed state")

	_, queryErr := nc.RequestClassified(ctx, "graph.index.query.predicate",
		[]byte(`{"predicate":"robotics.status.failed"}`), 2*time.Second)
	require.Error(t, queryErr)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, queryErr, &classified)
	require.Equal(t, graph.ErrorCodeIndexNotReady, classified.Code)

	fault.failing.Store(false)
	index.repairFailedEntities(ctx)
	require.Eventually(t, func() bool {
		return index.failedCount.Load() == 0 && index.computeIndexStatus(ctx).Ready
	}, 5*time.Second, 20*time.Millisecond, "ordered repair did not converge")
	assertReplacementPublicState(t, ctx, nc, entityID, "Repairable", "robotics.status.failed", targetID)
}

func TestIntegration_MalformedAuthoritativeDeletePoisonsWatcherWatermark(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	js, err := nc.JetStream()
	require.NoError(t, err)
	states, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	index := startReplacementIntegrationIndex(t, ctx, nc, 4)
	defer index.Stop(5 * time.Second)

	entityID := "acme.ops.robotics.gcs.drone.040"
	baselineRevision, err := states.Put(ctx, entityID, replacementStateData(t, entityID,
		"Baseline", "robotics.status.ready", "acme.ops.robotics.gcs.mission.040"))
	require.NoError(t, err)
	waitPublicWatermark(t, ctx, nc, baselineRevision)

	// A delete need not have a current value. NATS still commits the tombstone,
	// which proves the actual watcher/delete path validates its authoritative key
	// before any owner filter or bucket read can be attempted.
	require.NoError(t, states.Delete(ctx, "malformed.authoritative.key"))
	deleteRevision, err := natsclient.BucketLastSeq(ctx, states)
	require.NoError(t, err)
	require.Greater(t, deleteRevision, baselineRevision)
	require.Eventually(t, func() bool {
		status := index.computeIndexStatus(ctx)
		return index.failedCount.Load() == 1 &&
			status.State == graph.IndexStateResetRequired &&
			status.Code == graph.ErrorCodeGraphStateResetRequired &&
			status.Reason == string(graph.GraphStateReasonNoncanonicalEntityID)
	}, 5*time.Second, 20*time.Millisecond, "malformed delete did not poison the live watcher")
	require.Equal(t, baselineRevision, index.watermark.Indexed(),
		"the impossible delete revision must remain pending")
	require.Less(t, index.watermark.Indexed(), deleteRevision)
}

func TestIntegration_ReplacementAuthoritativeIdentityMismatchPoisonsAndCanonicalRestartRepairs(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	js, err := nc.JetStream()
	require.NoError(t, err)
	states, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	index := startReplacementIntegrationIndex(t, ctx, nc, 4)

	keyID := "acme.ops.robotics.gcs.drone.050"
	valueID := "acme.ops.robotics.gcs.drone.051"
	targetID := "acme.ops.robotics.gcs.mission.050"
	poisonRevision, err := states.Put(ctx, keyID,
		replacementStateData(t, valueID, "WrongOwner", "robotics.status.failed", targetID))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status := index.computeIndexStatus(ctx)
		return index.failedCount.Load() == 1 &&
			status.State == graph.IndexStateResetRequired &&
			status.Reason == string(graph.GraphStateReasonNoncanonicalEntityID)
	}, 5*time.Second, 20*time.Millisecond, "key/value identity mismatch did not poison live watcher")
	require.Less(t, index.watermark.Indexed(), poisonRevision,
		"mismatched authoritative revision must remain pending")
	_, err = index.predicateBucket.Get(ctx, predicateIndexKey("robotics.status.failed", valueID))
	require.True(t, natsclient.IsKVNotFoundError(err), "mismatched value owner was projected")

	repairRevision, err := states.Put(ctx, keyID,
		replacementStateData(t, keyID, "CanonicalOwner", "robotics.status.ready", targetID))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, getErr := index.predicateBucket.Get(ctx, predicateIndexKey("robotics.status.ready", keyID))
		return getErr == nil && index.failedCount.Load() == 0 && index.watermark.Indexed() >= repairRevision
	}, 5*time.Second, 20*time.Millisecond, "canonical replacement did not repair derived indexes")
	require.Equal(t, graph.IndexStateResetRequired, index.computeIndexStatus(ctx).State,
		"graph reset latch must remain fail-closed until restart")
	require.NoError(t, index.Stop(5*time.Second))

	restarted := startReplacementIntegrationIndex(t, ctx, nc, 4)
	defer restarted.Stop(5 * time.Second)
	waitPublicWatermark(t, ctx, nc, repairRevision)
	assertReplacementPublicState(t, ctx, nc, keyID, "CanonicalOwner", "robotics.status.ready", targetID)
	assertPredicateEntitiesPublic(t, ctx, nc, "robotics.status.failed", nil)
}

func TestIntegration_ReplacementActivationTombstoneTraversalClustering(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	js, err := nc.JetStream()
	require.NoError(t, err)
	states, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	index := startReplacementIntegrationIndex(t, ctx, nc, 4)
	defer index.Stop(5 * time.Second)

	retiredSource := "acme.ops.robotics.gcs.drone.100"
	liveSource := "acme.ops.robotics.gcs.sensor.100"
	targetID := "acme.ops.robotics.gcs.mission.100"
	leafID := "acme.ops.robotics.gcs.operation.100"
	for _, row := range []struct {
		id, name, target string
	}{
		{retiredSource, "RetiredSource", targetID},
		{liveSource, "LiveSource", targetID},
		{targetID, "Target", leafID},
		{leafID, "Leaf", ""},
	} {
		_, err = states.Put(ctx, row.id, replacementStateData(t, row.id, row.name, "robotics.status.ready", row.target))
		require.NoError(t, err)
	}
	seedRevision, err := natsclient.BucketLastSeq(ctx, states)
	require.NoError(t, err)
	waitPublicWatermark(t, ctx, nc, seedRevision)
	assertIncomingPublic(t, ctx, nc, targetID, []graph.IncomingEntry{
		{FromEntityID: retiredSource, Predicate: "robotics.assigned.mission"},
		{FromEntityID: liveSource, Predicate: "robotics.assigned.mission"},
	})

	queryClient, err := graphquery.NewClient(ctx, nc, nil)
	require.NoError(t, err)
	defer queryClient.Close()
	path, err := queryClient.ExecutePathQuery(ctx, graphquery.PathQuery{
		StartEntity: liveSource, MaxDepth: 3, MaxNodes: 10, MaxTime: 5 * time.Second,
		DecayFactor: 0.8, MaxPaths: 10,
	})
	require.NoError(t, err)
	visited := make([]string, 0, len(path.Entities))
	for _, entity := range path.Entities {
		visited = append(visited, entity.ID)
	}
	sort.Strings(visited)
	require.Equal(t, []string{targetID, leafID, liveSource}, visited,
		"authoritative ENTITY_STATES traversal must follow canonical live relationships")

	clusteringConfig := graphclustering.DefaultConfig()
	clusteringConfig.DetectionIntervalStr = "100ms"
	clusteringConfig.MinCommunitySize = 1
	clusteringConfig.MaxIterations = 25
	clusteringRaw, err := json.Marshal(clusteringConfig)
	require.NoError(t, err)
	clusteringComponent, err := graphclustering.CreateGraphClustering(clusteringRaw,
		component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	require.NoError(t, clusteringComponent.(component.LifecycleComponent).Initialize())
	require.NoError(t, clusteringComponent.(component.LifecycleComponent).Start(ctx))
	defer clusteringComponent.(component.LifecycleComponent).Stop(5 * time.Second)
	communityKV, err := js.KeyValue(ctx, graph.BucketCommunityIndex)
	require.NoError(t, err)
	// The window must clear a full readiness HEARTBEAT, not just a detection tick.
	// Under ADR-083 clustering gates on watched GRAPH_STATUS state, so it observes a
	// readiness transition at producer-heartbeat granularity rather than instantly as
	// the former per-tick request/reply did: graph-index publishes {target:0} on its
	// first tick (pre-enumeration, a hard-stop `empty` defer) and the clustering gate
	// cannot clear until the NEXT heartbeat carries state=ready. Measured: the gate
	// clears exactly readiness.DefaultHeartbeat after index Start, so a 5s window
	// expired on the boundary. Three heartbeats plus the 100ms detection tick and the
	// persist leaves real margin without hiding a genuine stall.
	require.Eventually(t, func() bool {
		keys, keysErr := communityKV.Keys(ctx)
		return keysErr == nil && len(keys) > 0
	}, 3*readiness.DefaultHeartbeat+2*time.Second, 25*time.Millisecond,
		"production graph-clustering cycle did not persist communities")

	require.NoError(t, states.Delete(ctx, retiredSource))
	sourceDeleteRevision, err := natsclient.BucketLastSeq(ctx, states)
	require.NoError(t, err)
	waitPublicWatermark(t, ctx, nc, sourceDeleteRevision)
	assertIncomingPublic(t, ctx, nc, targetID, []graph.IncomingEntry{{
		FromEntityID: liveSource, Predicate: "robotics.assigned.mission",
	}})
	incomingSources, err := queryClient.GetIncomingRelationships(ctx, targetID)
	require.NoError(t, err)
	require.Equal(t, []string{liveSource}, incomingSources,
		"production graph/query reader must exclude the tombstoned source")

	require.NoError(t, states.Delete(ctx, targetID))
	targetDeleteRevision, err := natsclient.BucketLastSeq(ctx, states)
	require.NoError(t, err)
	waitPublicWatermark(t, ctx, nc, targetDeleteRevision)
	assertIncomingPublic(t, ctx, nc, targetID, []graph.IncomingEntry{{
		FromEntityID: liveSource, Predicate: "robotics.assigned.mission",
	}})
	incomingSources, err = queryClient.GetIncomingRelationships(ctx, targetID)
	require.NoError(t, err)
	require.Equal(t, []string{liveSource}, incomingSources,
		"production graph/query reader must preserve live-source ownership after target retirement")
	outgoing := requestPublic[graph.OutgoingQueryResponse](t, ctx, nc, "graph.index.query.outgoing",
		map[string]any{"entity_id": targetID})
	require.Empty(t, outgoing.Data.Relationships, "retired target must retract its source-owned outgoing edge")

	// Capture the community-bucket revision only after both authoritative
	// tombstones have crossed graph-index's public watermark. A later revision
	// whose persisted member set contains both live entities and neither retired
	// entity proves a new production component/kvProvider/LPA cycle completed on
	// the replacement topology (rather than merely observing the seed cycle).
	postRetirementCommunityRevision, err := natsclient.BucketLastSeq(ctx, communityKV)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		revision, revisionErr := natsclient.BucketLastSeq(ctx, communityKV)
		if revisionErr != nil || revision <= postRetirementCommunityRevision {
			return false
		}
		keys, keysErr := communityKV.Keys(ctx)
		if keysErr != nil {
			return false
		}
		members := make(map[string]bool)
		for _, key := range keys {
			if len(key) >= len("entity.") && key[:len("entity.")] == "entity." {
				continue
			}
			entry, getErr := communityKV.Get(ctx, key)
			if getErr != nil {
				return false
			}
			var community clusteringdata.Community
			if json.Unmarshal(entry.Value(), &community) != nil {
				return false
			}
			for _, member := range community.Members {
				members[member] = true
			}
		}
		return members[liveSource] && members[leafID] &&
			!members[retiredSource] && !members[targetID]
	}, 5*time.Second, 25*time.Millisecond,
		"a post-retirement production clustering cycle must persist only live ENTITY_STATES members")
}

type failPutKeyValue struct {
	jetstream.KeyValue
	failing atomic.Bool
}

func (f *failPutKeyValue) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	if f.failing.Load() {
		return 0, errors.New("injected predicate membership failure")
	}
	return f.KeyValue.Put(ctx, key, value)
}

func startReplacementIntegrationIndex(t *testing.T, ctx context.Context, nc *natsclient.Client, workers int) *Component {
	t.Helper()
	config := DefaultConfig()
	config.Workers = workers
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	created, err := CreateGraphIndex(raw, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	index := created.(*Component)
	require.NoError(t, index.Initialize())
	// Readiness is now observed through the GRAPH_STATUS heartbeat rather than an
	// on-demand status request (ADR-083), so shorten the heartbeat — pre-Start, the
	// only safe window — to keep waitPublicWatermark's granularity in the same range
	// it had when every check computed the envelope fresh.
	index.statusInterval = 100 * time.Millisecond
	require.NoError(t, index.Start(ctx))
	return index
}

func replacementStateData(t *testing.T, entityID, name, statusPredicate, targetID string) []byte {
	t.Helper()
	triples := make([]message.Triple, 0, 3)
	if name != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: vocabulary.DCTermsTitle, Object: name})
	}
	if statusPredicate != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: statusPredicate, Object: true})
	}
	if targetID != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: "robotics.assigned.mission", Object: targetID,
		})
	}
	data, err := graph.MarshalEntityState(&graph.EntityState{ID: entityID, Triples: triples})
	require.NoError(t, err)
	return data
}

// waitPublicWatermark blocks until the PUBLICLY observable readiness envelope shows the
// index caught up to at least revision. "Publicly observable" moved from the removed
// `graph.index.query.status` request/reply to the GRAPH_STATUS KV key (ADR-083); the
// point of the gate is unchanged — these tests assert against what an outside consumer
// can see, never against the component's in-process state.
func waitPublicWatermark(t *testing.T, ctx context.Context, nc *natsclient.Client, revision uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		bucket, err := nc.GetKeyValueBucket(ctx, readiness.BucketGraphStatus)
		if err != nil {
			return false
		}
		entry, err := bucket.Get(ctx, readiness.KeyGraphIndex)
		if err != nil {
			return false
		}
		var status graph.IndexStatusResponse
		if json.Unmarshal(entry.Value(), &status) != nil {
			return false
		}
		return status.Ready && status.IndexedRevision >= revision && status.TargetRevision >= revision
	}, 15*time.Second, 20*time.Millisecond, "public status did not reach revision %d", revision)
}

func assertReplacementPublicState(
	t *testing.T,
	ctx context.Context,
	nc *natsclient.Client,
	entityID, name, statusPredicate, targetID string,
) {
	t.Helper()
	assertNameEntitiesPublic(t, ctx, nc, name, []string{entityID})
	assertPredicateEntitiesPublic(t, ctx, nc, statusPredicate, []string{entityID})
	assertIncomingPublic(t, ctx, nc, targetID, []graph.IncomingEntry{{
		FromEntityID: entityID, Predicate: "robotics.assigned.mission",
	}})

	outgoing := requestPublic[graph.OutgoingQueryResponse](t, ctx, nc, "graph.index.query.outgoing",
		map[string]any{"entity_id": entityID})
	require.Equal(t, []graph.OutgoingEntry{{ToEntityID: targetID, Predicate: "robotics.assigned.mission"}},
		outgoing.Data.Relationships)

	value := "true"
	predicate := requestPublic[graph.PredicateQueryResponse](t, ctx, nc, "graph.index.query.predicate",
		map[string]any{"predicate": statusPredicate, "value": value})
	require.Equal(t, []string{entityID}, predicate.Data.Entities)

	stats := requestPublic[graph.PredicateStatsQueryResponse](t, ctx, nc, "graph.index.query.predicateStats",
		map[string]any{"predicate": statusPredicate, "sample_limit": 10})
	require.Equal(t, 1, stats.Data.EntityCount)
	require.Equal(t, []string{entityID}, stats.Data.SampleEntities)

	compound := requestPublic[graph.CompoundPredicateQueryResponse](t, ctx, nc, "graph.index.query.predicateCompound",
		graph.CompoundPredicateQuery{Predicates: []string{vocabulary.DCTermsTitle, statusPredicate}, Operator: "AND"})
	require.Equal(t, []string{entityID}, compound.Data.Entities)
	assertPredicateListPublic(t, ctx, nc, "robotics.status", []string{statusPredicate})
}

func assertReplacementPublicEmpty(
	t *testing.T,
	ctx context.Context,
	nc *natsclient.Client,
	entityID string,
	predicates, names, targets []string,
) {
	t.Helper()
	for _, predicate := range predicates {
		assertPredicateEntitiesPublic(t, ctx, nc, predicate, nil)
	}
	for _, name := range names {
		assertNameEntitiesPublic(t, ctx, nc, name, nil)
	}
	for _, target := range targets {
		assertIncomingPublic(t, ctx, nc, target, nil)
	}
	outgoing := requestPublic[graph.OutgoingQueryResponse](t, ctx, nc, "graph.index.query.outgoing",
		map[string]any{"entity_id": entityID})
	require.Empty(t, outgoing.Data.Relationships)
	assertPredicateListPublic(t, ctx, nc, "robotics.status", nil)
}

func assertPredicateEntitiesPublic(
	t *testing.T, ctx context.Context, nc *natsclient.Client, predicate string, want []string,
) {
	t.Helper()
	response := requestPublic[graph.PredicateQueryResponse](t, ctx, nc, "graph.index.query.predicate",
		map[string]any{"predicate": predicate})
	require.Equal(t, emptyStrings(want), response.Data.Entities)
}

func assertNameEntitiesPublic(t *testing.T, ctx context.Context, nc *natsclient.Client, name string, want []string) {
	t.Helper()
	response := requestPublic[graph.QueryResponse[graph.NameData]](t, ctx, nc, "graph.index.query.byName",
		map[string]any{"name": name})
	got := make([]string, 0, len(response.Data.Matches))
	for _, match := range response.Data.Matches {
		got = append(got, match.EntityID)
	}
	require.Equal(t, emptyStrings(want), got)
}

func assertIncomingPublic(
	t *testing.T, ctx context.Context, nc *natsclient.Client, targetID string, want []graph.IncomingEntry,
) {
	t.Helper()
	response := requestPublic[graph.IncomingQueryResponse](t, ctx, nc, "graph.index.query.incoming",
		map[string]any{"entity_id": targetID})
	if want == nil {
		want = []graph.IncomingEntry{}
	}
	require.Equal(t, want, response.Data.Relationships)
}

func assertPredicateListPublic(
	t *testing.T, ctx context.Context, nc *natsclient.Client, namespace string, want []string,
) {
	t.Helper()
	response := requestPublic[graph.PredicateListQueryResponse](t, ctx, nc, "graph.index.query.predicateList",
		graph.PredicateListQuery{Namespace: namespace})
	got := make([]string, 0, len(response.Data.Predicates))
	for _, summary := range response.Data.Predicates {
		got = append(got, summary.Predicate)
	}
	sort.Strings(got)
	require.Equal(t, emptyStrings(want), got)
}

func requestPublic[T any](
	t *testing.T, ctx context.Context, nc *natsclient.Client, subject string, request any,
) T {
	t.Helper()
	data, err := json.Marshal(request)
	require.NoError(t, err)
	responseData, err := nc.RequestClassified(ctx, subject, data, 2*time.Second)
	require.NoError(t, err)
	var response T
	require.NoError(t, json.Unmarshal(responseData, &response))
	return response
}

func emptyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
