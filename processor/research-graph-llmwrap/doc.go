// Package llmwrap holds JSON-extraction and truncation helpers shared
// across the LLM-wrapping components of the ADR-045 Phase 1 research-
// graph chain (route_search, assess_sufficiency, synthesize_answer).
//
// Scope is deliberately small: extracting a balanced JSON object out
// of a model's response (markdown fences + prose preface), and
// capping a string at N bytes with an ellipsis suffix for error-line
// readability. The chat-completion adapter stays per-component so
// each component can keep a narrowly-purposed Router/Assessor/
// Synthesizer interface in its handler; only the response-shape
// helpers live here.
//
// Promoted to a shared package in ADR-045 Phase 1 PR 5 (the second
// and third LLM-wrapping components). Previously inlined in
// processor/research-graph-route/handler.go; that copy is removed in
// the same PR.
package llmwrap
