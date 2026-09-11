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
	// Server and SDK pins are shared with the predicate-layout smoke gate —
	// see nats_pin_test.go for why one source, and for the unit-tier
	// assertion that keeps the reported SDK honest against go.mod.
	ownerLoadNATSServer = graphIndexNATSServerPin
	ownerLoadNATSSDK    = graphIndexNATSGoPin
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
	p95Budget         time.Duration
	p99Budget         time.Duration
	maxServerRSSBytes int64
}

func activeOwnerLoadProfile() ownerLoadProfile {
	if os.Getenv("GRAPH_INDEX_OWNER_FILTER_FULL") == "1" {
		return ownerLoadFullProfile()
	}
	return ownerLoadCIProfile()
}

func ownerLoadCIProfile() ownerLoadProfile {
	return ownerLoadProfile{
		name: "ci", entities: 5_000, nameContext: 5_000, spread: 20,
		repetitions: 5, churnPerWriter: 50, workerShapes: []int{4},
		// This profile is a REGRESSION GUARD, not activation evidence (#1284, owner ruling
		// 2026-09-11). ADR-077 section 8 condition 4 —
		// docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:139 — is satisfied by the
		// supervised record at docs/operations/32-predicate-layout-smoke-harness.md:80+, taken on the
		// current server and SDK pin, never by a shared-runner CI run. The graph-index spec states the
		// same split under the requirement "Fixed-position owner filtering is proven before production
		// reconciliation activates".
		//
		// There is exactly ONE absolute ceiling on a directly measured key listing, and this harness
		// does not predict it: natsclient.DefaultKVOptions().Timeout (natsclient/kv.go:39, applied by
		// applyTimeout at :538) bounds every KeysByFilter call here, and an expiry surfaces as the
		// operation's own typed error on the require.NoError that precedes each measurement — with the
		// partial key set refused at natsclient/kv.go:589 and :592. A per-repetition wall-clock budget
		// is NOT restated at any value: below the deadline it fires on stalls the framework itself
		// tolerates (the five gh#750/#1284 events), above it it can never fire (#1286).
		//
		// p95Budget/p99Budget stay at 3s by the same owner ruling. They are an order-of-magnitude
		// latency check and they are deliberately loose. Measured healthy p95, stated in
		// MILLISECONDS — the unit discipline #1286 exists for:
		//
		//	quiet box, worst filter (name-forward)          p95  77.861 ms
		//	shared runner, worst filter (predicate-forward)  p95 175.4   ms  (run 33208133273)
		//	shared runner, worst healthy single sample           389.0   ms  (run 33208133273)
		//	supervised 21k record (rev 60c79736)             p95 311.449 ms, p99 320.157 ms
		//
		// 3s is therefore ~38x the quiet-box p95 and ~17x the shared-runner p95: a weak regression
		// guard that would not notice a 10x regression, against a realistic 5-20x regression class.
		// Tightening it needs within-filter stall-adjacency data that does not exist yet, which is
		// exactly what the submission-order recording below starts collecting. gh#1287 owns the
		// re-derivation and must not be pre-empted by a bare constant edit.
		//
		// gh#750 / PR #755 raised the now-deleted per-operation budget against a distribution that no
		// longer exists: that run measured p50 99.784608 ms, p95 697.726516 ms, max 2.23697341 s,
		// while forward filters now measure p95 78-175 ms and max 166-389 ms. What an ADR/spec change
		// protects from here is the EVIDENCE HOME, not this constant.
		p95Budget: 3 * time.Second, p99Budget: 3 * time.Second,
		maxServerRSSBytes: 1 << 30,
	}
}

