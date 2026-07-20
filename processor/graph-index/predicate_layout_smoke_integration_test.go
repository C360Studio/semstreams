//go:build integration

package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/vocabulary"
	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

const (
	predicateSmokeNATSServer = "2.12.4-alpine@sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea"
	predicateSmokeNATSSDK    = "v1.48.0"
	predicateSmokeHot        = "robotics.status.ready"
)

type predicateSmokeCodec struct {
	name             string
	membershipBucket string
	catalogBucket    string
	memberKey        func(string, string) string
	exactFilter      func(string) string
	namespaceFilters func(string) []string
	ownerFilter      func(string) string
}

func predicateSmokeHashCatalogCodec() predicateSmokeCodec {
	return predicateSmokeCodec{
		name:             "hash-catalog",
		membershipBucket: "PRED_SMOKE_HASH_MEMBERS",
		catalogBucket:    "PRED_SMOKE_HASH_CATALOG",
		memberKey:        hashPredicateCandidateKey,
		exactFilter:      hashPredicateCandidateForwardFilter,
		ownerFilter:      hashPredicateCandidateOwnerFilter,
	}
}

func predicateSmokeRawCodec() predicateSmokeCodec {
	return predicateSmokeCodec{
		name:             "raw-nine-token",
		membershipBucket: "PRED_SMOKE_RAW_MEMBERS",
		memberKey:        predicateIndexKey,
		exactFilter: func(predicate string) string {
			return predicateIndexForwardFilters(predicate)[0]
		},
		namespaceFilters: predicateIndexForwardFilters,
		ownerFilter:      predicateIndexEntityFilter,
	}
}

type predicateSmokeProfile struct {
	name              string
	entities          int
	spread            int
	churnWriters      int
	churnPerWriter    int
	repetitions       int
	operationBudget   time.Duration
	p95Budget         time.Duration
	p99Budget         time.Duration
	maxServerRSSBytes int64
}

func activePredicateSmokeProfile() predicateSmokeProfile {
	if os.Getenv("PREDICATE_SMOKE_FULL") == "1" {
		return predicateSmokeProfile{
			name: "full", entities: 21_000, spread: 20, churnWriters: 4, churnPerWriter: 500,
			repetitions: 30, operationBudget: 10 * time.Second,
			p95Budget: 3 * time.Second, p99Budget: 5 * time.Second, maxServerRSSBytes: 2 << 30,
		}
	}
	// CI budgets carry ≥3× headroom over observed healthy latencies per the
	// wall-clock-assertion discipline (gh#220): shared-runner contention put a
	// namespace-domain wildcard at 3.29s against the old 3s budget (2026-07-18,
	// second flake mode of the gh#555 class) with healthy p95 already 2.65s.
	// The CI smoke exists to catch order-of-magnitude regressions; tight
	// budgets belong to the opt-in "full" profile on a quiet box.
	return predicateSmokeProfile{
		name: "ci", entities: 5_000, spread: 20, churnWriters: 2, churnPerWriter: 100,
		repetitions: 5, operationBudget: 10 * time.Second,
		p95Budget: 8 * time.Second, p99Budget: 9 * time.Second, maxServerRSSBytes: 1 << 30,
	}
}

type predicateSmokeStores struct {
	membershipRaw    jetstream.KeyValue
	membership       *natsclient.KVStore
	membershipStream jetstream.Stream
	catalogRaw       jetstream.KeyValue
	catalog          *natsclient.KVStore
	catalogStream    jetstream.Stream
}

type predicateSmokeTruth struct {
	hotKeys       []string
	allKeys       []string
	catalogKeys   []string
	ownerKey      string
	maximumKey    string
	maximumFilter string
}

type predicateSmokeServerStats struct {
	CPU           float64 `json:"cpu"`
	Mem           int64   `json:"mem"`
	Subscriptions uint64  `json:"subscriptions"`
	Connections   int     `json:"connections"`
	SlowConsumers int64   `json:"slow_consumers"`
}

