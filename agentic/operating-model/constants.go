package operatingmodel

// Domain and version constants for the operating-model message namespace.
const (
	// Domain is the payload registry domain for all operating-model messages.
	Domain = "operating_model"

	// SchemaVersion is the current schema version for operating-model payloads.
	SchemaVersion = "v1"
)

// Category constants for operating-model message types.
const (
	// CategoryLayerApproved is the payload category for an approved-layer checkpoint.
	CategoryLayerApproved = "layer_approved"

	// CategoryProfileContext is the payload category for assembled profile context
	// injected into the agentic loop's system prompt.
	CategoryProfileContext = "profile_context"
)

// Layer names, in interview order.
const (
	// LayerOperatingRhythms captures recurring cadence and calendar structure.
	LayerOperatingRhythms = "operating_rhythms"

	// LayerRecurringDecisions captures decisions made on a regular schedule.
	LayerRecurringDecisions = "recurring_decisions"

	// LayerDependencies captures people, systems, and inputs the work relies on.
	LayerDependencies = "dependencies"

	// LayerInstitutionalKnowledge captures tribal knowledge and hard-won context.
	LayerInstitutionalKnowledge = "institutional_knowledge"

	// LayerFriction captures where the work gets stuck today.
	LayerFriction = "friction"
)

// Layers returns the canonical interview order. The returned slice is a copy
// so callers can safely mutate it without affecting package state.
func Layers() []string {
	return []string{
		LayerOperatingRhythms,
		LayerRecurringDecisions,
		LayerDependencies,
		LayerInstitutionalKnowledge,
		LayerFriction,
	}
}

// IsValidLayer reports whether name is one of the five canonical layer names.
func IsValidLayer(name string) bool {
	switch name {
	case LayerOperatingRhythms,
		LayerRecurringDecisions,
		LayerDependencies,
		LayerInstitutionalKnowledge,
		LayerFriction:
		return true
	}
	return false
}

// Source-confidence values for operating-model entries.
const (
	// ConfidenceConfirmed marks an entry the user explicitly approved.
	ConfidenceConfirmed = "confirmed"

	// ConfidenceSynthesized marks an entry inferred from other entries or
	// from a contradiction pass.
	ConfidenceSynthesized = "synthesized"
)

// Entry-status values.
const (
	// StatusActive marks a currently-true entry.
	StatusActive = "active"

	// StatusUnresolved marks an entry flagged for follow-up.
	StatusUnresolved = "unresolved"

	// StatusSuperseded marks an entry replaced by a later profile version.
	StatusSuperseded = "superseded"
)

// Predicate constants for operating-model triples. All predicates live under
// the om.* or user.operating_model.* namespace so they can be queried cleanly
// without conflicting with other teams-memory categories.
const (
	// Profile-level predicates
	PredicateProfileVersion     = "user.operating_model.version"
	PredicateProfileLastUpdated = "user.operating_model.last_updated"
	PredicateProfileHasLayer    = "user.operating_model.has_layer"

	// Layer-level predicates
	PredicateLayerName              = "om.layer.name"
	PredicateLayerCheckpointSummary = "om.layer.checkpoint_summary"
	PredicateLayerVersion           = "om.layer.version"
	PredicateLayerHasEntry          = "om.layer.has_entry"

	// Entry-level predicates
	PredicateEntryTitle            = "om.entry.title"
	PredicateEntrySummary          = "om.entry.summary"
	PredicateEntryCadence          = "om.entry.cadence"
	PredicateEntryTrigger          = "om.entry.trigger"
	PredicateEntryInputs           = "om.entry.inputs"
	PredicateEntryStakeholders     = "om.entry.stakeholders"
	PredicateEntryConstraints      = "om.entry.constraints"
	PredicateEntrySourceConfidence = "om.entry.source_confidence"
	PredicateEntryStatus           = "om.entry.status"
)

// TripleSource identifies operating-model triples in the Source field.
// This matches the convention from semstreams' agentic graph_writer where
// the source names the producing component.
const TripleSource = "operating_model"