func ownerLoadFullProfile() ownerLoadProfile {
	return ownerLoadProfile{
		name: "full", entities: 21_000, nameContext: 5_000, spread: 20,
		repetitions: 30, churnPerWriter: 200, workerShapes: []int{4, maxGraphIndexWorkers},
		// No operationBudget: the same natsclient KV deadline bounds this profile too, so a predicted
		// 10s ceiling here could never fire (#1284 design P18). The supervised record is this
		// profile's output. Whether THESE percentiles are also re-derived is #1284 design Q7(b), still
		// open on the owner docket; gh#1287 covers the CI profile's, not this one's.
		p95Budget: 3 * time.Second, p99Budget: 5 * time.Second,
		maxServerRSSBytes: 2 << 30,
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
		natsclient.WithMonitoring(),
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
	)
	bucketNames := []string{predicateBucket, nameBucket, incomingBucket}
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
	}

	type seedRow struct {
		store *natsclient.KVStore
		key   string
		value []byte
	}
	rows := make([]seedRow, 0, 2*profile.entities+profile.nameContext+profile.spread)
	nameValue, err := json.Marshal(nameCompositeValue{Name: name, Priority: 0})
	require.NoError(t, err)
	for i := 0; i < profile.entities; i++ {
		entityID := ownerLoadEntityID(i)
		rows = append(rows,
			seedRow{stores[predicateBucket], predicateIndexKey(ownerLoadPredicate, entityID), predicateIndexMarker},
			seedRow{stores[incomingBucket], incomingIndexKey(target, entityID, "robotics.assigned.hub"), incomingIndexMarker},
		)
		if i < profile.nameContext {
			rows = append(rows,
				seedRow{stores[nameBucket], nameCompositeKey(nameIndexKey(name), entityID, "core.identity.name"), nameValue})
		}
	}
	spreadValues := [...]string{
		"robotics.spread.p00", "robotics.spread.p01", "robotics.spread.p02", "robotics.spread.p03",
		"robotics.spread.p04", "robotics.spread.p05", "robotics.spread.p06", "robotics.spread.p07",
		"robotics.spread.p08", "robotics.spread.p09", "robotics.spread.p10", "robotics.spread.p11",
		"robotics.spread.p12", "robotics.spread.p13", "robotics.spread.p14", "robotics.spread.p15",
		"robotics.spread.p16", "robotics.spread.p17", "robotics.spread.p18", "robotics.spread.p19",
	}
	require.LessOrEqual(t, profile.spread, len(spreadValues))
	for i, spreadValue := range spreadValues[:profile.spread] {
		rows = append(rows, seedRow{stores[predicateBucket],
			predicateIndexKey(spreadValue, ownerLoadEntityID(i)), predicateIndexMarker})
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
		serial   int
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
			results <- listResult{label: job.fixture.name, serial: job.serial,
				duration: time.Since(started), count: len(keys), err: err}
		})
	dispatcher.Start(dispatchCtx)

	consumerHighWater := make(map[string]*atomic.Int64, len(fixtures))
	var aggregateConsumerHighWater atomic.Int64
	var aggregateConsumerBaseline int64
	for _, fixture := range fixtures {
		count := int64(baselines[fixture.name])
		consumerHighWater[fixture.name] = &atomic.Int64{}
		consumerHighWater[fixture.name].Store(count)
		aggregateConsumerBaseline += count
	}
	aggregateConsumerHighWater.Store(aggregateConsumerBaseline)
	sampleCtx, stopSampling := context.WithCancel(ctx)
	samplerErrs := make(chan error, 1)
	var samplerWG sync.WaitGroup
	samplerWG.Add(1)
	go func() {
		defer samplerWG.Done()
		for sampleCtx.Err() == nil {
			var aggregate int64
			for _, fixture := range fixtures {
				info, infoErr := fixture.stream.Info(sampleCtx)
				if infoErr != nil {
					if sampleCtx.Err() == nil {
						samplerErrs <- fmt.Errorf("sample %s consumers: %w", fixture.name, infoErr)
					}
					return
				}
				count := int64(info.State.Consumers)
				aggregate += count
				storeOwnerLoadHighWater(consumerHighWater[fixture.name], count)
			}
			storeOwnerLoadHighWater(&aggregateConsumerHighWater, aggregate)
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
	// Results arrive in COMPLETION order; the recorded distribution has to be in SUBMISSION order, so
	// a reader can separate one inflated repetition from two adjacent ones (#1284). Each
	// (fixture, serial) pair is submitted exactly once and exactly resultCount results are consumed,
	// so a duplicate or lost result necessarily leaves a zero slot that the check below rejects —
	// a zero-duration sample would otherwise enter the distribution as the fastest observation.
	durations := make(map[string][]time.Duration, len(fixtures))
	for _, fixture := range fixtures {
		durations[fixture.name] = make([]time.Duration, profile.repetitions)
	}
	for range resultCount {
		result := <-results
		require.NoError(t, result.err, result.label)
		require.Equal(t, 1, result.count, result.label)
		durations[result.label][result.serial] = result.duration
		queueHighWater = max(queueHighWater, ownerLoadQueueDepth(dispatcher))
	}
	for label, samples := range durations {
		for repetition, sample := range samples {
			require.NotZero(t, sample, "%s repetition %d was never recorded", label, repetition)
		}
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
	select {
	case <-dispatcher.done:
	case <-time.After(time.Second):
		t.Fatal("owner-filter dispatcher did not join after parent cancellation")
	}

	for label, samples := range durations {
		assertOwnerLoadLatency(t, label, samples, profile)
	}
	require.LessOrEqual(t, queueHighWater, 1000, "dispatcher queue must remain bounded")
	t.Logf("phase=concurrent workers=%d operations=%d catch_up=%s throughput=%.1f_ops_per_second queue_high_water=%d",
		workers, resultCount, catchUp, float64(resultCount)/catchUp.Seconds(), queueHighWater)

	for _, fixture := range fixtures {
		fixture := fixture
		require.Eventually(t, func() bool {
			info, infoErr := fixture.stream.Info(ctx)
			return infoErr == nil && info.State.Consumers == baselines[fixture.name]
		}, 5*time.Second, 20*time.Millisecond, "%s temporary consumers did not return to baseline", fixture.name)
	}
	afterConsumers := ownerLoadConsumerCounts(t, ctx, fixtures)
	require.Equal(t, baselines, afterConsumers, "temporary consumers must return to every per-store baseline")
	aggregateConsumerAfter := 0
	for _, count := range afterConsumers {
		aggregateConsumerAfter += count
	}
	t.Logf("phase=consumers workers=%d aggregate_baseline=%d aggregate_high=%d aggregate_after=%d predicate_baseline=%d predicate_high=%d predicate_after=%d name_baseline=%d name_high=%d name_after=%d incoming_baseline=%d incoming_high=%d incoming_after=%d",
		workers, aggregateConsumerBaseline, aggregateConsumerHighWater.Load(), aggregateConsumerAfter,
		baselines["predicate"], consumerHighWater["predicate"].Load(), afterConsumers["predicate"],
		baselines["name"], consumerHighWater["name"].Load(), afterConsumers["name"],
		baselines["incoming"], consumerHighWater["incoming"].Load(), afterConsumers["incoming"])

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
	t.Logf("phase=resource workers=%d cpu_before=%.2f cpu_after=%.2f rss_before=%d rss_after=%d subscriptions_before=%d subscriptions_after=%d slow_consumers=%d",
		workers, phaseBefore.CPU, phaseAfter.CPU, phaseBefore.Mem, phaseAfter.Mem,
		phaseBefore.Subscriptions, phaseAfter.Subscriptions, phaseAfter.SlowConsumers)
}

func storeOwnerLoadHighWater(counter *atomic.Int64, value int64) {
	for {
		current := counter.Load()
		if value <= current || counter.CompareAndSwap(current, value) {
			return
		}
	}
}

func TestStoreOwnerLoadHighWater(t *testing.T) {
	var counter atomic.Int64
	storeOwnerLoadHighWater(&counter, 2)
	storeOwnerLoadHighWater(&counter, 1)
	storeOwnerLoadHighWater(&counter, 5)
	require.Equal(t, int64(5), counter.Load())
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
		durations = append(durations, duration)
	}
	assertOwnerLoadLatency(t, label, durations, profile)
}

func assertOwnerLoadLatency(t *testing.T, label string, durations []time.Duration, profile ownerLoadProfile) {
	t.Helper()
	// The caller's slice is in submission order and stays that way: percentiles come from a sorted
	// COPY, because the submission sequence is the evidence this harness now publishes (#1284).
	// Recording happens BEFORE the budget assertions, so a run that breaches a percentile still
	// publishes the distribution that explains the breach.
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p95 := sorted[(len(sorted)-1)*95/100]
	p99 := sorted[(len(sorted)-1)*99/100]
	line := fmt.Sprintf("phase=latency filter=%s reps=%d p50=%s p95=%s p99=%s max=%s submitted=%s",
		label, len(sorted), sorted[len(sorted)/2], p95, p99, sorted[len(sorted)-1],
		ownerLoadSubmissionOrder(durations))
	t.Log(line)
	recordOwnerLoadDistribution(t, line)
	require.LessOrEqual(t, p95, profile.p95Budget, "%s p95", label)
	require.LessOrEqual(t, p99, profile.p99Budget, "%s p99", label)
}

// ownerLoadSubmissionOrder renders the per-repetition durations in the order they were submitted.
// Every value carries its own unit because time.Duration prints one (ns, µs, ms, s); a unit stated
// once, far from the numbers, is the #1286 defect.
func ownerLoadSubmissionOrder(durations []time.Duration) string {
	rendered := make([]string, len(durations))
	for i, duration := range durations {
		rendered[i] = duration.String()
	}
	return strings.Join(rendered, ",")
}

// ownerLoadDistributionLogEnv names a file this harness APPENDS each distribution line to.
//
// go test discards a passing package's output entirely unless -v is passed — measured: a passing
// test writing to t.Log, os.Stdout and os.Stderr produces only "ok <pkg> <time>". CI reaches this
// suite through scripts/run-integration-tests.sh, which runs one un-verbose `go test` over ./...,
// so a green run published nothing at all and the recording above would be invisible where it is
// most needed. The script sets this variable, prints the file after the suite, and removes it:
// the distribution lands in the job log on passing runs without making the whole integration suite
// verbose or running this harness twice. Unset — a plain local `go test` — nothing is written, and
// -v still shows the same line through t.Log.
const ownerLoadDistributionLogEnv = "GRAPH_INDEX_LATENCY_LOG"

func recordOwnerLoadDistribution(t *testing.T, line string) {
	t.Helper()
	path := os.Getenv(ownerLoadDistributionLogEnv)
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err, "open %s=%q", ownerLoadDistributionLogEnv, path)
	_, writeErr := fmt.Fprintf(file, "test=%s %s\n", t.Name(), line)
	closeErr := file.Close()
	require.NoError(t, writeErr, "append to %s=%q", ownerLoadDistributionLogEnv, path)
	require.NoError(t, closeErr, "close %s=%q", ownerLoadDistributionLogEnv, path)
}