// TestIntegration_PredicateLayoutSmoke is a decision harness, not a production
// selector. Both candidates must satisfy the same absolute gates; their results
// are recorded independently and are never compared as pass/fail thresholds.
func TestIntegration_PredicateLayoutSmoke(t *testing.T) {
	profile := activePredicateSmokeProfile()
	t.Logf("phase=setup profile=%s entities=%d spread=%d reps=%d churn_writers=%d server=%s sdk=%s",
		profile.name, profile.entities, profile.spread, profile.repetitions, profile.churnWriters,
		predicateSmokeNATSServer, predicateSmokeNATSSDK)
	for _, codec := range []predicateSmokeCodec{
		predicateSmokeHashCatalogCodec(),
		predicateSmokeRawCodec(),
	} {
		t.Run(codec.name, func(t *testing.T) {
			runPredicateLayoutSmoke(t, codec, profile)
		})
	}
}

func runPredicateLayoutSmoke(t *testing.T, codec predicateSmokeCodec, profile predicateSmokeProfile) {
	t.Helper()
	testClient := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithFileStorage(),
		natsclient.WithNATSVersion(predicateSmokeNATSServer),
		natsclient.WithTestTimeout(15*time.Second),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	stores := createPredicateSmokeStores(t, ctx, js, testClient.Client, codec)

	before := scrapePredicateSmokeServerStats(t, testClient.MonitoringURL)
	assertPredicateSmokeServerBounds(t, profile, "before-seed", before)
	truth := seedPredicateSmoke(t, ctx, codec, stores, profile)
	afterSeed := scrapePredicateSmokeServerStats(t, testClient.MonitoringURL)
	assertPredicateSmokeServerBounds(t, profile, "after-seed", afterSeed)
	t.Logf("phase=seed codec=%s rss_before=%d rss_after=%d rss_delta=%d subscriptions=%d",
		codec.name, before.Mem, afterSeed.Mem, afterSeed.Mem-before.Mem, afterSeed.Subscriptions)

	consumerEvidence := provePredicateSmokeConsumerLifecycle(t, ctx, stores)
	measurePredicateSmokeQuiescent(t, ctx, codec, stores, truth, profile)
	measurePredicateSmokeChurn(t, ctx, codec, stores, truth, profile)
	assertPredicateSmokeRestartParity(t, ctx, js, testClient.Client, codec, truth)
	assertPredicateSmokeConsumerReturn(t, ctx, stores, consumerEvidence)
	membershipAfter := predicateSmokeConsumerCount(t, ctx, stores.membershipStream)
	catalogAfter := 0
	if stores.catalogStream != nil {
		catalogAfter = predicateSmokeConsumerCount(t, ctx, stores.catalogStream)
	}

	after := scrapePredicateSmokeServerStats(t, testClient.MonitoringURL)
	assertPredicateSmokeServerBounds(t, profile, "after-workload", after)
	require.LessOrEqual(t, after.Subscriptions, before.Subscriptions+2,
		"temporary predicate listing subscriptions must be released")
	require.Zero(t, after.SlowConsumers, "predicate layout smoke must not create slow consumers")
	t.Logf("phase=resource codec=%s cpu_before=%.2f cpu_after=%.2f rss_before=%d rss_after=%d subscriptions_before=%d subscriptions_after=%d slow_consumers=%d membership_consumer_baseline=%d membership_consumer_high_water=%d membership_consumer_after=%d catalog_consumer_baseline=%d catalog_consumer_high_water=%d catalog_consumer_after=%d",
		codec.name, before.CPU, after.CPU, before.Mem, after.Mem, before.Subscriptions,
		after.Subscriptions, after.SlowConsumers, consumerEvidence.membershipBaseline,
		consumerEvidence.membershipHighWater, membershipAfter, consumerEvidence.catalogBaseline,
		consumerEvidence.catalogHighWater, catalogAfter)
}

