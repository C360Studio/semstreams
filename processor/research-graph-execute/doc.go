// Package researchexecute implements the execute_subqueries
// component from ADR-045 Phase 1 (PR 4 of six per
// docs/operations/22-adr045-phase1-plan.md).
//
// execute_subqueries is the code-heavy stage of the graph-search
// rule chain. It receives a publish trigger on
// component.execute_subqueries.<loop_id>, reads the upstream
// research.Intent + research.RouteDecision payloads from
// AGENT_LOOPS, materializes one or more typed sub-queries from
// the routing intent, executes them in parallel across multiple
// retrieval tiers, normalises + dedups results, enforces the
// caller's token budget, and writes a research.Evidence array
// envelope plus an execute.complete.<loop_id> trigger key that R3
// watches to dispatch the assess_sufficiency stage.
//
// Two input shapes — same component:
//
//   - walk_seeds args: model emits seed references
//     (name/partial_id/candidate_index); component resolves to full
//     6-part federated entity IDs via the entity index, then
//     dispatches multi-hop expansion as predicate_walk sub-queries.
//
//   - decompose args: model emits decomposition intent
//     (axes/focus/scope); component materializes typed sub-queries
//     from MINIMAL TEMPLATES — entity_type → entity_state,
//     time → temporal_range, predicate → predicate_walk (the
//     fallback for axes that don't map to a dedicated primitive).
//     spatial is deferred to Phase 2 (graph-index-spatial wire).
//
// Architectural notes:
//
//   - Pure-code component (no LLM calls). All work is graph + index
//     retrieval against existing gateways.
//
//   - Tier 0 (predicate queries) executes via graph-query NATS-
//     direct subjects (graph.query.entitiesByPrefix etc.). Tier 1
//     (BM25) executes via graph.query.searchGraph (same surface
//     research-graph-classify uses for initial candidate retrieval).
//     Tier 2 (neural) is deferred to Phase 2.
//
//   - Sub-query types are component-internal — defined here, not in
//     agentic/research/. Reshapeable without cross-package churn if
//     Phase 2 learns better primitives.
//
//   - Parallel fan-out via errgroup with a bounded concurrency cap
//     (config-driven). Per-tier ordering with recency tie-break;
//     learned ranker deferred to Phase 2.
//
//   - Provenance preserved end-to-end — every Evidence carries
//     {tier, source, entity_id} the agent can quote back. No
//     fabricated refs.
//
//   - All public methods safe for concurrent use across loops; the
//     component holds no per-call mutable state. Same pattern as
//     research-graph-classify and research-graph-route.
package researchexecute
