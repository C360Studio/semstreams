// Package llmwrap holds helpers shared across the ADR-045 Phase 1
// research-graph chain components (nl_classify, route_search,
// execute_subqueries, assess_sufficiency, synthesize_answer).
//
// Two scopes:
//
//   - LLM-response shape helpers (PR 5): extracting a balanced JSON
//     object out of a model's response (markdown fences + prose
//     preface), and capping a string at N bytes with an ellipsis
//     suffix for error-line readability. Used by the three LLM-
//     wrapping components (route_search, assess_sufficiency,
//     synthesize_answer).
//   - TriplePublisher (PR 6): the graph.mutation publish surface
//     each component uses to stamp orchestration triples on the
//     research-pipeline loop entity (research.classify.complete,
//     research.route.action, etc.). Required by R0-R6 rule chain —
//     the rule engine watches ENTITY_STATES, so per-stage completion
//     must land as entity-state triples for rules to trigger. Used
//     by all five components (LLM-wrapping and code-only alike).
//
// The chat-completion adapter stays per-component so each component
// can keep a narrowly-purposed Router/Assessor/Synthesizer interface
// in its handler; only the cross-cutting helpers live here.
package llmwrap