func createPredicateSmokeStores(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
	nc *natsclient.Client,
	codec predicateSmokeCodec,
) predicateSmokeStores {
	t.Helper()
	require.Regexp(t, `^[A-Z0-9_]+$`, codec.membershipBucket)
	membershipRaw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: codec.membershipBucket, Storage: jetstream.FileStorage,
	})
	require.NoError(t, err)
	membershipStream, err := js.Stream(ctx, "KV_"+codec.membershipBucket)
	require.NoError(t, err)
	stores := predicateSmokeStores{
		membershipRaw: membershipRaw, membership: nc.NewKVStore(membershipRaw), membershipStream: membershipStream,
	}
	if codec.catalogBucket != "" {
		require.Regexp(t, `^[A-Z0-9_]+$`, codec.catalogBucket)
		catalogRaw, createErr := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: codec.catalogBucket, Storage: jetstream.FileStorage,
		})
		require.NoError(t, createErr)
		catalogStream, streamErr := js.Stream(ctx, "KV_"+codec.catalogBucket)
		require.NoError(t, streamErr)
		stores.catalogRaw = catalogRaw
		stores.catalog = nc.NewKVStore(catalogRaw)
		stores.catalogStream = catalogStream
	}
	return stores
}

func seedPredicateSmoke(
	t *testing.T,
	ctx context.Context,
	codec predicateSmokeCodec,
	stores predicateSmokeStores,
	profile predicateSmokeProfile,
) predicateSmokeTruth {
	t.Helper()
	owner := predicateSmokeEntityID(profile.entities / 2)
	truth := predicateSmokeTruth{
		hotKeys:     make([]string, 0, profile.entities),
		allKeys:     make([]string, 0, profile.entities+profile.spread),
		catalogKeys: make([]string, 0, profile.spread+2),
		ownerKey:    codec.memberKey(predicateSmokeHot, owner),
	}
	type row struct {
		key   string
		value []byte
	}
	rows := make([]row, 0, profile.entities+profile.spread+1)
	for i := 0; i < profile.entities; i++ {
		key := codec.memberKey(predicateSmokeHot, predicateSmokeEntityID(i))
		require.NoError(t, natsclient.ValidateKVLiteralKey(key))
		truth.hotKeys = append(truth.hotKeys, key)
		truth.allKeys = append(truth.allKeys, key)
		rows = append(rows, row{key: key, value: predicateIndexMarker})
	}
	truth.catalogKeys = append(truth.catalogKeys, predicateSmokeHot)
	for i := 0; i < profile.spread; i++ {
		predicate := predicateSmokeSpreadPredicate(i)
		key := codec.memberKey(predicate, predicateSmokeEntityID(i))
		require.NoError(t, natsclient.ValidateKVLiteralKey(key))
		truth.allKeys = append(truth.allKeys, key)
		truth.catalogKeys = append(truth.catalogKeys, predicate)
		rows = append(rows, row{key: key, value: predicateIndexMarker})
	}

	maximumEntity := maximumEntityIDForContract()
	maximumPredicate := predicateSmokeMaximumPredicate()
	truth.maximumKey = codec.memberKey(maximumPredicate, maximumEntity)
	truth.maximumFilter = codec.ownerFilter(maximumEntity)
	require.NoError(t, natsclient.ValidateKVLiteralKey(truth.maximumKey))
	require.NoError(t, natsclient.ValidateKVWildcardFilter(truth.maximumFilter))
	if codec.name == "raw-nine-token" {
		require.Len(t, truth.maximumKey, 451)
	} else {
		require.Len(t, truth.maximumKey, 321)
	}
	rows = append(rows, row{key: truth.maximumKey, value: predicateIndexMarker})
	truth.catalogKeys = append(truth.catalogKeys, maximumPredicate)

	for _, filter := range append([]string{codec.exactFilter(predicateSmokeHot), codec.ownerFilter(owner)},
		predicateSmokeNamespaceFilters(codec, predicateSmokeHot)...) {
		require.NoError(t, natsclient.ValidateKVWildcardFilter(filter))
	}

	seedStarted := time.Now()
	jobs := make(chan row, 256)
	errs := make(chan error, len(rows))
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for next := range jobs {
				if _, err := stores.membership.Put(ctx, next.key, next.value); err != nil {
					errs <- err
				}
			}
		}()
	}
	for _, next := range rows {
		jobs <- next
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for seedErr := range errs {
		require.NoError(t, seedErr)
	}

	if stores.catalog != nil {
		for _, predicate := range truth.catalogKeys {
			require.NoError(t, natsclient.ValidateKVLiteralKey(predicate))
			_, err := stores.catalog.Put(ctx, predicate, predicateIndexMarker)
			require.NoError(t, err)
		}
	}
	sort.Strings(truth.hotKeys)
	sort.Strings(truth.allKeys)
	sort.Strings(truth.catalogKeys)
	elapsed := time.Since(seedStarted)
	catalogRows := 0
	if stores.catalog != nil {
		catalogRows = len(truth.catalogKeys)
	}
	t.Logf("phase=seed-throughput codec=%s membership_rows=%d catalog_rows=%d elapsed=%s throughput=%.1f_rows_per_second maximum_key_bytes=%d",
		codec.name, len(rows), catalogRows, elapsed,
		float64(len(rows)+catalogRows)/elapsed.Seconds(), len(truth.maximumKey))
	return truth
}

