package research

// Domain and version constants for the research message namespace. All
// three payload types (ResearchIntent, SearchResult, RouteDecision)
// share the domain and version; only the category differs.
const (
	// Domain is the payload registry domain for the ADR-045 graph
	// search rule chain.
	Domain = "research"

	// SchemaVersion is the current schema version for research payloads.
	SchemaVersion = "v1"
)

// Category constants for research payload types.
const (
	// CategoryIntent is the caller's research request. Topic + free-form
	// hints + budget. Entity IDs and predicates are outputs of the chain,
	// not inputs (see ADR-045's classifier-first amendment).
	CategoryIntent = "intent"

	// CategoryResult is the chain's terminal output. Synthesis text plus
	// evidence refs the parent can quote back, plus decomp trace for
	// trajectory review.
	CategoryResult = "result"

	// CategoryRouteDecision is route_search's structured emit: one of
	// four actions plus action-specific args plus rationale.
	CategoryRouteDecision = "route_decision"

	// CategoryClassifierOutput is the nl_classify component's emit:
	// classifier hints + initial candidate set. R1 fires on the
	// matching trigger key; route_search (PR 3) consumes the payload
	// to pick a routing action.
	CategoryClassifierOutput = "classifier_output"
)

// RouteAction values for RouteDecision.Action. Closed enum per ADR-045
// section "Why this is not just-a-better-classifier" — the four actions
// are the load-bearing taxonomy. Validate rejects values outside this
// set so structured-emit retries can trigger per ADR-035.
const (
	// ActionSynthesizeDirectly short-circuits to synthesize_answer when
	// classifier output is already enough to answer the topic. Many
	// parent topics will land here; the chain doesn't pay for multi-hop
	// work it doesn't need.
	ActionSynthesizeDirectly = "synthesize_directly"

	// ActionRetighten re-runs nl_classify with refined topic/hints when
	// the initial classification produced noisy or empty hits. Bounded
	// at MaxIterations=2 on R2's retighten branch.
	ActionRetighten = "retighten"

	// ActionWalkSeeds dispatches execute_subqueries with seed
	// REFERENCES (names, partial IDs, or candidate-list indices) the
	// classifier surfaced. Per ADR-045 Amendment 2026-05-23
	// (intent-not-structure), the model emits references; the backend
	// resolves to full 6-part entity IDs via the entity index. Multi-
	// hop expansion from concrete seeds — structurally different from
	// topic-wide decompose. See WalkSeedsArgs.
	ActionWalkSeeds = "walk_seeds"

	// ActionDecompose dispatches execute_subqueries with the
	// decomposition INTENT (axes, focus, scope). Per ADR-045
	// Amendment 2026-05-23 (intent-not-structure), the model emits
	// intent; the backend constructs the typed sub-queries from
	// templates. The v1 default-path now used only when the
	// classifier shows it's necessary. See DecomposeArgs.
	ActionDecompose = "decompose"
)

// DefaultBudgetTokens is ResearchIntent.BudgetTokens default per
// docs/operations/22-adr045-phase1-plan.md PR 1 spec.
const DefaultBudgetTokens = 4000

// DefaultMaxIterations is ResearchIntent.MaxIterations default. The
// refine loop cap on R4 ships with the same default — see ADR-045
// rule chain R4.
const DefaultMaxIterations = 5

// PipelineRole is the role string carried on the research-pipeline
// LoopEntity in AGENT_LOOPS. Lets downstream tooling (trajectory
// viewers, ops dashboards) filter research loops from coordinator and
// task loops without parsing intent payloads.
const PipelineRole = "research_pipeline"
