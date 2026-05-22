// Package researchclassify implements the nl_classify component from
// ADR-045 Phase 1 (PR 2 of six per
// docs/operations/22-adr045-phase1-plan.md).
//
// nl_classify is the thinnest stage of the graph-search rule chain.
// It receives a publish trigger on component.nl_classify.<loop_id>,
// reads the research.Intent payload from AGENT_LOOPS, runs the
// existing graph/query.ClassifierChain (keyword + optional embedding
// + optional LLM variants) on the topic, executes the resulting
// SearchOptions against the graph to retrieve initial candidates,
// and writes a research.ClassifierOutput payload that R1 (PR 6)
// will fire route_search on.
//
// Architectural notes:
//
//   - The component wraps the existing classifier primitive; no new
//     LLM calls beyond what the classifier chain already performs.
//   - SearchOptions execution flows through the existing
//     graph.query.searchGraph NATS surface so PR 4
//     (execute_subqueries) and this component share one evidence
//     schema (Candidate ≡ subset of GlobalSearchResponse.EntityDigests).
//   - All public methods are safe for concurrent use across loops;
//     the component holds no per-call mutable state.
package researchclassify