func measurePredicateSmokeQuiescent(
	t *testing.T,
	ctx context.Context,
	codec predicateSmokeCodec,
	stores predicateSmokeStores,
	truth predicateSmokeTruth,
	profile predicateSmokeProfile,
) {
	t.Helper()
	measurePredicateSmokeOperation(t, profile, codec.name+"-exact", func() ([]string, error) {
		return stores.membership.KeysByFilter(ctx, codec.exactFilter(predicateSmokeHot))
	}, truth.hotKeys)
	measurePredicateSmokeOperation(t, profile, codec.name+"-owner", func() ([]string, error) {
		return stores.membership.KeysByFilter(ctx, codec.ownerFilter(predicateSmokeEntityID(profile.entities/2)))
	}, []string{truth.ownerKey})
	measurePredicateSmokeOperation(t, profile, codec.name+"-maximum-owner", func() ([]string, error) {
		return stores.membership.KeysByFilter(ctx, truth.maximumFilter)
	}, []string{truth.maximumKey})

	if stores.catalog != nil {
		measurePredicateSmokeOperation(t, profile, codec.name+"-namespace-catalog-join", func() ([]string, error) {
			return predicateSmokeHashNamespaceJoin(ctx, stores, "robotics.*.*")
		}, truth.allKeys)
		catalogKeys, err := stores.catalog.KeysByFilter(ctx, ">")
		require.NoError(t, err)
		sort.Strings(catalogKeys)
		require.Equal(t, truth.catalogKeys, catalogKeys)
		return
	}

	filters := predicateSmokeNamespaceFilters(codec, predicateSmokeHot)
	require.Len(t, filters, 2)
	measurePredicateSmokeOperation(t, profile, codec.name+"-namespace-category", func() ([]string, error) {
		return stores.membership.KeysByFilter(ctx, filters[0])
	}, truth.hotKeys)
	measurePredicateSmokeOperation(t, profile, codec.name+"-namespace-domain", func() ([]string, error) {
		return stores.membership.KeysByFilter(ctx, filters[1])
	}, truth.allKeys)
}

func predicateSmokeHashNamespaceJoin(
	ctx context.Context, stores predicateSmokeStores, catalogFilter string,
) ([]string, error) {
	predicates, err := stores.catalog.KeysByFilter(ctx, catalogFilter)
	if err != nil {
		return nil, err
	}
	sort.Strings(predicates)
	var keys []string
	for _, predicate := range predicates {
		members, listErr := stores.membership.KeysByFilter(ctx, hashPredicateCandidateForwardFilter(predicate))
		if listErr != nil {
			return nil, fmt.Errorf("join predicate %q: %w", predicate, listErr)
		}
		keys = append(keys, members...)
	}
	return keys, nil
}

