// kvcatalog.go is the POPULATION half of the framework KV bucket catalog
// (framework-bucket-catalog): the ONE literal table of every KV bucket whose
// write-ownership or retention the framework guarantees. The mechanism half —
// the BucketSpec type and the Ensure/Open acquisition seam — lives in
// natsclient/kvspec.go, name-free.
//
// Boundary rule (so the catalog cannot grow by accretion): a bucket belongs
// here ONLY if the framework guarantees its write-ownership or retention.
// Application/product buckets (AGENT_LOOPS, flowstore, personas, governance
// stores, workflow-type Lifecycle buckets, research-graph state, ...) are
// outside it by rule and keep plain CreateKeyValueBucket.
//
// Every list the framework enforces from is a DERIVED view of this table:
// FrameworkOwnedBuckets() (the rule update_kv write guard) filters
// Write == WriteOwnerOnly; the pre-start legacy-drift backstop
// (AssertOwnedBucketsClean) filters Retention.Kind == RetentionNoLifecycle.
// There is no parallel hand-maintained list to forget a member from — the
// contract test (test/contract) mechanically rejects any non-test file outside
// this package that names a catalog bucket in a string literal.
//
// Owner enforcement is call-site selection: owners call
// natsclient.EnsureFrameworkBucket with their descriptor, readers call
// natsclient.OpenFrameworkBucket. The framework does not verify caller
// identity at runtime; that boundary is review-enforced, deliberately.

package graph

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// ownerPresenceTTL is OWNER_PRESENCE's declared bounded TTL — the ADR-056
// liveness staleness floor (ttl_hint >= 3 x max(boot_time, gc_pause_budget)).
// pkg/ownership.PresenceTTL carries the same value next to the heartbeat
// interval derived from it; a cross-pin test in pkg/ownership asserts the two
// can never drift (graph cannot import pkg/ownership — ownership acquires its
// buckets through this catalog, so the dependency points the other way).
const ownerPresenceTTL = 120 * time.Second

