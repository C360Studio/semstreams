//go:build integration

package graphindex

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

const (
	ownerLoadNATSServer = "2.12.4-alpine@sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea"
	ownerLoadNATSSDK    = "v1.48.0"
	ownerLoadPredicate  = "robotics.status.ready"
)

type ownerLoadProfile struct {
	name              string
	entities          int
	nameContext       int
	spread            int
	repetitions       int
	churnPerWriter    int
	workerShapes      []int
	operationBudget   time.Duration
	p95Budget         time.Duration
	p99Budget         time.Duration
	maxServerRSSBytes int64
}

func activeOwnerLoadProfile() ownerLoadProfile {
	if os.Getenv("GRAPH_INDEX_OWNER_FILTER_FULL") == "1" {
		return ownerLoadProfile{
			name: "full", entities: 21_000, nameContext: 5_000, spread: 20,
			repetitions: 30, churnPerWriter: 200, workerShapes: []int{4, maxGraphIndexWorkers},
			operationBudget: 10 * time.Second, p95Budget: 3 * time.Second, p99Budget: 5 * time.Second,
			maxServerRSSBytes: 2 << 30,
		}
	}
	return ownerLoadProfile{
		name: "ci", entities: 5_000, nameContext: 5_000, spread: 20,
		repetitions: 5, churnPerWriter: 50, workerShapes: []int{4},
		operationBudget: 3 * time.Second, p95Budget: 3 * time.Second, p99Budget: 3 * time.Second,
		maxServerRSSBytes: 1 << 30,
	}
}

type ownerLoadFixture struct {
	name          string
	bucket        string
	store         *natsclient.KVStore
	ownerFilter   string
	forwardFilter string
	wantForward   int
	stream        jetstream.Stream
}

type ownerLoadServerStats struct {
	CPU           float64 `json:"cpu"`
	Mem           int64   `json:"mem"`
	Subscriptions uint64  `json:"subscriptions"`
	Connections   int     `json:"connections"`
	SlowConsumers int64   `json:"slow_consumers"`
}

// TestIntegration_OwnerFilterLoadHarness is the #543 owner-filter gate for the
// currently shipped layouts. The default 5k profile is a CI guard. Set
// GRAPH_INDEX_OWNER_FILTER_FULL=1 for the separately recorded 21k sustained-
// churn run at the configured and selected-maximum worker shapes.
func TestIntegration_OwnerFilterLoadHarness(t *testing.T) {
	profile := activeOwnerLoadProfile()
	t.Logf("phase=setup profile=%s entities=%d name_context=%d spread=%d reps=%d workers=%v server=%s sdk=%s",
		profile.name, profile.entities, profile.nameContext, profile.spread, profile.repetitions,
		profile.workerShapes, ownerLoadNATSServer, ownerLoadNATSSDK)

	testClient := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithFileStorage(),
		natsclient.WithNATSVersion(ownerLoadNATSServer),
		natsclient.WithTestTimeout(15*time.Second),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)

	beforeSeed := scrapeOwnerLoadServerStats(t, testClient.MonitoringURL)
	assertOwnerLoadServerBounds(t, profile, "before-seed", beforeSeed)
	fixtures := createAndSeedOwnerLoadBuckets(t, ctx, js, testClient.Client, profile)
	afterSeed := scrapeOwnerLoadServerStats(t, testClient.MonitoringURL)
	assertOwnerLoadServerBounds(t, profile, "after-seed", afterSeed)
	t.Logf("phase=seed-complete cpu=%.2f rss=%d subscriptions=%d rss_delta=%d",
		afterSeed.CPU, afterSeed.Mem, afterSeed.Subscriptions, afterSeed.Mem-beforeSeed.Mem)

	assertOwnerLoadMaxima(t, ctx, js, testClient.Client)
	assertOwnerLoadCancellationEmptyAndRecreate(t, ctx, js, testClient.Client)
	assertOwnerLoadRestartHandles(t, ctx, js, testClient.Client, fixtures)

	for _, workers := range profile.workerShapes {
		t.Run(fmt.Sprintf("workers-%d", workers), func(t *testing.T) {
			runOwnerLoadWorkerShape(t, ctx, testClient, fixtures, profile, workers)
		})
	}
}