func measurePredicateSmokeChurn(
	t *testing.T,
	ctx context.Context,
	codec predicateSmokeCodec,
	stores predicateSmokeStores,
	truth predicateSmokeTruth,
	profile predicateSmokeProfile,
) {
	t.Helper()
	churnErrs := make(chan error, profile.churnWriters)
	var churnWG sync.WaitGroup
	for writer := 0; writer < profile.churnWriters; writer++ {
		churnWG.Add(1)
		go func(writer int) {
			defer churnWG.Done()
			for iteration := 0; iteration < profile.churnPerWriter; iteration++ {
				entityIndex := (writer*profile.churnPerWriter + iteration) % profile.entities
				key := codec.memberKey(predicateSmokeHot, predicateSmokeEntityID(entityIndex))
				var err error
				if iteration%2 == 0 {
					err = stores.membership.Delete(ctx, key)
				} else {
					_, err = stores.membership.Put(ctx, key, predicateIndexMarker)
				}
				if err != nil {
					churnErrs <- fmt.Errorf("writer %d iteration %d key %q: %w", writer, iteration, key, err)
					return
				}
			}
		}(writer)
	}

	allowed := make(map[string]struct{}, len(truth.hotKeys))
	for _, key := range truth.hotKeys {
		allowed[key] = struct{}{}
	}
	durations := make([]time.Duration, 0, profile.repetitions)
	for repetition := 0; repetition < profile.repetitions; repetition++ {
		started := time.Now()
		keys, err := stores.membership.KeysByFilter(ctx, codec.exactFilter(predicateSmokeHot))
		duration := time.Since(started)
		require.NoError(t, err)
		require.Less(t, duration, profile.operationBudget)
		for _, key := range keys {
			_, exists := allowed[key]
			require.True(t, exists, "churn listing returned false member %q", key)
		}
		durations = append(durations, duration)
	}
	churnWG.Wait()
	close(churnErrs)
	for churnErr := range churnErrs {
		require.NoError(t, churnErr)
	}
	assertPredicateSmokeLatencies(t, codec.name+"-exact-under-churn", durations, profile)

	for _, key := range truth.hotKeys {
		_, err := stores.membership.Put(ctx, key, predicateIndexMarker)
		require.NoError(t, err)
	}
	final, err := stores.membership.KeysByFilter(ctx, codec.exactFilter(predicateSmokeHot))
	require.NoError(t, err)
	sort.Strings(final)
	require.Equal(t, truth.hotKeys, final, "churn must converge to exact seeded truth")
	t.Logf("phase=churn codec=%s writers=%d mutations=%d convergence=exact",
		codec.name, profile.churnWriters, profile.churnWriters*profile.churnPerWriter)
}

func measurePredicateSmokeOperation(
	t *testing.T,
	profile predicateSmokeProfile,
	label string,
	operation func() ([]string, error),
	want []string,
) {
	t.Helper()
	durations := make([]time.Duration, 0, profile.repetitions)
	want = append([]string(nil), want...)
	sort.Strings(want)
	for repetition := 0; repetition < profile.repetitions; repetition++ {
		started := time.Now()
		got, err := operation()
		duration := time.Since(started)
		require.NoError(t, err, label)
		require.Less(t, duration, profile.operationBudget, "%s rep %d", label, repetition)
		sort.Strings(got)
		require.Equal(t, want, got, "%s rep %d", label, repetition)
		durations = append(durations, duration)
	}
	assertPredicateSmokeLatencies(t, label, durations, profile)
}

func assertPredicateSmokeLatencies(
	t *testing.T, label string, durations []time.Duration, profile predicateSmokeProfile,
) {
	t.Helper()
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := predicateSmokePercentile(durations, 0.95)
	p99 := predicateSmokePercentile(durations, 0.99)
	require.LessOrEqual(t, p95, profile.p95Budget, "%s p95", label)
	require.LessOrEqual(t, p99, profile.p99Budget, "%s p99", label)
	t.Logf("phase=latency operation=%s reps=%d p50=%s p95=%s p99=%s max=%s",
		label, len(durations), predicateSmokePercentile(durations, 0.50), p95, p99, durations[len(durations)-1])
}

