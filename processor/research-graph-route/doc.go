// Package researchroute implements the route_search component from
// ADR-045 Phase 1 (PR 3 of six per
// docs/operations/22-adr045-phase1-plan.md).
//
// route_search is the first LLM-judgment stage of the graph-search
// rule chain. It receives a publish trigger on
// component.route_search.<loop_id>, reads the upstream
// research.Intent + research.ClassifierOutput payloads from
// AGENT_LOOPS, builds a structured-emit prompt, calls the configured
// research_routing LLM endpoint, parses the model's JSON output into
// a research.RouteDecision, validates per-action arg shape, and
// writes the RouteDecision envelope plus a route.complete.<loop_id>
// trigger key that R2 (PR 6) watches to dispatch the next stage.
//
// Architectural notes:
//
//   - LLM-wrapping component, not agent-wrapping. Direct LLM call via
//     the configured CapabilityResearchRouting endpoint; the chain
//     does not delegate decision-making to a free-form ReAct loop.
//
//   - Prompt is structured per discipline memory
//     feedback_persona_prose_needs_decision_criteria: per-action
//     purpose framing + decision-axis framing (no magic-number
//     thresholds) + concrete examples for non-obvious choice points
//     (decompose vs walk_seeds vs retighten) + negative-shape
//     definition for synthesize_directly.
//
//   - Args emitted by the model follow intent-not-structure
//     (ADR-045 Amendment 2026-05-23): decompose carries axes/focus/
//     scope; walk_seeds carries seed references (name/partial_id/
//     candidate_index), not full 6-part entity IDs. Backend
//     (execute_subqueries, PR 4) resolves to typed sub-queries and
//     full IDs.
//
//   - All public methods are safe for concurrent use across loops;
//     the component holds no per-call mutable state. Same pattern as
//     research-graph-classify.
package researchroute