func createAndSeedOwnerLoadBuckets(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
	nc *natsclient.Client,
	profile ownerLoadProfile,
) []ownerLoadFixture {
	t.Helper()
	const (
		predicateBucket = "OWNER_LOAD_PREDICATE"
		nameBucket      = "OWNER_LOAD_NAME"
		incomingBucket  = "OWNER_LOAD_INCOMING"
		contextBucket   = "OWNER_LOAD_CONTEXT"
	)
	bucketNames := []string{predicateBucket, nameBucket, incomingBucket, contextBucket}
	stores := make(map[string]*natsclient.KVStore, len(bucketNames))
	streams := make(map[string]jetstream.Stream, len(bucketNames))
	for _, bucketName := range bucketNames {
		raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucketName, Storage: jetstream.FileStorage})
		require.NoError(t, err)
		stores[bucketName] = nc.NewKVStore(raw)
		stream, err := js.Stream(ctx, "KV_"+bucketName)
		require.NoError(t, err)
		streams[bucketName] = stream
	}

	owner := ownerLoadEntityID(profile.nameContext / 2)
	target := "acme.ops.load.graph.target.hub"
	name := "Owner Hotspot"
	contextValue := "source.owner.load"
	fixtures := []ownerLoadFixture{
		{name: "predicate", bucket: predicateBucket, store: stores[predicateBucket],
			ownerFilter: predicateIndexEntityFilter(owner), forwardFilter: predicateIndexForwardFilter(ownerLoadPredicate),
			wantForward: profile.entities, stream: streams[predicateBucket]},
		{name: "name", bucket: nameBucket, store: stores[nameBucket],
			ownerFilter: nameIndexEntityFilter(owner), forwardFilter: nameIndexForwardFilter(name),
			wantForward: profile.nameContext, stream: streams[nameBucket]},
		{name: "incoming", bucket: incomingBucket, store: stores[incomingBucket],
			ownerFilter: incomingIndexSourceFilter(owner), forwardFilter: incomingIndexTargetFilter(target),
			wantForward: profile.entities, stream: streams[incomingBucket]},
		{name: "context", bucket: contextBucket, store: stores[contextBucket],
			ownerFilter: contextIndexEntityFilter(owner), wantForward: -1, stream: streams[contextBucket]},
	}

	type seedRow struct {
		store *natsclient.KVStore
		key   string
		value []byte
	}
	rows := make([]seedRow, 0, 2*profile.entities+2*profile.nameContext+profile.spread)
	nameValue, err := json.Marshal(nameCompositeValue{Name: name, Priority: 0})
	require.NoError(t, err)
	contextValueJSON, err := json.Marshal(contextIndexValue{Context: contextValue})
	require.NoError(t, err)
	for i := 0; i < profile.entities; i++ {
		entityID := ownerLoadEntityID(i)
		rows = append(rows,
			seedRow{stores[predicateBucket], predicateIndexKey(ownerLoadPredicate, entityID), predicateIndexMarker},
			seedRow{stores[incomingBucket], incomingIndexKey(target, entityID, "robotics.assigned.hub"), incomingIndexMarker},
		)
		if i < profile.nameContext {
			rows = append(rows,
				seedRow{stores[nameBucket], nameCompositeKey(nameIndexKey(name), entityID, "core.identity.name"), nameValue},
				seedRow{stores[contextBucket], contextIndexKey(entityID, contextHashHex(contextValue), ownerLoadPredicate), contextValueJSON},
			)
		}
	}
	for i := 0; i < profile.spread; i++ {
		predicate := fmt.Sprintf("robotics.spread.p%02d", i)
		rows = append(rows, seedRow{stores[predicateBucket],
			predicateIndexKey(predicate, ownerLoadEntityID(i)), predicateIndexMarker})
	}
	for _, row := range rows {
		require.NoError(t, natsclient.ValidateKVLiteralKey(row.key))
	}
	for _, fixture := range fixtures {
		require.NoError(t, natsclient.ValidateKVWildcardFilter(fixture.ownerFilter))
		if fixture.forwardFilter != "" {
			require.NoError(t, natsclient.ValidateKVWildcardFilter(fixture.forwardFilter))
		}
	}

	seedStart := time.Now()
	jobs := make(chan seedRow, 256)
	errs := make(chan error, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				if _, putErr := row.store.Put(ctx, row.key, row.value); putErr != nil {
					errs <- putErr
				}
			}
		}()
	}
	for _, row := range rows {
		jobs <- row
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for seedErr := range errs {
		require.NoError(t, seedErr)
	}
	elapsed := time.Since(seedStart)
	t.Logf("phase=seed rows=%d elapsed=%s throughput=%.1f_rows_per_second",
		len(rows), elapsed, float64(len(rows))/elapsed.Seconds())
	return fixtures
}

