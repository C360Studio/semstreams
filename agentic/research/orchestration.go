package research

import (
	"strconv"
	"time"

	"github.com/c360studio/semstreams/message"
)

// Triple-builder helpers for the rule chain's per-stage completion
// stamps. Each component (nl_classify, route_search, execute_subqueries,
// assess_sufficiency, synthesize_answer) calls the matching Build*Triples
// function after its main work succeeds, then publishes the result via
// the shared TriplePublisher surface (agentictools.TriplePublisher) so
// graph-ingest atomically lands the batch on the research-pipeline
// loop entity. R0-R6 watch ENTITY_STATES and trigger on the false→true
// transition of these triples' presence.
//
// All builders return atomic batches sharing one Subject so graph-ingest's
// per-Subject CAS path preserves the rule-matching contract — a rule that
// branches on action+complete must never see a fresh completion paired
// with a stale action.
//
// Source convention matches the per-component package name with a
// graph-writer suffix (`research-graph-classify`, etc.) so operator
// dashboards can attribute provenance per stage.

const (
	// SourceClassify is the Triple.Source for nl_classify orchestration stamps.
	SourceClassify = "research-graph-classify"
	// SourceRoute is the Triple.Source for route_search orchestration stamps.
	SourceRoute = "research-graph-route"
	// SourceExecute is the Triple.Source for execute_subqueries orchestration stamps.
	SourceExecute = "research-graph-execute"
	// SourceAssess is the Triple.Source for assess_sufficiency orchestration stamps.
	SourceAssess = "research-graph-assess"
	// SourceSynthesize is the Triple.Source for synthesize_answer orchestration stamps.
	SourceSynthesize = "research-graph-synthesize"
)

// formatStamp encodes a completion timestamp as RFC3339Nano so the
// triple round-trip preserves sub-second ordering across the chain.
func formatStamp(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339Nano)
}

// formatBool encodes a Go bool as the canonical "true"/"false" string
// triples carry. Rules match on these exact strings via the eq
// operator.
func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// BuildClassifyCompleteTriples returns the orchestration triples
// nl_classify stamps after its candidate set is durably written to
// AGENT_LOOPS. Three triples share the loop entity Subject and land
// atomically. R1 fires on PredicateResearchClassifyComplete's
// false→true transition.
func BuildClassifyCompleteTriples(loopEntityID string, candidateCount int, degraded bool, ts time.Time) []message.Triple {
	stamp := formatStamp(ts)
	return []message.Triple{
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchClassifyComplete,
			Object:     stamp,
			Source:     SourceClassify,
			Timestamp:  ts,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchClassifyCandidateCount,
			Object:     strconv.Itoa(candidateCount),
			Source:     SourceClassify,
			Timestamp:  ts,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchClassifyDegraded,
			Object:     formatBool(degraded),
			Source:     SourceClassify,
			Timestamp:  ts,
			Confidence: 1.0,
		},
	}
}

// BuildRouteCompleteTriples returns the orchestration triples
// route_search stamps after its RouteDecision envelope is durably
// written to AGENT_LOOPS. Two triples share the loop entity Subject
// and land atomically — R2's action-level when clauses match on
// PredicateResearchRouteAction so a partial write (action without
// complete, or complete without action) would corrupt the dispatch.
// Action must be one of the four research.Action* constants; callers
// pass the validated RouteDecision.Action verbatim.
func BuildRouteCompleteTriples(loopEntityID, action string, ts time.Time) []message.Triple {
	stamp := formatStamp(ts)
	return []message.Triple{
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchRouteComplete,
			Object:     stamp,
			Source:     SourceRoute,
			Timestamp:  ts,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchRouteAction,
			Object:     action,
			Source:     SourceRoute,
			Timestamp:  ts,
			Confidence: 1.0,
		},
	}
}

// BuildExecuteCompleteTriples returns the orchestration triples
// execute_subqueries stamps after its evidence array is durably
// written to AGENT_LOOPS. R3 fires on PredicateResearchExecuteComplete's
// false→true transition. evidenceCount lets ops dashboards spot
// zero-evidence executes (likely router misclassification) without
// re-fetching the envelope.
func BuildExecuteCompleteTriples(loopEntityID string, evidenceCount int, ts time.Time) []message.Triple {
	stamp := formatStamp(ts)
	return []message.Triple{
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchExecuteComplete,
			Object:     stamp,
			Source:     SourceExecute,
			Timestamp:  ts,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchExecuteEvidenceCount,
			Object:     strconv.Itoa(evidenceCount),
			Source:     SourceExecute,
			Timestamp:  ts,
			Confidence: 1.0,
		},
	}
}