// KVCatalog returns the full descriptor table. The slice is rebuilt per call
// so no caller can mutate the catalog for another.
func KVCatalog() []natsclient.BucketSpec {
	noLifecycle := natsclient.RetentionPolicy{Kind: natsclient.RetentionNoLifecycle}
	owned := func(name, owner, description string, class natsclient.BucketClass) natsclient.BucketSpec {
		return natsclient.BucketSpec{
			Name:        name,
			Owner:       owner,
			Description: description,
			Class:       class,
			Retention:   noLifecycle,
			Write:       natsclient.WriteOwnerOnly,
			Posture:     natsclient.PostureOwnerCreates,
			History:     1,
			Replicas:    1,
		}
	}
	derived := func(name, owner, description string) natsclient.BucketSpec {
		return owned(name, owner, description, natsclient.ClassDerived)
	}

	entityStates := owned(BucketEntityStates, "graph-ingest",
		"Entity state storage for graph-ingest", natsclient.ClassAuthoritative)
	// Owner decision 2026-07-28: History 1. Nothing reads deeper ENTITY_STATES
	// history (the only History() consumer is Lifecycle's workflow buckets);
	// deploys where the retired tool-registration create had won the race with
	// History 3 are reconciled down at acquisition — destructive-but-unread,
	// and the seam WARNs naming both values.
	entityStates.History = 1

	graphStatus := owned(BucketGraphStatus, "graph-index/graph-embedding (readiness producers)",
		"ADR-083 readiness envelopes, one key per producer (operational component status, not graph data)",
		natsclient.ClassOperational)
	// Readiness replay depth: enough replay for a late-binding consumer to see
	// recent envelopes (was readiness.BucketHistory before the catalog owned it).
	graphStatus.History = 3

	storageReport := owned(BucketStorageReport, "storage inventory collector",
		"Account storage report, one key per inventoried resource (capacity, growth, pressure)",
		natsclient.ClassOperational)
	// History 10, and the depth is a CONTRACT rather than a log preference: the
	// per-key history IS the growth series. The rate is Δbytes over Δt across
	// successive published observations, and this is where those observations
	// live — restart-surviving by construction, with no separate sample store.
	//
	// Why 10 and not the bare floor of 2
	// (natsclient.StorageReportGrowthObservations, cross-pinned by a test in
	// this package): depth 2 covers only the benign restart. It does NOT cover
	// the cases the design says the projection matters most in. A crash loop or
	// deploy loop restarting faster than natsclient.MinGrowthSampleInterval
	// publishes rows too close together to measure against, and several
	// processes polling account-wide interleave the same way; the derivation
	// then has to reach back PAST those rows for an observation separated by a
	// usable interval. At depth 1 or 2 there is nothing to reach back to and
	// every rate in the account reports unknown for as long as the loop lasts.
	// Ten rows also give an operator a recent trend to eyeball
	// (`nats kv history`), which is why OWNER_CLAIMS carries the same depth for
	// its own audit trail.
	//
	// The cost is bounded and self-referential: this bucket appears in the
	// inventory it publishes, so the depth is also what keeps the observer from
	// becoming the growth problem it exists to detect — ten small rows per
	// account resource, and no retention needed to hold that line.
	storageReport.History = 10

	ownerClaims := owned(BucketOwnerClaims, "ownership registry (composition root)",
		"ADR-056 owner-claim registry — single _registry epoch key (no TTL)",
		natsclient.ClassOperational)
	// The epoch write IS the registration audit trail; History answers "who
	// registered what, when" across recent deploys (was ownership's
	// ownerClaimsHistory before the catalog owned it).
	ownerClaims.History = 10

	ownerPresence := natsclient.BucketSpec{
		Name:        BucketOwnerPresence,
		Owner:       "ownership registry (composition root)",
		Description: "ADR-056 owner liveness heartbeats — bucket-TTL staleness backstop",
		Class:       natsclient.ClassOperational,
		// The TTL is the liveness contract: converged to, never stripped —
		// the live proof that retention is a per-descriptor policy, not a
		// "framework-owned means no retention" global rule.
		Retention: natsclient.RetentionPolicy{Kind: natsclient.RetentionBoundedTTL, TTL: ownerPresenceTTL},
		Write:     natsclient.WriteOwnerOnly,
		Posture:   natsclient.PostureOwnerCreates,
		History:   1,
		Replicas:  1,
	}

	componentStatus := natsclient.BucketSpec{
		Name:        BucketComponentStatus,
		Owner:       "every lifecycle-reporting component (shared diagnostic)",
		Description: "Component lifecycle status tracking",
		// #717 answered by evidence: 24 production writers, ZERO production
		// readers (only the e2e harness consumes it) — write-protecting it
		// would guard state nothing reads, and the framework guarantees no
		// retention posture for it. Both are explicit catalog facts, not
		// omissions; a future ops TTL is a one-line edit of this row.
		Class:     natsclient.ClassDiagnostic,
		Retention: natsclient.RetentionPolicy{Kind: natsclient.RetentionUnmanaged},
		Write:     natsclient.WriteOpen,
		Posture:   natsclient.PostureOwnerCreates,
		History:   1,
		Replicas:  1,
	}

	return []natsclient.BucketSpec{
		// Authoritative domain state + graph-ingest's operational tiers.
		entityStates,
		derived(BucketEntitySuffixIndex, "graph-ingest",
			"Suffix-to-full-ID reverse index for partial entity ID resolution"),
		owned(BucketGraphIngestAppliedSeq, "graph-ingest",
			"graph-ingest redelivery guard: (entityID/streamName) -> last-applied stream sequence (ADR-072)",
			natsclient.ClassOperational),

		// graph-index's four config-declared output indexes + the name lookup internal.
		derived(BucketOutgoingIndex, "graph-index", "Graph index bucket: outgoing relationships"),
		derived(BucketIncomingIndex, "graph-index", "Graph index bucket: incoming relationships"),
		derived(BucketAliasIndex, "graph-index", "Graph index bucket: alias lookup"),
		derived(BucketPredicateIndex, "graph-index", "Graph index bucket: predicate lookup"),
		derived(BucketNameIndex, "graph-index",
			"Name/title → entities index for deterministic name lookup"),

		// Spatial / temporal tier.
		derived(BucketSpatialIndex, "graph-index-spatial", "Spatial index for geospatial queries"),
		derived(BucketTemporalIndex, "graph-index-temporal", "Temporal index for time-based queries"),
		derived(BucketTemporalIndexReverse, "graph-index-temporal",
			"Temporal index reverse map (entity -> current time bucket) for stale-entry cleanup"),

		// Semantic tier.
		derived(BucketEmbeddingIndex, "graph-embedding", "Entity embedding index"),
		derived(BucketEmbeddingDedup, "graph-embedding", "Entity embedding deduplication"),
		derived(BucketCommunityIndex, "graph-clustering", "Community detection index"),
		derived(BucketCommunitySummaries, "graph-clustering",
			"LLM community summaries (content-addressed by membership hash, ADR-087)"),
		derived(BucketAnomalyIndex, "graph-clustering",
			"Anomaly detection index for structural gaps and inferences"),

		// Framework operational plane.
		graphStatus,
		ownerClaims,
		ownerPresence,
		storageReport,

		// Diagnostic plane.
		componentStatus,
	}
}

