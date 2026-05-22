package research

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
	PredicateResearchRequested = "research.requested"

	// PredicateResearchTopic carries the topic verbatim. Distinct from
	// PredicateResearchRequested because the latter doubles as the
	// chain-kickoff trigger; this predicate is the durable record of
	// what the parent actually asked for.
	PredicateResearchTopic = "research.topic"

	// PredicateResearchHint stamps a single hint key/value pair. One
	// triple per hint, with the key encoded in the Object field as
	// "<key>=<value>". Multi-triple is preferred over JSON-stringified
	// hint maps so graph queries can filter on specific hint keys.
	PredicateResearchHint = "research.hint"

	// PredicateResearchBudgetTokens carries the resolved per-call token
	// budget (after defaulting). Stored as string per Triple.Object
	// shape; consumers parse to int.
	PredicateResearchBudgetTokens = "research.budget_tokens"

	// PredicateResearchMaxIterations carries the resolved refine-loop
	// cap (after defaulting).
	PredicateResearchMaxIterations = "research.max_iterations"

	// PredicateResearchParentLoop links the research-pipeline loop
	// back to its parent loop entity ID. Stamped by the research_graph
	// tool from call.LoopID so the continuation rule can route the
	// SearchResult back to the right caller.
	PredicateResearchParentLoop = "research.parent_loop"

	// PredicateLoopRole stamps the loop entity's role. Same convention
	// as other agentic loops; lets ops dashboards filter
	// research-pipeline loops without parsing intent payloads.
	PredicateLoopRole = "loop.role"
)

// TripleSource is the Source field on triples emitted by the
// research_graph tool. Mirrors the decide / write_todos convention so
// operators can distinguish chain-kickoff triples from later component
// emissions in graph queries.
const TripleSource = "agent-research-graph"