func runOwnerLoadWorkerShape(
	t *testing.T,
	ctx context.Context,
	testClient *natsclient.TestClient,
	fixtures []ownerLoadFixture,
	profile ownerLoadProfile,
	workers int,
) {
	t.Helper()
	require.LessOrEqual(t, workers, maxGraphIndexWorkers)
	phaseBefore := scrapeOwnerLoadServerStats(t, testClient.MonitoringURL)
	baselines := ownerLoadConsumerCounts(t, ctx, fixtures)

	for _, fixture := range fixtures {
		measureOwnerLoadFilter(t, ctx, fixture.store, fixture.name+"-owner", fixture.ownerFilter,
			profile, 1)
		if fixture.forwardFilter != "" {
			measureOwnerLoadFilter(t, ctx, fixture.store, fixture.name+"-forward", fixture.forwardFilter,
				profile, fixture.wantForward)
		}
	}

	type listJob struct {
		fixture ownerLoadFixture
		serial  int
	}
	type listResult struct {
		label    string
		duration time.Duration
		count    int
		err      error
	}
	resultCount := profile.repetitions * len(fixtures)
	results := make(chan listResult, resultCount)
	dispatchCtx, cancelDispatch := context.WithCancel(ctx)
	dispatcher := newKeyedDispatcher(workers, 1000,
		func(job listJob) string { return fmt.Sprintf("%s-%d", job.fixture.name, job.serial) },
		func(runCtx context.Context, job listJob) {
			started := time.Now()
			keys, err := job.fixture.store.KeysByFilter(runCtx, job.fixture.ownerFilter)
			results <- listResult{label: job.fixture.name, duration: time.Since(started), count: len(keys), err: err}
		})
	dispatcher.Start(dispatchCtx)

	var consumerHighWater atomic.Int64
	for _, count := range baselines {
		if int64(count) > consumerHighWater.Load() {
			consumerHighWater.Store(int64(count))
		}
	}
	sampleCtx, stopSampling := context.WithCancel(ctx)
	samplerErrs := make(chan error, 1)
	var samplerWG sync.WaitGroup
	samplerWG.Add(1)
	go func() {
		defer samplerWG.Done()
		for sampleCtx.Err() == nil {
			for _, fixture := range fixtures {
				info, infoErr := fixture.stream.Info(sampleCtx)
				if infoErr != nil {
					if sampleCtx.Err() == nil {
						samplerErrs <- fmt.Errorf("sample %s consumers: %w", fixture.name, infoErr)
					}
					return
				}
				for {
					current := consumerHighWater.Load()
					if int64(info.State.Consumers) <= current ||
						consumerHighWater.CompareAndSwap(current, int64(info.State.Consumers)) {
						break
					}
				}
			}
			runtime.Gosched()
		}
	}()

	churnErrs := make(chan error, workers)
	var churnWG sync.WaitGroup
	for writer := 0; writer < workers; writer++ {
		churnWG.Add(1)
		go func(writer int) {
			defer churnWG.Done()
			fixture := fixtures[writer%len(fixtures)]
			for iteration := 0; iteration < profile.churnPerWriter; iteration++ {
				key, value := ownerLoadChurnRow(fixture.name, writer, iteration, profile)
				var mutationErr error
				if iteration%2 == 0 {
					mutationErr = fixture.store.Delete(ctx, key)
				} else {
					_, mutationErr = fixture.store.Put(ctx, key, value)
				}
				if mutationErr != nil {
					churnErrs <- fmt.Errorf("%s writer %d iteration %d: %w",
						fixture.name, writer, iteration, mutationErr)
					return
				}
			}
		}(writer)
	}

	queueHighWater := 0
	catchUpStarted := time.Now()
	for repetition := 0; repetition < profile.repetitions; repetition++ {
		for _, fixture := range fixtures {
			require.NoError(t, dispatcher.Submit(ctx, listJob{fixture: fixture, serial: repetition}))
			queueHighWater = max(queueHighWater, ownerLoadQueueDepth(dispatcher))
		}
	}
	durations := make(map[string][]time.Duration, len(fixtures))
	for range resultCount {
		result := <-results
		require.NoError(t, result.err, result.label)
		require.Equal(t, 1, result.count, result.label)
		require.Less(t, result.duration, profile.operationBudget, result.label)
		durations[result.label] = append(durations[result.label], result.duration)
		queueHighWater = max(queueHighWater, ownerLoadQueueDepth(dispatcher))
	}
	catchUp := time.Since(catchUpStarted)
	churnWG.Wait()
	close(churnErrs)
	for churnErr := range churnErrs {
		require.NoError(t, churnErr)
	}
	stopSampling()
	samplerWG.Wait()
	close(samplerErrs)
	for samplerErr := range samplerErrs {
		require.NoError(t, samplerErr)
	}
	cancelDispatch()
	require.NoError(t, dispatcher.Stop(5*time.Second))

	for label, samples := range durations {
		assertOwnerLoadLatency(t, label, samples, profile)
	}
	require.LessOrEqual(t, queueHighWater, 1000, "dispatcher queue must remain bounded")
	t.Logf("phase=concurrent workers=%d operations=%d catch_up=%s throughput=%.1f_ops_per_second queue_high_water=%d consumer_high_water=%d",
		workers, resultCount, catchUp, float64(resultCount)/catchUp.Seconds(), queueHighWater,
		consumerHighWater.Load())

	for _, fixture := range fixtures {
		fixture := fixture
		require.Eventually(t, func() bool {
			info, infoErr := fixture.stream.Info(ctx)
			return infoErr == nil && info.State.Consumers == baselines[fixture.bucket]
		}, 5*time.Second, 20*time.Millisecond, "%s temporary consumers did not return to baseline", fixture.name)
	}

	// Writers only touched their own deterministic rows. Restore the seeded truth,
	// then prove every forward result is exact again.
	for writer := 0; writer < workers; writer++ {
		fixture := fixtures[writer%len(fixtures)]
		for iteration := 0; iteration < profile.churnPerWriter; iteration++ {
			key, value := ownerLoadChurnRow(fixture.name, writer, iteration, profile)
			if iteration%2 == 0 {
				_, err := fixture.store.Put(ctx, key, value)
				require.NoError(t, err)
			}
		}
	}
	for _, fixture := range fixtures {
		if fixture.forwardFilter == "" {
			continue
		}
		keys, err := fixture.store.KeysByFilter(ctx, fixture.forwardFilter)
		require.NoError(t, err)
		require.Len(t, keys, fixture.wantForward, "%s did not converge", fixture.name)
	}

	phaseAfter := scrapeOwnerLoadServerStats(t, testClient.MonitoringURL)
	assertOwnerLoadServerBounds(t, profile, fmt.Sprintf("workers-%d", workers), phaseAfter)
	require.LessOrEqual(t, phaseAfter.Subscriptions, phaseBefore.Subscriptions+2,
		"temporary list subscriptions must be released")
	require.Zero(t, phaseAfter.SlowConsumers, "load gate must not create slow consumers")
	t.Logf("phase=resource workers=%d cpu_before=%.2f cpu_after=%.2f rss_before=%d rss_after=%d subscriptions_before=%d subscriptions_after=%d",
		workers, phaseBefore.CPU, phaseAfter.CPU, phaseBefore.Mem, phaseAfter.Mem,
		phaseBefore.Subscriptions, phaseAfter.Subscriptions)
}

