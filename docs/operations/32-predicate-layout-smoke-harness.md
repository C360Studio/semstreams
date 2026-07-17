# Predicate Layout Smoke Harness

Working skeleton for the `predicate-raw-key-representation` decision gates (tasks 1.4 and 2.1): verify the raw
nine-token PREDICATE layout sits inside NATS's designed envelope, and record the hash+catalog comparison as ADR
evidence. This is a smoke test, not a tournament — budgets are absolute (the ADR-065 guard family), and the
side-by-side numbers are recorded, never used as selection thresholds.

Intended home when implemented: `processor/graph-index/predicate_layout_smoke_integration_test.go` (build tag
`integration`), reusing the shared 21k churn profile from the `graph-index-replacement-semantics` owner-filter
proof. The five `TODO(codex)` markers are the open wiring decisions: server monitoring-port exposure (varz scrape
or docker-stats fallback), the worst-case 451-byte member, spread predicates, the catalog-join measurement mirror,
and the temporary-consumer return-to-baseline assert.

Record with any full-profile result: the profile constants, the pinned nats-server image digest, and the SDK
version (the kv-contract SDK matrix governs both).

```go
//go:build integration

// PREDICATE layout smoke harness — SKETCH for rule task predicate-raw-key-representation 1.4/2.1.
//
// Verifies our usage pattern sits inside NATS's designed envelope for the raw
// nine-token candidate, and records the hash+catalog comparison as ADR
// evidence. Budgets are ABSOLUTE (the ADR-065 guard family), never
// comparative thresholds:
//
//   - every measured list operation completes < 3s at the CI profile;
//   - p95 <= 3s, p99 <= 5s at the full profile;
//   - temporary filtered consumers return to baseline after each phase;
//   - churn converges: after writers quiesce, the exact match set equals the
//     seeded truth (zero false/missing/stale keys).
//
// Two run shapes from one harness:
//   - CI guard (default): 5,000 hot members + 20 spread predicates. Runs in
//     the normal -tags=integration sweep.
//   - Full decision profile: 21,000 entities, PREDICATE_SMOKE_FULL=1. Run
//     once per candidate for the ADR record; not a per-PR gate.
//
// Server-side resource evidence: enable the NATS monitoring port on the test
// server and scrape /varz before/after each phase (see serverStats TODO).
// nats-server also accepts -profile <port> for live pprof if a phase looks
// pathological — that is a diagnosis tool, not part of the recorded evidence.
//
// Record with the result: profile constants, nats-server image digest (the
// pinned matrix from the kv-contract work), and SDK version.

package graphindex

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Codec seam: the ONLY thing that differs between candidates.
// ---------------------------------------------------------------------------

type predicateLayoutCodec struct {
	name string
	// memberKey builds the membership key for one (predicate, entity) pair.
	memberKey func(predicate, entityID string) string
	// exactFilter enumerates one predicate's complete membership.
	exactFilter func(predicate string) string
	// namespaceFilters enumerate domain and domain.category memberships.
	// Hash+catalog has none (namespace queries go through the catalog join);
	// the harness measures its catalog-scan+join path instead.
	namespaceFilters func(predicate string) []string
	// ownerFilter enumerates one entity's memberships (leading wildcard —
	// the shape NATS could plausibly surprise us on).
	ownerFilter func(entityID string) string
	// extraWrites per membership (the catalog Put for hash; nil for raw).
	extraWrites func(kv *natsclient.KVStore, ctx context.Context, predicate string) error
}

func hashCatalogCodec() predicateLayoutCodec {
	return predicateLayoutCodec{
		name:      "hash+catalog",
		memberKey: predicateIndexKey,
		exactFilter: func(p string) string {
			return predicateIndexForwardFilter(p) // hash.*x6
		},
		namespaceFilters: nil, // namespace = catalog scan + join; measured separately
		ownerFilter: func(e string) string {
			return "*." + e // 7 positions: hash token + entity6
		},
		extraWrites: func(kv *natsclient.KVStore, ctx context.Context, p string) error {
			_, err := kv.Put(ctx, p, []byte{1}) // PREDICATE_CATALOG name-recovery row
			return err
		},
	}
}

func rawNineTokenCodec() predicateLayoutCodec {
	return predicateLayoutCodec{
		name:      "raw-nine-token",
		memberKey: rawPredicateCandidateKey, // predicate3.entity6 — kv_contract_benchmark.go
		exactFilter: func(p string) string {
			return rawPredicateCandidateForwardFilters(p)[0] // pred3.*x6
		},
		namespaceFilters: func(p string) []string {
			return rawPredicateCandidateForwardFilters(p)[1:] // domain.category.*x7, domain.*x8
		},
		ownerFilter: rawPredicateCandidateOwnerFilter, // *.*.*.entity6
		extraWrites: nil,                              // no catalog
	}
}

// ---------------------------------------------------------------------------
// Profile
// ---------------------------------------------------------------------------

type smokeProfile struct {
	entities       int // members of the hot predicate
	spread         int // additional predicates with one member each
	churnWriters   int // concurrent Put/Delete goroutines during listing
	reps           int // measured repetitions per filter shape
	perOpBudget    time.Duration
	p95Budget      time.Duration
	p99Budget      time.Duration
}

func activeProfile() smokeProfile {
	if os.Getenv("PREDICATE_SMOKE_FULL") == "1" {
		return smokeProfile{entities: 21_000, spread: 20, churnWriters: 4, reps: 30,
			perOpBudget: 10 * time.Second, p95Budget: 3 * time.Second, p99Budget: 5 * time.Second}
	}
	// ADR-065 CI guard shape.
	return smokeProfile{entities: 5_000, spread: 20, churnWriters: 2, reps: 5,
		perOpBudget: 3 * time.Second, p95Budget: 3 * time.Second, p99Budget: 3 * time.Second}
}

const hotPredicate = "robotics.assigned.mission"

// maxPredicate + maxEntity exercise the proven 451-byte worst-case raw key.
// TODO(codex): build from pkg/types.MaxEntityIDBytes and the 194-byte
// canonical predicate maximum, mirroring the kv-contract boundary tests.

func smokeEntityID(i int) string {
	return fmt.Sprintf("acme.ops.robotics.gcs.drone.%06d", i)
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

func TestPredicateLayoutSmoke(t *testing.T) {
	profile := activeProfile()
	for _, codec := range []predicateLayoutCodec{hashCatalogCodec(), rawNineTokenCodec()} {
		t.Run(codec.name, func(t *testing.T) {
			runPredicateLayoutSmoke(t, codec, profile)
		})
	}
	// TODO(codex): after both subtests, log the side-by-side table (latency
	// percentiles, matched-key counts, varz deltas) — recorded as ADR
	// evidence, asserted only against the absolute budgets above.
}

func runPredicateLayoutSmoke(t *testing.T, codec predicateLayoutCodec, profile smokeProfile) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	// TODO(codex): NewTestClient does not expose the server monitoring port
	// today. Either extend the helper (-m 8222, -profile 6060) or scrape
	// docker stats for the container as the CPU/RSS fallback. Silence here
	// is not success — the full-profile run MUST record server deltas.
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "PRED_SMOKE_" + strings.ToUpper(strings.ReplaceAll(codec.name, "-", "_"))[:16]})
	require.NoError(t, err)
	kv := testClient.Client.NewKVStore(raw)

	// --- Phase 0: preflight every key and filter through the shared contract
	// BEFORE any I/O (house discipline: a failed proof is side-effect free).
	expected := make([]string, 0, profile.entities)
	for i := 0; i < profile.entities; i++ {
		key := codec.memberKey(hotPredicate, smokeEntityID(i))
		require.NoError(t, natsclient.ValidateKVLiteralKey(key))
		expected = append(expected, key)
	}
	require.NoError(t, natsclient.ValidateKVWildcardFilter(codec.exactFilter(hotPredicate)))
	require.NoError(t, natsclient.ValidateKVWildcardFilter(codec.ownerFilter(smokeEntityID(0))))
	if codec.namespaceFilters != nil {
		for _, f := range codec.namespaceFilters(hotPredicate) {
			require.NoError(t, natsclient.ValidateKVWildcardFilter(f))
		}
	}

	stats := scrapeServerStats(t) // varz snapshot: cpu, mem, subscriptions

	// --- Phase 1: seed. One hot predicate spanning all entities + spread
	// predicates + the worst-case-length member (TODO). Include catalog
	// writes for the hash codec so its write amplification is measured.
	seedStart := time.Now()
	for i := 0; i < profile.entities; i++ {
		_, err := kv.Put(ctx, expected[i], []byte{1})
		require.NoError(t, err)
	}
	if codec.extraWrites != nil {
		require.NoError(t, codec.extraWrites(kv, ctx, hotPredicate))
	}
	// TODO(codex): spread predicates (profile.spread), worst-case key.
	t.Logf("%s: seeded %d members in %s", codec.name, profile.entities, time.Since(seedStart))

	// --- Phase 2: quiescent listing, three filter shapes.
	measureFiltered(t, kv, ctx, "exact", codec.exactFilter(hotPredicate), profile, profile.entities)
	if codec.namespaceFilters != nil {
		for i, f := range codec.namespaceFilters(hotPredicate) {
			measureFiltered(t, kv, ctx, fmt.Sprintf("namespace-%d", i), f, profile, profile.entities)
		}
	} else {
		// Hash codec: measure the catalog-scan + per-name membership join —
		// the path #541 just made deterministic. TODO(codex): mirror
		// listPredicatesByNamespace's Keys()+KeysByPrefix loop here.
	}
	// Owner filter (leading wildcard) — the shape under test. Expected match
	// set is exactly ONE key; the interesting number is scan cost.
	measureFiltered(t, kv, ctx, "owner", codec.ownerFilter(smokeEntityID(profile.entities/2)), profile, 1)

	// --- Phase 3: churn — list while writers mutate, then converge.
	var wg sync.WaitGroup
	churnCtx, stopChurn := context.WithCancel(ctx)
	for w := 0; w < profile.churnWriters; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; churnCtx.Err() == nil; i++ {
				id := smokeEntityID((w*10_000 + i) % profile.entities)
				key := codec.memberKey(hotPredicate, id)
				if i%2 == 0 {
					_ = kv.Delete(churnCtx, key)
				} else {
					_, _ = kv.Put(churnCtx, key, []byte{1})
				}
			}
		}(w)
	}
	measureFiltered(t, kv, ctx, "exact-under-churn", codec.exactFilter(hotPredicate), profile, -1 /* count varies */)
	stopChurn()
	wg.Wait()

	// Converge: restore truth, then the exact filter MUST return precisely
	// the expected set — zero false, missing, or stale keys.
	for _, key := range expected {
		_, err := kv.Put(ctx, key, []byte{1})
		require.NoError(t, err)
	}
	final, err := kv.KeysByFilter(ctx, codec.exactFilter(hotPredicate))
	require.NoError(t, err)
	sort.Strings(final)
	require.Equal(t, expected, final, "churn must converge to the seeded truth")

	// --- Phase 4: consumer + server hygiene.
	// TODO(codex): assert temporary filtered consumers on the bucket stream
	// returned to baseline (jsz/consumer list) — the leak class that a pure
	// latency benchmark never sees.
	reportServerDelta(t, codec.name, stats, scrapeServerStats(t))
}

func measureFiltered(t *testing.T, kv *natsclient.KVStore, ctx context.Context, label, filter string, profile smokeProfile, wantCount int) {
	durations := make([]time.Duration, 0, profile.reps)
	for r := 0; r < profile.reps; r++ {
		start := time.Now()
		keys, err := kv.KeysByFilter(ctx, filter)
		elapsed := time.Since(start)
		require.NoError(t, err, label)
		require.Less(t, elapsed, profile.perOpBudget, "%s rep %d blew the per-op budget", label, r)
		if wantCount >= 0 {
			require.Len(t, keys, wantCount, label)
		}
		durations = append(durations, elapsed)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[len(durations)*95/100]
	p99 := durations[len(durations)*99/100]
	require.LessOrEqual(t, p95, profile.p95Budget, "%s p95", label)
	require.LessOrEqual(t, p99, profile.p99Budget, "%s p99", label)
	t.Logf("%s: reps=%d p50=%s p95=%s p99=%s", label, profile.reps, durations[len(durations)/2], p95, p99)
}

// scrapeServerStats reads /varz from the test server's monitoring port.
// TODO(codex): wire once NewTestClient exposes -m; fields worth recording:
// cpu, mem, subscriptions, slow_consumers. Fallback: docker stats delta.
func scrapeServerStats(t *testing.T) map[string]any { return nil }

func reportServerDelta(t *testing.T, name string, before, after map[string]any) {
	// TODO(codex): log cpu/mem/subscriptions deltas for the ADR record.
}
```