func predicateSmokePercentile(sorted []time.Duration, percentile float64) time.Duration {
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

type predicateSmokeConsumerEvidence struct {
	membershipBaseline  int
	membershipHighWater int
	catalogBaseline     int
	catalogHighWater    int
}

// catalogConsumerVisibilityRequired is false because catalog streams are tiny
// (~22 rows): the ephemeral list consumer can deliver every key and vanish between
// Info polls, so observing it above baseline is inherently racy (gh#555).
//
// ONE constant feeds BOTH the observation and the assertion below. They disagreed
// before — the observation was best-effort while the assertion demanded proof — so
// a loaded machine that starved the polling goroutine failed a test whose own
// comment said the case was tolerated. The leak check stays strict either way; that
// is the invariant actually under test (a temporary consumer that cleans up), and
// unlike visibility it cannot be missed by slow polling.
const catalogConsumerVisibilityRequired = false

func provePredicateSmokeConsumerLifecycle(
	t *testing.T, ctx context.Context, stores predicateSmokeStores,
) predicateSmokeConsumerEvidence {
	t.Helper()
	evidence := predicateSmokeConsumerEvidence{}
	evidence.membershipBaseline, evidence.membershipHighWater = provePredicateSmokeBucketConsumer(
		t, ctx, stores.membershipRaw, stores.membershipStream, ">", true)
	if stores.catalogRaw != nil {
		// Observation-only; the leak check stays strict. See
		// catalogConsumerVisibilityRequired for why.
		evidence.catalogBaseline, evidence.catalogHighWater = provePredicateSmokeBucketConsumer(
			t, ctx, stores.catalogRaw, stores.catalogStream, ">", catalogConsumerVisibilityRequired)
	}
	return evidence
}

func provePredicateSmokeBucketConsumer(
	t *testing.T,
	ctx context.Context,
	raw jetstream.KeyValue,
	stream jetstream.Stream,
	filter string,
	requireVisible bool,
) (int, int) {
	t.Helper()
	baselineInfo, err := stream.Info(ctx)
	require.NoError(t, err)
	baseline := baselineInfo.State.Consumers
	lister, err := raw.ListKeysFiltered(ctx, filter)
	require.NoError(t, err)
	highWater := baseline
	observeVisibility := func() bool {
		info, infoErr := stream.Info(ctx)
		if infoErr != nil {
			return false
		}
		highWater = max(highWater, info.State.Consumers)
		return highWater > baseline
	}
	if requireVisible {
		require.Eventually(t, observeVisibility, 5*time.Second, time.Millisecond,
			"filtered list consumer was never visible above baseline")
	} else {
		// Best-effort observation for streams too small to guarantee the
		// consumer outlives the polling granularity (gh#555): a consumer
		// that delivered all keys can be gone before the first Info poll —
		// the Stop() tolerance for ErrBadSubscription below anticipates
		// exactly that fast-completion case.
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && !observeVisibility() {
			time.Sleep(time.Millisecond)
		}
		t.Logf("consumer visibility (observation-only): baseline=%d high_water=%d", baseline, highWater)
	}
	stopErr := lister.Stop()
	require.True(t, stopErr == nil || errors.Is(stopErr, gonats.ErrBadSubscription),
		"stop held key lister: %v", stopErr)
	for range lister.Keys() {
	}
	require.Eventually(t, func() bool {
		info, infoErr := stream.Info(ctx)
		return infoErr == nil && info.State.Consumers == baseline
	}, 5*time.Second, time.Millisecond, "filtered list consumer did not return to baseline")
	return baseline, highWater
}

func assertPredicateSmokeConsumerReturn(
	t *testing.T,
	ctx context.Context,
	stores predicateSmokeStores,
	evidence predicateSmokeConsumerEvidence,
) {
	t.Helper()
	assertStream := func(label string, stream jetstream.Stream, baseline, highWater int, proveAllocation bool) {
		if proveAllocation {
			require.Greater(t, highWater, baseline, "%s consumer high-water must prove temporary allocation", label)
		}
		// The leak check is unconditional: whether or not the consumer was ever
		// caught in the act, it must not still be there.
		require.Eventually(t, func() bool {
			info, err := stream.Info(ctx)
			return err == nil && info.State.Consumers == baseline
		}, 5*time.Second, time.Millisecond, "%s consumers did not return to baseline", label)
	}
	assertStream("membership", stores.membershipStream,
		evidence.membershipBaseline, evidence.membershipHighWater, true)
	if stores.catalogStream != nil {
		assertStream("catalog", stores.catalogStream,
			evidence.catalogBaseline, evidence.catalogHighWater, catalogConsumerVisibilityRequired)
	}
}

func predicateSmokeConsumerCount(t *testing.T, ctx context.Context, stream jetstream.Stream) int {
	t.Helper()
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	return info.State.Consumers
}

func assertPredicateSmokeRestartParity(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
	nc *natsclient.Client,
	codec predicateSmokeCodec,
	truth predicateSmokeTruth,
) {
	t.Helper()
	raw, err := js.KeyValue(ctx, codec.membershipBucket)
	require.NoError(t, err)
	restarted := nc.NewKVStore(raw)
	keys, err := restarted.KeysByFilter(ctx, codec.exactFilter(predicateSmokeHot))
	require.NoError(t, err)
	sort.Strings(keys)
	require.Equal(t, truth.hotKeys, keys)
	if codec.catalogBucket != "" {
		catalogRaw, catalogErr := js.KeyValue(ctx, codec.catalogBucket)
		require.NoError(t, catalogErr)
		catalog := nc.NewKVStore(catalogRaw)
		catalogKeys, catalogErr := catalog.KeysByFilter(ctx, ">")
		require.NoError(t, catalogErr)
		sort.Strings(catalogKeys)
		require.Equal(t, truth.catalogKeys, catalogKeys)
	}
	t.Logf("phase=restart codec=%s fresh_bucket_handles=parity", codec.name)
}

func predicateSmokeNamespaceFilters(codec predicateSmokeCodec, predicate string) []string {
	if codec.namespaceFilters == nil {
		return []string{"robotics.*.*"}
	}
	all := codec.namespaceFilters(predicate)
	if len(all) != 3 {
		return nil
	}
	return all[1:]
}

func predicateSmokeEntityID(index int) string {
	return fmt.Sprintf("acme.ops.smoke.graph.entity.%06d", index)
}

func predicateSmokeSpreadPredicate(index int) string {
	return fmt.Sprintf("robotics.spread.p%02d", index)
}

func predicateSmokeMaximumPredicate() string {
	segment := "a" + strings.Repeat("b", vocabulary.MaxPredicateSegmentBytes-1)
	return segment + "." + segment + "." + segment
}

func scrapePredicateSmokeServerStats(t *testing.T, monitoringURL string) predicateSmokeServerStats {
	t.Helper()
	require.NotEmpty(t, monitoringURL, "NATS monitoring URL is mandatory evidence")
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(monitoringURL + "/varz") //nolint:gosec // mapped testcontainers endpoint
	require.NoError(t, err, "NATS /varz scrape is a hard predicate-layout gate dependency")
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var stats predicateSmokeServerStats
	require.NoError(t, json.NewDecoder(response.Body).Decode(&stats))
	require.False(t, math.IsNaN(stats.CPU))
	require.False(t, math.IsInf(stats.CPU, 0))
	require.GreaterOrEqual(t, stats.CPU, 0.0)
	require.Greater(t, stats.Mem, int64(0))
	require.GreaterOrEqual(t, stats.Subscriptions, uint64(1))
	require.GreaterOrEqual(t, stats.Connections, 1)
	return stats
}

func assertPredicateSmokeServerBounds(
	t *testing.T, profile predicateSmokeProfile, phase string, stats predicateSmokeServerStats,
) {
	t.Helper()
	require.LessOrEqual(t, stats.CPU, float64(runtime.NumCPU()*100+1), "%s NATS CPU exceeds host capacity", phase)
	require.Less(t, stats.Mem, profile.maxServerRSSBytes, "%s NATS RSS exceeded profile ceiling", phase)
	require.Zero(t, stats.SlowConsumers, "%s NATS slow consumers", phase)
}