// SpecFor resolves a bucket name to its catalog descriptor. The second return
// is false for a name outside the catalog — the F2 boot-failure signal for a
// configuration-supplied bucket name that resolves to nothing (an operator
// typo may not silently create a stray unguarded bucket).
func SpecFor(name string) (natsclient.BucketSpec, bool) {
	for _, spec := range KVCatalog() {
		if spec.Name == name {
			return spec, true
		}
	}
	return natsclient.BucketSpec{}, false
}

// FrameworkOwnedBuckets returns the buckets whose writes are owned exclusively
// by a framework component — the DERIVED write-policy view of the catalog
// (Write == WriteOwnerOnly), which the rule update_kv guard enforces at load
// and at runtime. There is no hand-maintained list behind this: a catalog row
// declared owner-only appears here with nothing to forget.
func FrameworkOwnedBuckets() []string {
	return frameworkOwnedFrom(KVCatalog())
}

// frameworkOwnedFrom derives the owned set from a descriptor slice. Split out
// (and driven with a fixture in tests) so derivation-not-snapshot is provable.
func frameworkOwnedFrom(specs []natsclient.BucketSpec) []string {
	owned := make([]string, 0, len(specs))
	for _, spec := range specs {
		if spec.Write == natsclient.WriteOwnerOnly {
			owned = append(owned, spec.Name)
		}
	}
	return owned
}

// IsFrameworkOwnedBucket reports whether a bucket's catalog descriptor
// declares owner-only writes, so a generic KV writer (rule update_kv) must
// not mutate it.
func IsFrameworkOwnedBucket(bucket string) bool {
	spec, ok := SpecFor(bucket)
	return ok && spec.Write == natsclient.WriteOwnerOnly
}

// OwnerOf returns the catalog Owner string for a bucket ("" when the bucket is
// not in the catalog), for rejection messages that should tell the operator
// who legitimately writes it.
func OwnerOf(bucket string) string {
	spec, ok := SpecFor(bucket)
	if !ok {
		return ""
	}
	return spec.Owner
}

// EnsureCatalogBucket resolves a catalog bucket by name and acquires it
// through the OWNER seam (natsclient.EnsureFrameworkBucket): create-or-open,
// reconcile to the declared policy, verify, or fail the caller's Start closed.
// Only a bucket's declared owner calls this; readers use OpenCatalogBucket.
// A name outside the catalog is an invalid-config error naming it.
func EnsureCatalogBucket(ctx context.Context, client *natsclient.Client, name string) (jetstream.KeyValue, error) {
	spec, ok := SpecFor(name)
	if !ok {
		return nil, errs.WrapInvalid(
			fmt.Errorf("bucket %q is not in the framework KV catalog", name),
			"graph", "EnsureCatalogBucket", "resolve framework bucket")
	}
	return natsclient.EnsureFrameworkBucket(ctx, client, spec)
}

// OpenCatalogBucket resolves a catalog bucket by name and binds it must-exist
// through the READER seam (natsclient.OpenFrameworkBucket): it NEVER creates
// and never reconciles; an absent bucket yields a classified not-ready error
// naming the catalog owner. A name outside the catalog is an invalid-config
// error naming it.
func OpenCatalogBucket(ctx context.Context, client *natsclient.Client, name string) (jetstream.KeyValue, error) {
	spec, ok := SpecFor(name)
	if !ok {
		return nil, errs.WrapInvalid(
			fmt.Errorf("bucket %q is not in the framework KV catalog", name),
			"graph", "OpenCatalogBucket", "resolve framework bucket")
	}
	return natsclient.OpenFrameworkBucket(ctx, client, spec)
}
