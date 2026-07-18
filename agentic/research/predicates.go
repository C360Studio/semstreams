package research

import "github.com/c360studio/semstreams/vocabulary"

// Predicate constants for triples emitted by the research_graph chain.
// Every predicate lives under the research.* namespace so graph
// queries can filter chain triples cleanly without colliding with
// other agentic systems.
//
// Triple emission discipline (ADR-028):
//   - Bulky payloads (synthesis text, evidence body) live in
//     ObjectStore via ContentStorable; triples carry the ref.
//   - LLM-authored predicates (eventual route_rationale and
//     synthesis predicates introduced in PR 3 and PR 5) MUST default
//     to WithRuleOpaque(true) per
//     feedback_llm_authored_predicates_rule_opaque — rules branch on
//     typed fields, not free-form text. PR 1 reserves the namespace
//     here; the named constants land with the PRs that emit them so
//     the contract is reviewable alongside the emission.
const (
	// PredicateResearchRequested marks a research-pipeline loop entity
	// as the target of a research_graph tool invocation. Triple
	// subject = research-loop entity ID; object = the topic string.
	// R0 of the rule chain watches for this triple to kick off the
	// chain.
	PredicateResearchRequested = "research.request.received"

	// PredicateResearchTopic carries the topic verbatim. Distinct from
	// PredicateResearchRequested because the latter doubles as the
	// chain-kickoff trigger; this predicate is the durable record of
	// what the parent actually asked for.
	PredicateResearchTopic = "research.request.topic"

	// PredicateResearchHint stamps a single hint key/value pair. One
	// triple per hint, with the key encoded in the Object field as
	// "<key>=<value>". Multi-triple is preferred over JSON-stringified
	// hint maps so graph queries can filter on specific hint keys.
	PredicateResearchHint = "research.request.hint"

	// PredicateResearchBudgetTokens carries the resolved per-call token
	// budget (after defaulting). Stored as string per Triple.Object
	// shape; consumers parse to int.
	PredicateResearchBudgetTokens = "research.request.budget-tokens"

	// PredicateResearchMaxIterations carries the resolved refine-loop
	// cap (after defaulting).
	PredicateResearchMaxIterations = "research.request.max-iterations"

	// PredicateResearchParentLoop links the research-pipeline loop
	// back to its parent loop entity ID. Stamped by the research_graph
	// tool from call.LoopID so the continuation rule can route the
	// SearchResult back to the right caller.
	PredicateResearchParentLoop = "research.parent.loop"

	// PredicateLoopRole stamps the loop entity's role. Same convention
	// as other agentic loops; lets ops dashboards filter
	// research-pipeline loops without parsing intent payloads.
	PredicateLoopRole = "agent.loop.role"

	// PredicateResearchLoopID stamps the raw research-pipeline loop ID
	// (e.g. "rg_abc12345") on the loop entity as a dedicated triple
	// so R0-R6 rules can substitute it into NATS publish subjects via
	// `$entity.triple.research.loop.id`. The 6-part loop-execution
	// entity ID has the loop ID as its last segment, but the
	// substitution layer doesn't expose a "last-segment" accessor and
	// the rules need the raw form for routing. Cheap one-extra-triple
	// per chain instead of an engine feature.
	PredicateResearchLoopID = "research.loop.id"

	// PredicateResearchParentRole stamps the parent loop's role so the
	// continuation rule (R6) can route the SearchResult back to the
	// correct caller without re-fetching the parent LoopEntity from
	// AGENT_LOOPS. Empty when the research_graph tool was invoked from
	// outside an agent loop (rare).
	PredicateResearchParentRole = "research.parent.role"

	// Per-stage completion predicates emitted by the rule chain's five
	// components after each stage finishes. R0-R6 trigger on the
	// false→true transition of these predicates' presence on the loop
	// entity. Values are RFC3339 timestamps so trajectory viewers can
	// reconstruct chain timing without joining against a separate log.
	//
	// Stage transitions are ATOMIC per-component: every stage writes
	// its completion predicate together with any per-stage state
	// (e.g., route.action, assess.sufficient) in a single AddTriplesBatch
	// so a rule that branches on action+complete won't see a stale
	// action paired with a fresh completion timestamp.

	// PredicateResearchClassifyComplete marks nl_classify completion.
	// Stamped together with PredicateResearchClassifyCandidateCount
	// and PredicateResearchClassifyDegraded.
	PredicateResearchClassifyComplete = "research.classify.complete"

	// PredicateResearchClassifyCandidateCount is the number of candidate
	// entities the classifier surfaced. Stored as a string per Triple.Object
	// shape; consumers parse to int.
	PredicateResearchClassifyCandidateCount = "research.classify.candidate-count"

	// PredicateResearchClassifyDegraded marks "true" when the classifier
	// fell back to a degraded path (e.g., semantic-fallback fired).
	// "false" on the happy path.
	PredicateResearchClassifyDegraded = "research.classify.degraded"

	// PredicateResearchRouteComplete marks route_search completion.
	// Stamped together with PredicateResearchRouteAction.
	PredicateResearchRouteComplete = "research.route.complete"

	// PredicateResearchRouteAction is one of ActionSynthesizeDirectly /
	// ActionRetighten / ActionWalkSeeds / ActionDecompose. R2 branches
	// on this value via action-level when clauses (ADR-041).
	PredicateResearchRouteAction = "research.route.action"

	// PredicateResearchExecuteComplete marks execute_subqueries completion.
	// Stamped together with PredicateResearchExecuteEvidenceCount.
	PredicateResearchExecuteComplete = "research.execute.complete"

	// PredicateResearchExecuteEvidenceCount is the number of evidence
	// items the execute stage assembled (post-dedup, post-budget). Stored
	// as a string; consumers parse to int.
	PredicateResearchExecuteEvidenceCount = "research.execute.evidence-count"

	// PredicateResearchAssessComplete marks assess_sufficiency completion.
	// Stamped together with PredicateResearchAssessSufficient.
	PredicateResearchAssessComplete = "research.assess.complete"

	// PredicateResearchAssessSufficient is "true" or "false". R4 branches
	// on this value via action-level when clauses (ADR-041): sufficient
	// → synthesize_answer; insufficient → execute_subqueries (refine
	// loop, bounded by per-action MaxIterations=5).
	PredicateResearchAssessSufficient = "research.assess.sufficient"

	// PredicateResearchSearchResultComplete marks synthesize_answer
	// completion — the chain's terminal stage. R6 (continuation)
	// triggers on this predicate's appearance and routes the
	// SearchResult back to the parent loop via publish_agent.
	PredicateResearchSearchResultComplete = "research.search-result.complete"

	// PredicateResearchSearchResultRef is the KV key where the
	// SearchResult envelope lives (search_result.complete.<loopID> in
	// AGENT_LOOPS). R6's continuation publishes this ref to the parent
	// so the parent's next iteration can read_loop_result on the
	// SearchResult without it riding in the rule payload (per ADR-028:
	// rules carry references, not content).
	PredicateResearchSearchResultRef = "research.search-result.ref"

	// PredicateResearchEvidencePresent is the rule-authored evidence marker.
	PredicateResearchEvidencePresent = "research.evidence.present"

	// PredicateResearchStateStatus is the rule-authored terminal/degraded status.
	PredicateResearchStateStatus = "research.state.status"
)

func init() {
	for _, predicate := range []string{
		PredicateResearchRequested,
		PredicateResearchTopic,
		PredicateResearchHint,
		PredicateResearchBudgetTokens,
		PredicateResearchMaxIterations,
		PredicateResearchParentLoop,
		PredicateLoopRole,
		PredicateResearchLoopID,
		PredicateResearchParentRole,
		PredicateResearchClassifyComplete,
		PredicateResearchClassifyCandidateCount,
		PredicateResearchClassifyDegraded,
		PredicateResearchRouteComplete,
		PredicateResearchRouteAction,
		PredicateResearchExecuteComplete,
		PredicateResearchExecuteEvidenceCount,
		PredicateResearchAssessComplete,
		PredicateResearchAssessSufficient,
		PredicateResearchSearchResultComplete,
		PredicateResearchSearchResultRef,
		PredicateResearchEvidencePresent,
		PredicateResearchStateStatus,
	} {
		vocabulary.Register(predicate)
	}
}

// TripleSource is the Source field on triples emitted by the
// research_graph tool. Mirrors the decide / write_todos convention so
// operators can distinguish chain-kickoff triples from later component
// emissions in graph queries.
const TripleSource = "agent-research-graph"
