// Package researchassess implements the assess_sufficiency component
// from ADR-045 Phase 1 (PR 5 of six per
// docs/operations/22-adr045-phase1-plan.md).
//
// assess_sufficiency is the second LLM-judgment stage of the graph-
// search rule chain (after route_search). It receives a publish
// trigger on component.assess_sufficiency.<loop_id>, reads the
// upstream research.Intent + research.ExecutionOutput payloads from
// AGENT_LOOPS, builds a structured-emit prompt, calls the configured
// research_assessment LLM endpoint, parses the model's JSON output
// into a research.AssessmentOutput, and writes the AssessmentOutput
// envelope plus an assess.complete.<loop_id> trigger key that R3
// (PR 6) watches to dispatch synthesize_answer (Sufficient=true) or
// the refine loop (Sufficient=false).
//
// Architectural notes:
//
//   - LLM-wrapping component, not agent-wrapping. Direct LLM call via
//     the configured CapabilityResearchAssessment endpoint; the chain
//     does not delegate decision-making to a free-form ReAct loop.
//
//   - Prompt is structured per discipline memory
//     feedback_persona_prose_needs_decision_criteria: per-decision
//     framing for Sufficient vs Refine, concrete examples for the
//     "looks adequate but isn't" trap, and negative-shape definition
//     for Sufficient=true (when NOT to short-circuit).
//
//   - RefinedQueries are intent-shaped per
//     feedback_tool_signature_intent_not_structure: the model emits
//     short natural-language gap descriptors; the backend constructs
//     typed dispatches downstream. The model is not asked to spell
//     sub-query types it can't reliably produce.
//
//   - Per-loop ExecutionOutput sample is bounded by config
//     (MaxEvidenceInPrompt) so the prompt stays within small-model
//     context windows. Evidence is rendered with stable ordering
//     (Score descending; ties broken by EntityID) so the same loop
//     replayed produces byte-identical prompts.
//
//   - Authored Rationale predicate is rule-opaque per
//     feedback_llm_authored_predicates_rule_opaque — rules branch on
//     the typed Sufficient boolean only.
//
//   - All public methods are safe for concurrent use across loops;
//     the component holds no per-call mutable state. Same pattern as
//     research-graph-route + research-graph-execute.
package researchassess
