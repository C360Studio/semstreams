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

	// Operational buckets
	// BucketToolCallOutcomes is agentic-tools' immutable COMPLETED ledger for
	// durable tool-result replay. Keys are opaque v1 hashes of ToolCall.ID.
	BucketToolCallOutcomes = "TOOL_CALL_OUTCOMES"

	// BucketGraphIngestAppliedSeq is graph-ingest's ADR-072 redelivery-guard
	// durable tier: `(entityID/streamName) → last-applied stream sequence`.
	// Created and owned exclusively by graph-ingest (processor/graph-ingest);
	// a member of FrameworkOwnedBuckets so a generic rule update_kv cannot forge
	// a sequence stamp and silently reopen the redelivery overwrite the guard
	// closes (framework-owned-bucket-guards F2, #715). It is correctness-critical
	// no-eviction state, so the retention sweep covers it.
	BucketGraphIngestAppliedSeq = "GRAPH_INGEST_APPLIED_SEQ"
	// BucketSemStreamsConfig is the shared runtime configuration bucket. It is in
	// the catalog because the framework guarantees BOTH its retention and its
	// write-ownership: since ADR-104 it holds the create-once platform identity
	// record, which must never be evicted and must never be forged.
	//
	// Two subsystems legitimately write it — config.Manager for the
	// configuration keys, processor/rule's ConfigManager for rules.* — and it is
	// still owner-only, because ownership is about who may NOT write. A generic
	// rule update_kv is refused for every key in the bucket, at load validation,
	// action runtime, and writer acquisition (owner ruling 2026-08-31, #1168
	// comment 5479005060; an earlier revision of this comment called it
	// catalogued for retention and not write-ownership, which the ruling
	// supersedes).
	BucketSemStreamsConfig = "semstreams_config"

	// BucketGraphStatus is the ADR-083 readiness distribution bucket. Producers
	// (graph-index, graph-embedding, graph-ingest, and rule) write their liveness
	// envelope; consumers watch it to answer "(status, fresh|unknown)". It is the
	// single source of truth for the bucket name — graph/readiness re-exports this constant. Its catalog
	// descriptor declares History 3 (readiness replay depth) and no lifecycle
	// retention; the acquisition seam and backstop strip only MaxAge/MaxBytes and
	// leave History untouched.
	BucketGraphStatus = "GRAPH_STATUS"

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
