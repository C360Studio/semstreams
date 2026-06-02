// Package researchsynthesize implements the synthesize_answer
// component from ADR-045 Phase 1 (PR 5 of six per
// docs/operations/22-adr045-phase1-plan.md).
//
// synthesize_answer is the chain's terminal LLM stage. It receives a
// publish trigger on component.synthesize_answer.<loop_id>, reads the
// upstream research.Intent + research.ExecutionOutput payloads (and
// the research.RouteDecision when present, for the DecompTrace
// audit), builds a structured-emit prompt, calls the configured
// research_synthesis LLM endpoint, parses the model's JSON output,
// runs quote-back validation, and writes the research.SearchResult
// envelope at search_result.complete.<loop_id> per ADR-045 R6 — the
// continuation rule (R6 in PR 6) watches this terminal trigger key
// to deliver the result back to the parent loop. The trigger key
// names the payload shape, not the producing component, so the
// continuation rule's key_pattern stays stable across synthesizer
// reshuffles.
//
// Architectural notes:
//
//   - LLM-wrapping component, not agent-wrapping. Direct LLM call via
//     the configured CapabilityResearchSynthesis endpoint.
//
//   - Quote-back validation: every ObjectStoreRef and EntityID the
//     model emits in evidence_refs MUST appear in the input evidence
//     array. Refs that do not match are stripped and a Warn is
//     emitted; if the model emits zero valid refs against a non-
//     empty evidence array, the synthesis is downgraded to degraded
//     so operators can spot drift quickly. This enforces ADR-045's
//     "no fabricated refs" contract.
//
//   - DecompTrace is constructed server-side from the RouteDecision +
//     ExecutionOutput; the model is not asked to spell it. The chain
//     is the authoritative source of structural-decision history.
//
//   - Per-loop evidence sample is bounded by config
//     (MaxEvidenceInPrompt) so the prompt stays within small-model
//     context windows. Evidence is rendered with stable ordering
//     (Score descending; ties broken by EntityID) so the same loop
//     replayed produces byte-identical prompts.
//
//   - Authored synthesis prose carries no rule-matchable predicate —
//     rules pattern-match on the SearchResult schema (Synthesis
//     present, evidence_refs present), not on prose content.
//
//   - All public methods are safe for concurrent use across loops;
//     the component holds no per-call mutable state. Same pattern as
//     research-graph-assess + research-graph-route.
package researchsynthesize