func measureOwnerLoadFilter(
	t *testing.T,
	ctx context.Context,
	store *natsclient.KVStore,
	label, filter string,
	profile ownerLoadProfile,
	want int,
) {
	t.Helper()
	durations := make([]time.Duration, 0, profile.repetitions)
	for repetition := 0; repetition < profile.repetitions; repetition++ {
		started := time.Now()
		keys, err := store.KeysByFilter(ctx, filter)
		duration := time.Since(started)
		require.NoError(t, err, label)
		require.Len(t, keys, want, label)
		require.Less(t, duration, profile.operationBudget, "%s rep %d", label, repetition)
		durations = append(durations, duration)
	}
	assertOwnerLoadLatency(t, label, durations, profile)
}

func assertOwnerLoadLatency(t *testing.T, label string, durations []time.Duration, profile ownerLoadProfile) {
	t.Helper()
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(len(durations)-1)*95/100]
	p99 := durations[(len(durations)-1)*99/100]
	require.LessOrEqual(t, p95, profile.p95Budget, "%s p95", label)
	require.LessOrEqual(t, p99, profile.p99Budget, "%s p99", label)
	t.Logf("phase=latency filter=%s reps=%d p50=%s p95=%s p99=%s max=%s",
		label, len(durations), durations[len(durations)/2], p95, p99, durations[len(durations)-1])
}

