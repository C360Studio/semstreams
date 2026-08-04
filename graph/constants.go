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

	// Semantic tier buckets
	BucketEmbeddingIndex = "EMBEDDING_INDEX"
	BucketEmbeddingDedup = "EMBEDDING_DEDUP"
	BucketCommunityIndex = "COMMUNITY_INDEX"
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
	// the bucket name — graph/readiness re-exports this constant. Its catalog
	// descriptor declares History 3 (readiness replay depth) and no lifecycle
	// retention; the acquisition seam and backstop strip only MaxAge/MaxBytes and
	// leave History untouched.
	BucketGraphStatus = "GRAPH_STATUS"

	// BucketOwnerClaims is the ADR-056 owner-claim registry — the single
	// `_registry` epoch key, written only through the ownership Registry. The
	// epoch write IS the registration audit trail (the KV-twofer), so its
	// catalog descriptor declares History 10 to answer "who registered what,
	// when" across recent deploys — and NO TTL (a TTL would age out the durable
	// epoch between deploys).
	BucketOwnerClaims = "OWNER_CLAIMS"
	// BucketOwnerPresence holds ADR-056 owner liveness heartbeats. Its catalog
	// descriptor declares bounded-ttl retention (ownership.PresenceTTL): the
	// TTL IS the liveness contract — a presence key not re-bumped within the
	// window ages out so a crashed owning lease frees — and the acquisition
	// seam converges to it rather than stripping it.
	BucketOwnerPresence = "OWNER_PRESENCE"

	// BucketStorageReport is the account storage report
	// (storage-observability): ONE KEY PER INVENTORIED RESOURCE, carrying that
	// resource's attribution, capacity, growth rate, projection, and pressure
	// state, written by the storage collector each collection. Every
	// operator-facing surface reads it rather than recomputing an inventory, so
	// there is one produced truth and no two surfaces can disagree.
	//
	// A resource that disappears from the account has its key DELETED by the
	// collector — a semantic decision, never an expiry — which is why its
	// catalog descriptor declares no-lifecycle retention like the rest of the
	// graph. Its bounded History is the restart-surviving growth series.
	BucketStorageReport = "STORAGE_REPORT"
)