// BuildAssessCompleteTriples returns the orchestration triples
// assess_sufficiency stamps after its AssessmentOutput envelope is
// durably written to AGENT_LOOPS. R4's action-level when clauses
// match on PredicateResearchAssessSufficient.
func BuildAssessCompleteTriples(loopEntityID string, sufficient bool, ts time.Time) []message.Triple {
	stamp := formatStamp(ts)
	return []message.Triple{
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchAssessComplete,
			Object:     stamp,
			Source:     SourceAssess,
			Timestamp:  ts,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchAssessSufficient,
			Object:     formatBool(sufficient),
			Source:     SourceAssess,
			Timestamp:  ts,
			Confidence: 1.0,
		},
	}
}

// SourceResearchGraphTool is the Triple.Source for the kickoff triples
// stamped by the research_graph agent tool when a chain spawns.
// Distinct from the per-component Source values so operator dashboards
// can attribute the chain entry point separately.
const SourceResearchGraphTool = "agent-research-graph"

// BuildKickoffTriples returns the triple batch the research_graph tool
// stamps on the research-pipeline loop entity at chain spawn time. R0
// fires on PredicateResearchRequested's false→true transition (and
// loop.role gates which loops the rule scopes to). Triples are:
//
//   - loop.role = "research_pipeline" (gates R0 scope to this chain)
//   - research.request.received = "true" (the trigger marker)
//   - research.request.topic = <topic>
//   - research.loop.id = <loopID> (rule substitution into NATS subjects)
//   - research.parent.loop = <parentLoopID> (continuation lineage)
//   - research.parent.role = <parentRole> (R6 publish_agent role)
//   - research.request.budget_tokens = <int>
//   - research.request.max_iterations = <int>
//
// All triples share the loop entity Subject so the batch lands
// atomically — R0 must not see a half-populated chain identity.
//
// parentRole may be empty when the research_graph tool is invoked
// outside an agent loop; R6 then falls back to a configured default.
func BuildKickoffTriples(loopEntityID, topic, loopID, parentLoopID, parentRole string, budgetTokens, maxIterations int, ts time.Time) []message.Triple {
	t := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    loopEntityID,
			Predicate:  predicate,
			Object:     object,
			Source:     SourceResearchGraphTool,
			Timestamp:  ts,
			Confidence: 1.0,
		}
	}
	triples := []message.Triple{
		t(PredicateLoopRole, PipelineRole),
		t(PredicateResearchRequested, "true"),
		t(PredicateResearchTopic, topic),
		t(PredicateResearchLoopID, loopID),
		t(PredicateResearchBudgetTokens, strconv.Itoa(budgetTokens)),
		t(PredicateResearchMaxIterations, strconv.Itoa(maxIterations)),
	}
	if parentLoopID != "" {
		triples = append(triples, t(PredicateResearchParentLoop, parentLoopID))
	}
	if parentRole != "" {
		triples = append(triples, t(PredicateResearchParentRole, parentRole))
	}
	return triples
}

// BuildSearchResultCompleteTriples returns the orchestration triples
// synthesize_answer stamps after its SearchResult envelope is durably
// written to AGENT_LOOPS at the search_result.complete.<loopID> key.
// R6 (continuation) fires on PredicateResearchSearchResultComplete and
// publishes back to the parent loop with resultRef as the payload_ref
// (per ADR-028: rules carry references, not content). resultRef is
// the AGENT_LOOPS key (e.g., "search_result.complete.<loopID>").
func BuildSearchResultCompleteTriples(loopEntityID, resultRef string, ts time.Time) []message.Triple {
	stamp := formatStamp(ts)
	return []message.Triple{
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchSearchResultComplete,
			Object:     stamp,
			Source:     SourceSynthesize,
			Timestamp:  ts,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  PredicateResearchSearchResultRef,
			Object:     resultRef,
			Source:     SourceSynthesize,
			Timestamp:  ts,
			Confidence: 1.0,
		},
	}
}