func ownerLoadConsumerCounts(t *testing.T, ctx context.Context, fixtures []ownerLoadFixture) map[string]int {
	t.Helper()
	counts := make(map[string]int, len(fixtures))
	for _, fixture := range fixtures {
		info, err := fixture.stream.Info(ctx)
		require.NoError(t, err)
		counts[fixture.bucket] = info.State.Consumers
	}
	return counts
}

func ownerLoadQueueDepth[T any](dispatcher *keyedDispatcher[T]) int {
	depth := 0
	for _, lane := range dispatcher.lanes {
		depth += len(lane)
	}
	return depth
}

func ownerLoadChurnRow(name string, writer, iteration int, profile ownerLoadProfile) (string, []byte) {
	index := (writer*profile.churnPerWriter + iteration) % profile.nameContext
	entityID := ownerLoadEntityID(index)
	switch name {
	case "predicate":
		return predicateIndexKey(ownerLoadPredicate, entityID), predicateIndexMarker
	case "name":
		return nameCompositeKey(nameIndexKey("Owner Hotspot"), entityID, "core.identity.name"),
			[]byte(`{"name":"Owner Hotspot","priority":0}`)
	case "incoming":
		return incomingIndexKey("acme.ops.load.graph.target.hub", entityID, "robotics.assigned.hub"), incomingIndexMarker
	default:
		return contextIndexKey(entityID, contextHashHex("source.owner.load"), ownerLoadPredicate),
			[]byte(`{"context":"source.owner.load"}`)
	}
}

func assertOwnerLoadMaxima(t *testing.T, ctx context.Context, js jetstream.JetStream, nc *natsclient.Client) {
	t.Helper()
	raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "OWNER_LOAD_MAXIMA"})
	require.NoError(t, err)
	store := nc.NewKVStore(raw)
	entityID := maximumEntityIDForContract()
	maxSegment := "a" + strings.Repeat("b", vocabulary.MaxPredicateSegmentBytes-1)
	maxPredicate := maxSegment + "." + maxSegment + "." + maxSegment
	rows := []struct {
		key    string
		filter string
	}{
		{predicateIndexKey(maxPredicate, entityID), predicateIndexEntityFilter(entityID)},
		{nameCompositeKey(nameIndexKey("Maximum"), entityID, maxPredicate), nameIndexEntityFilter(entityID)},
		{incomingIndexKey(entityID, entityID, maxPredicate), incomingIndexSourceFilter(entityID)},
		{contextIndexKey(entityID, contextHashHex("maximum"), maxPredicate), contextIndexEntityFilter(entityID)},
	}
	for _, row := range rows {
		require.NoError(t, natsclient.ValidateKVLiteralKey(row.key))
		require.NoError(t, natsclient.ValidateKVWildcardFilter(row.filter))
		_, err := store.Put(ctx, row.key, []byte{1})
		require.NoError(t, err)
		keys, err := store.KeysByFilter(ctx, row.filter)
		require.NoError(t, err)
		require.Equal(t, []string{row.key}, keys)
	}
	t.Logf("phase=maxima entity_bytes=%d predicate_bytes=%d key_bytes=[451,710,902,710]",
		len(entityID), len(maxPredicate))
}

