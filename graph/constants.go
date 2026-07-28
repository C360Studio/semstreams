package graph

// Bucket name constants for NATS KV storage
const (
	// Primary entity storage
	BucketEntityStates = "ENTITY_STATES"

	// Graph relationship indexes
	BucketPredicateIndex = "PREDICATE_INDEX"
	BucketIncomingIndex  = "INCOMING_INDEX"
	BucketOutgoingIndex  = "OUTGOING_INDEX"

	// Lookup indexes
	BucketAliasIndex = "ALIAS_INDEX"
	// BucketNameIndex maps a normalized (case-folded) human-readable name/title
	// to the entities carrying it, for deterministic name→ranked-IDs lookup
	// (graph.query.byName, gh#376). Complements ALIAS_INDEX, which excludes
	// display-name (AliasTypeLabel) predicates.
	BucketNameIndex = "NAME_INDEX"
	// BucketEntitySuffixIndex maps an entity-ID suffix to the full 6-part ID(s)
	// carrying it, for partial-ID resolution. Created and owned exclusively by
	// graph-ingest (component.go); a member of FrameworkOwnedBuckets so a generic
	// rule update_kv cannot mutate it (closed by framework-owned-bucket-guards).
	BucketEntitySuffixIndex = "ENTITY_SUFFIX_INDEX"
	BucketSpatialIndex      = "SPATIAL_INDEX"
	BucketTemporalIndex     = "TEMPORAL_INDEX"
	// BucketTemporalIndexReverse maps entityID -> current temporal bucket key.
	// It lets graph-index-temporal remove an entity's stale event from its prior
	// time bucket when the entity is re-indexed (observed-time changed) or deleted,
	// so a range query never returns an entity from a bucket it has since left.
	BucketTemporalIndexReverse = "TEMPORAL_INDEX_REVERSE"
	BucketContextIndex         = "CONTEXT_INDEX"

	// Semantic tier buckets
	BucketEmbeddingsCache = "EMBEDDINGS_CACHE"
	BucketEmbeddingIndex  = "EMBEDDING_INDEX"
	BucketEmbeddingDedup  = "EMBEDDING_DEDUP"
	BucketCommunityIndex  = "COMMUNITY_INDEX"
	// BucketCommunitySummaries holds LLM-generated community summaries, written
	// ONLY by the graph-clustering enhancement worker and keyed by
	// {level}.{membership_hash} (content-addressed). It is deliberately SEPARATE
	// from COMMUNITY_INDEX: the detector owns the partition (COMMUNITY_INDEX),
	// the worker owns the LLM prose (this bucket), so a lagging worker can never
	// clobber a fresher partition or resurrect a Prune-deleted community (ADR-087).
	BucketCommunitySummaries = "COMMUNITY_SUMMARIES"
	BucketAnomalyIndex       = "ANOMALY_INDEX"

	// Structural tier buckets
	BucketStructuralIndex = "STRUCTURAL_INDEX"

	// Operational buckets
	BucketComponentStatus = "COMPONENT_STATUS"
	// BucketGraphIngestAppliedSeq is graph-ingest's ADR-072 redelivery-guard
	// durable tier: `(entityID/streamName) → last-applied stream sequence`.
	// Created and owned exclusively by graph-ingest (processor/graph-ingest);
	// a member of FrameworkOwnedBuckets so a generic rule update_kv cannot forge
	// a sequence stamp and silently reopen the redelivery overwrite the guard
	// closes (framework-owned-bucket-guards F2, #715). It is correctness-critical
	// no-eviction state, so the retention sweep covers it.
	BucketGraphIngestAppliedSeq = "GRAPH_INGEST_APPLIED_SEQ"
	// BucketGraphStatus is the ADR-083 readiness distribution bucket. Producers
	// (graph-index/graph-embedding) write their liveness envelope; consumers watch
	// it to answer "(status, fresh|unknown)". It is the single source of truth for
	// the bucket name — graph/readiness re-exports this constant. A member of
	// FrameworkOwnedBuckets so a generic rule update_kv cannot forge readiness
	// (framework-owned-bucket-guards F3). The retention sweep covers it (it is
	// created clean with History=BucketHistory and no TTL, so a no-op in steady
	// state); the sweep strips only MaxAge/MaxBytes and leaves History untouched.
	BucketGraphStatus = "GRAPH_STATUS"
)

// FrameworkOwnedBuckets returns the authoritative, derived, and framework
// operational graph buckets whose writes are owned by graph components. Generic
// KV writers (e.g. a rule update_kv) must not mutate these buckets. The set
// includes two operational buckets alongside ENTITY_STATES and its derived
// indexes: GRAPH_INGEST_APPLIED_SEQ (redelivery-guard stamps, #715) and
// GRAPH_STATUS (readiness envelopes) — forging either subverts a framework
// invariant, so both are write-protected.
func FrameworkOwnedBuckets() []string {
	return []string{
		BucketEntityStates,
		BucketPredicateIndex,
		BucketIncomingIndex,
		BucketOutgoingIndex,
		BucketAliasIndex,
		BucketNameIndex,
		BucketEntitySuffixIndex,
		BucketSpatialIndex,
		BucketTemporalIndex,
		BucketTemporalIndexReverse,
		BucketContextIndex,
		BucketEmbeddingsCache,
		BucketEmbeddingIndex,
		BucketEmbeddingDedup,
		BucketCommunityIndex,
		BucketCommunitySummaries,
		BucketAnomalyIndex,
		BucketStructuralIndex,
		BucketGraphIngestAppliedSeq,
		BucketGraphStatus,
	}
}

// IsFrameworkOwnedBucket reports whether a bucket's writes belong exclusively
// to an authoritative or derived graph component.
func IsFrameworkOwnedBucket(bucket string) bool {
	for _, owned := range FrameworkOwnedBuckets() {
		if bucket == owned {
			return true
		}
	}
	return false
}
