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
	// BucketPredicateCatalog maps a predicate's raw, unhashed name to a
	// presence marker. Its sole job is hash→name recovery for
	// PREDICATE_INDEX's hashed composite keys (ADR-065) — deliberately
	// unhashed itself, so KeysByPrefix on it is a safe, server-side
	// namespace query over predicate names (unlike PREDICATE_INDEX
	// membership keys, it carries no membership data to corrupt).
	BucketPredicateCatalog = "PREDICATE_CATALOG"
	BucketSpatialIndex     = "SPATIAL_INDEX"
	BucketTemporalIndex    = "TEMPORAL_INDEX"
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
	BucketAnomalyIndex    = "ANOMALY_INDEX"

	// Structural tier buckets
	BucketStructuralIndex = "STRUCTURAL_INDEX"

	// Operational buckets
	BucketComponentStatus = "COMPONENT_STATUS"
)

// FrameworkOwnedBuckets returns the authoritative and derived graph buckets
// whose writes are owned by graph components. Generic KV writers must not
// mutate these buckets.
func FrameworkOwnedBuckets() []string {
	return []string{
		BucketEntityStates,
		BucketPredicateIndex,
		BucketIncomingIndex,
		BucketOutgoingIndex,
		BucketAliasIndex,
		BucketNameIndex,
		BucketPredicateCatalog,
		BucketSpatialIndex,
		BucketTemporalIndex,
		BucketTemporalIndexReverse,
		BucketContextIndex,
		BucketEmbeddingsCache,
		BucketEmbeddingIndex,
		BucketEmbeddingDedup,
		BucketCommunityIndex,
		BucketAnomalyIndex,
		BucketStructuralIndex,
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
