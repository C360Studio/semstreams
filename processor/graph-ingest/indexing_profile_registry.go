package graphingest

import (
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// indexingProfileDefaults is the ADR-054 Phase 2 framework-registry seed: it
// maps a message type (message.Type.Key() == "domain.category.version") to the
// indexing profile graph-ingest stamps when a producer of that type declares
// none (channel c, the fallback floor). A type that is absent here falls
// through to control (fail-safe toward keeping the substrate reachable) and the
// indexing_profile_default_total{message_type} metric fires — that metric now
// means "a registry GAP", the operator signal ADR-054 §5 calls for.
//
// Deliberately keyed by STRING, not by importing the agentic/research/
// operating-model domain packages into the generic graph-ingest layer (which
// imports none of them today). A key that drifts — e.g. a future schema-version
// bump — simply degrades to the control default and is surfaced by the metric:
// graceful, self-revealing, never a hard failure. The keys were verified
// against the domain constants (agentic.SchemaVersion / research.SchemaVersion /
// operating_model.SchemaVersion are all "v1").
//
// These values are LENIENT in Phase 1/2 — no consumer acts on the profile yet.
// They shape the metric/dry-run data and the Phase-3 strict-exclusion policy.
// "content" entries are NOT exhaustive (most content is producer-declared via
// the envelope/IndexingProfiler channels); this seed is about classifying the
// high-cardinality trace storm OUT and the obvious low-cardinality machinery as
// control, so Phase 3 has sane defaults for the undeclared tail.
var indexingProfileDefaults = map[string]string{
	// content — prose-bearing retrieval corpus (embed yes, community yes).
	"agentic.user_message.v1":            vocabulary.IndexingProfileContent,
	"agentic.user_response.v1":           vocabulary.IndexingProfileContent,
	"research.result.v1":                 vocabulary.IndexingProfileContent,
	"operating_model.layer_approved.v1":  vocabulary.IndexingProfileContent,
	"operating_model.profile_context.v1": vocabulary.IndexingProfileContent,

	// control — low-cardinality lifecycle/harness/run machinery (embed yes).
	// (control is also the unlisted default; listing these documents intent and
	// distinguishes "deliberately control" from "unclassified gap" in review.)
	"agentic.task.v1":              vocabulary.IndexingProfileControl,
	"agentic.loop_created.v1":      vocabulary.IndexingProfileControl,
	"agentic.loop_completed.v1":    vocabulary.IndexingProfileControl,
	"agentic.loop_failed.v1":       vocabulary.IndexingProfileControl,
	"agentic.loop_cancelled.v1":    vocabulary.IndexingProfileControl,
	"agentic.approval_pending.v1":  vocabulary.IndexingProfileControl,
	"agentic.approval_response.v1": vocabulary.IndexingProfileControl,
	"research.intent.v1":           vocabulary.IndexingProfileControl,

	// signal — control/telemetry signals (embed only summarized rollups).
	"agentic.signal.v1":         vocabulary.IndexingProfileSignal,
	"agentic.signal_message.v1": vocabulary.IndexingProfileSignal,

	// trace — high-cardinality, mechanically generated execution detail (the
	// storm; embed no, community no). This is the load-bearing classification:
	// these default OUT of embedding once Phase 3 flips, without each producer
	// needing to declare.
	"agentic.request.v1":            vocabulary.IndexingProfileTrace,
	"agentic.response.v1":           vocabulary.IndexingProfileTrace,
	"agentic.tool_call.v1":          vocabulary.IndexingProfileTrace,
	"agentic.tool_result.v1":        vocabulary.IndexingProfileTrace,
	"agentic.context_event.v1":      vocabulary.IndexingProfileTrace,
	"research.route_decision.v1":    vocabulary.IndexingProfileTrace,
	"research.classifier_output.v1": vocabulary.IndexingProfileTrace,
	"research.execution_output.v1":  vocabulary.IndexingProfileTrace,
	"research.assessment_output.v1": vocabulary.IndexingProfileTrace,
}

// indexingProfileFloorFor returns the registry default profile for a message
// type and whether the type was found. A miss returns (control, false) — the
// fail-safe floor plus the signal that this type is an unclassified registry
// gap the default-fallback metric should surface (ADR-054 §5).
func indexingProfileFloorFor(mt message.Type) (profile string, registered bool) {
	if p, ok := indexingProfileDefaults[mt.Key()]; ok {
		return p, true
	}
	return vocabulary.IndexingProfileControl, false
}
