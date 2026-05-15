// Package agenticdetonator implements ADR-043 Phase 3: offline
// labeling of untrusted text via a sandboxed canary loop with a
// fake tool registry (WildcardExecutor) plus a markdown-URL
// scanner. Output records flow into the Phase 2
// processor/agentic-governance/injection_corpus loader.
//
// Phase 3 scope:
//
//   - 3a (this slice): WildcardExecutor + signal extraction +
//     markdown-URL scanner + Detonation aggregator. Pure-Go module,
//     no LLM. Library only — the canary loop in 3b is the first user.
//   - 3b: Canary loop wrapping the agentic-model adapter with
//     per-detonation cost caps (max turns / max tool calls / max
//     tokens) and ADR-024 layered timeouts.
//   - 3c: cmd/detonate-injections/ batch CLI. JSONL in → JSONL out
//     in the existing injectioncorpus.Record format. sha256
//     idempotency.
//   - 3d: First-user proof — detonate an unlabeled OSINT-shape
//     batch, run the Phase 2 measurement harness before/after,
//     document the delta.
//
// Storage choice (Phase 3): JSONL files in the existing corpus
// format. The INJECTION_LABELS_{tenant} KV bucket from
// ADR-043 §"Cache size and policy" is explicitly Phase 4 and
// compounds with multi-tenant isolation; not a Phase 3 concern.
package agenticdetonator