func ownerLoadConsumerCounts(t *testing.T, ctx context.Context, fixtures []ownerLoadFixture) map[string]int {
	t.Helper()
	counts := make(map[string]int, len(fixtures))
	for _, fixture := range fixtures {
		info, err := fixture.stream.Info(ctx)
		require.NoError(t, err)
		counts[fixture.name] = info.State.Consumers
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

func ownerLoadChurnIndex(writer, iteration int, profile ownerLoadProfile) int {
	if profile.nameContext <= 1 {
		panic("owner load churn domain must contain a measured owner and at least one non-owner")
	}
	measured := profile.nameContext / 2
	index := (writer*profile.churnPerWriter + iteration) % (profile.nameContext - 1)
	if index >= measured {
		index++
	}
	return index
}

func TestOwnerLoadChurnIndexExcludesMeasuredOwner(t *testing.T) {
	tests := []ownerLoadProfile{
		ownerLoadCIProfile(),
		ownerLoadFullProfile(),
	}
	for _, profile := range tests {
		t.Run(profile.name, func(t *testing.T) {
			measured := profile.nameContext / 2
			measuredEntity := ownerLoadEntityID(measured)
			for _, workers := range profile.workerShapes {
				for writer := 0; writer < workers; writer++ {
					for iteration := 0; iteration < profile.churnPerWriter; iteration++ {
						index := ownerLoadChurnIndex(writer, iteration, profile)
						require.NotEqual(t, measured, index)
						require.GreaterOrEqual(t, index, 0)
						require.Less(t, index, profile.nameContext)
						for _, domain := range []string{"predicate", "name", "incoming"} {
							key, _ := ownerLoadChurnRow(domain, writer, iteration, profile)
							require.NotContains(t, key, measuredEntity, "%s writer %d iteration %d", domain, writer, iteration)
						}
					}
				}
			}
		})
	}
	require.Panics(t, func() {
		ownerLoadChurnIndex(0, 0, ownerLoadProfile{nameContext: 1})
	})
}

func ownerLoadChurnRow(name string, writer, iteration int, profile ownerLoadProfile) (string, []byte) {
	index := ownerLoadChurnIndex(writer, iteration, profile)
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
		panic("unknown owner-load fixture: " + name)
	}
}

func assertOwnerLoadMaxima(t *testing.T, ctx context.Context, js jetstream.JetStream, nc *natsclient.Client) {
	t.Helper()
	raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "OWNER_LOAD_MAXIMA"})
	require.NoError(t, err)
	store := nc.NewKVStore(raw)
	entityID := maximumEntityIDForContract()
	const maximumValue = "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb." +
		"abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb." +
		"abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.Len(t, strings.Split(maximumValue, ".")[0], vocabulary.MaxPredicateSegmentBytes)
	rows := []struct {
		name   string
		key    string
		filter string
		bytes  int
	}{
		{"predicate", predicateIndexKey(maximumValue, entityID), predicateIndexEntityFilter(entityID), 451},
		{"name", nameCompositeKey(nameIndexKey("Maximum"), entityID, maximumValue), nameIndexEntityFilter(entityID), 710},
		{"incoming", incomingIndexKey(entityID, entityID, maximumValue), incomingIndexSourceFilter(entityID), 902},
	}
	for _, row := range rows {
		require.NoError(t, natsclient.ValidateKVLiteralKey(row.key))
		require.NoError(t, natsclient.ValidateKVWildcardFilter(row.filter))
		require.Len(t, row.key, row.bytes, row.name)
		_, err := store.Put(ctx, row.key, []byte{1})
		require.NoError(t, err)
		keys, err := store.KeysByFilter(ctx, row.filter)
		require.NoError(t, err)
		require.Equal(t, []string{row.key}, keys)
	}
	// OUTGOING_INDEX is keyed directly by the source entity rather than an owner
	// wildcard. Exercise its production Put/Get shape at the governed 256-byte
	// entity maximum in the same real-NATS bucket.
	require.NoError(t, natsclient.ValidateKVLiteralKey(entityID))
	require.Len(t, entityID, 256, "outgoing")
	outgoingValue := []byte(`[{"to_entity_id":"acme.ops.load.graph.target.hub","predicate":"robotics.assigned.hub"}]`)
	_, err = store.Put(ctx, entityID, outgoingValue)
	require.NoError(t, err)
	outgoingEntry, err := store.Get(ctx, entityID)
	require.NoError(t, err)
	require.Equal(t, outgoingValue, outgoingEntry.Value)
	t.Logf("phase=maxima entity_bytes=%d predicate_bytes=%d key_bytes predicate=451 name=710 incoming=902 outgoing=256",
		len(entityID), len(maximumValue))
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
