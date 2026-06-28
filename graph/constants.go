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
	BucketAliasIndex    = "ALIAS_INDEX"
	BucketSpatialIndex  = "SPATIAL_INDEX"
	BucketTemporalIndex = "TEMPORAL_INDEX"
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
