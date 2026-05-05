// Package component — bucket-name helpers and canonical bucket-name constants.
//
// Tag 4 of ADR-032 introduces per-org bucket suffixing. Every framework bucket
// today is named e.g. RULE_STATE; after the programme, each process running
// with Platform.Org=acme writes its state into RULE_STATE_acme. BucketName is
// the chokepoint helper that applies the suffix when org is non-empty, and the
// canonical-name constants below give every framework bucket a single source of
// truth so future renames touch one file, not twelve.
//
// This file is purely additive in chunk 2. Chunk 3 migrates the call sites to
// use BucketName + the constants. Chunk 6's flagship integration test fails red
// if any caller is missed.
package component

// BucketName returns the per-org KV bucket name. When org is empty, returns
// base unchanged — preserves the single-org default for products that haven't
// migrated yet. When org is non-empty, appends "_<org>" so e.g.
// RULE_STATE + "acme" → "RULE_STATE_acme".
//
// org is assumed validated by config.Validate (NATS-subject-safe lowercase
// alphanumeric + hyphen only); this function does not re-validate.
func BucketName(base, org string) string {
	if org == "" {
		return base
	}
	return base + "_" + org
}

// Canonical bucket-name constants. Single source of truth for every
// framework-managed JetStream KV bucket. Out-of-tree readers (sister products,
// ops dashboards) reference these by name; renaming any of them is a breaking
// change tracked in the migration doc.
//
// Bucket names are UPPERCASE_SNAKE_CASE (NATS convention) except where
// pre-existing buckets (semstreams_config) used lowercase — those stay
// lowercase to preserve the wire format.
const (
	// Rule processor
	RuleStateBucketName     = "RULE_STATE"
	RuleSchedulesBucketName = "RULE_SCHEDULES"
	RuleWindowsBucketName   = "RULE_WINDOWS"

	// Agentic loop
	AgentLoopsBucketName = "AGENT_LOOPS"

	// Lifecycle / status
	ComponentStatusBucketName = "COMPONENT_STATUS"

	// Governance
	GovernanceViolationsBucketName = "GOVERNANCE_VIOLATIONS"

	// SemStreams config (control plane: component configs, model registry)
	SemstreamsConfigBucketName = "semstreams_config"

	// Graph — primary entity storage
	EntityStatesBucketName = "ENTITY_STATES"

	// Graph — relationship indexes
	PredicateIndexBucketName = "PREDICATE_INDEX"
	IncomingIndexBucketName  = "INCOMING_INDEX"
	OutgoingIndexBucketName  = "OUTGOING_INDEX"

	// Graph — lookup indexes
	AliasIndexBucketName    = "ALIAS_INDEX"
	SpatialIndexBucketName  = "SPATIAL_INDEX"
	TemporalIndexBucketName = "TEMPORAL_INDEX"
	ContextIndexBucketName  = "CONTEXT_INDEX"

	// Graph — semantic / clustering tier
	EmbeddingsCacheBucketName = "EMBEDDINGS_CACHE"
	EmbeddingIndexBucketName  = "EMBEDDING_INDEX"
	EmbeddingDedupBucketName  = "EMBEDDING_DEDUP"
	CommunityIndexBucketName  = "COMMUNITY_INDEX"
	AnomalyIndexBucketName    = "ANOMALY_INDEX"

	// Graph — structural tier
	StructuralIndexBucketName = "STRUCTURAL_INDEX"

	// OASF / AGNTCY interop
	OASFRecordsBucketName = "OASF_RECORDS"
)