func assertOwnerLoadCancellationEmptyAndRecreate(
	t *testing.T, ctx context.Context, js jetstream.JetStream, nc *natsclient.Client,
) {
	t.Helper()
	raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "OWNER_LOAD_LIFECYCLE"})
	require.NoError(t, err)
	store := nc.NewKVStore(raw)
	keys, err := store.KeysByFilter(ctx, ">")
	require.NoError(t, err)
	require.Empty(t, keys)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	keys, err = store.KeysByFilter(cancelled, ">")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, keys)
	require.NoError(t, js.DeleteKeyValue(ctx, "OWNER_LOAD_LIFECYCLE"))
	raw, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "OWNER_LOAD_LIFECYCLE"})
	require.NoError(t, err)
	store = nc.NewKVStore(raw)
	keys, err = store.KeysByFilter(ctx, ">")
	require.NoError(t, err)
	require.Empty(t, keys)
	_, err = store.Put(ctx, "clean.recreate", []byte("ok"))
	require.NoError(t, err)
	entry, err := store.Get(ctx, "clean.recreate")
	require.NoError(t, err)
	require.Equal(t, "ok", string(entry.Value))
	t.Log("phase=lifecycle cancellation=pass empty=pass clean_recreate=pass")
}

func assertOwnerLoadRestartHandles(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
	nc *natsclient.Client,
	fixtures []ownerLoadFixture,
) {
	t.Helper()
	for _, fixture := range fixtures {
		raw, err := js.KeyValue(ctx, fixture.bucket)
		require.NoError(t, err)
		restarted := nc.NewKVStore(raw)
		keys, err := restarted.KeysByFilter(ctx, fixture.ownerFilter)
		require.NoError(t, err)
		require.Len(t, keys, 1, fixture.name)
	}
	t.Log("phase=restart fresh_bucket_handles=pass")
}

func scrapeOwnerLoadServerStats(t *testing.T, monitoringURL string) ownerLoadServerStats {
	t.Helper()
	require.NotEmpty(t, monitoringURL, "NATS monitoring URL is mandatory evidence")
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(monitoringURL + "/varz") //nolint:gosec // mapped testcontainers endpoint
	require.NoError(t, err, "NATS /varz scrape is a hard load-gate dependency")
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var stats ownerLoadServerStats
	require.NoError(t, json.NewDecoder(response.Body).Decode(&stats))
	require.False(t, math.IsNaN(stats.CPU))
	require.False(t, math.IsInf(stats.CPU, 0))
	require.GreaterOrEqual(t, stats.CPU, 0.0)
	require.Greater(t, stats.Mem, int64(0))
	return stats
}

func assertOwnerLoadServerBounds(
	t *testing.T, profile ownerLoadProfile, phase string, stats ownerLoadServerStats,
) {
	t.Helper()
	require.LessOrEqual(t, stats.CPU, float64(runtime.NumCPU()*100+1), "%s NATS CPU is outside host capacity", phase)
	require.Less(t, stats.Mem, profile.maxServerRSSBytes, "%s NATS RSS exceeded profile ceiling", phase)
}

func ownerLoadEntityID(index int) string {
	return fmt.Sprintf("acme.ops.load.graph.entity.%06d", index)
}
