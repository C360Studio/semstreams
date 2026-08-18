# ADR-045 Phase 1 — research-graph rule pack (R0–R6)

Reference rule chain for the ADR-045 graph-search-decompose-and-fusion
pattern. Wires the five research-graph components
(`nl_classify`, `route_search`, `execute_subqueries`,
`assess_sufficiency`, `synthesize_answer`) into a single coordinated
pipeline.

Under ADR-075 this pack is part of the **atomic graph-research framework
capability**, together with the five components, research payloads,
`research_graph`, and `read_loop_result`. Selecting any part selects the
capability; boot validation rejects missing or incoherent components,
model routes, loop storage, tool access, or canonical rule files. Products
own their personas and domain policy, but not an alternate partial copy of
this framework rule chain.

See:

- [`docs/adr/045-graph-search-rule-chain.md`](../../../docs/adr/045-graph-search-rule-chain.md) — architecture + rationale (v2 classifier-first chain).
- [`docs/operations/22-adr045-phase1-plan.md`](../../../docs/operations/22-adr045-phase1-plan.md) — the PR sequence this rule pack closes (PR 6).
- [`docs/concepts/25-phased-agentic-chains.md`](../../../docs/concepts/25-phased-agentic-chains.md) — the substrate-vs-application split that motivates this pack's positioning.
- [`configs/examples/research-graph-pipeline.json`](../../../configs/examples/research-graph-pipeline.json) — the reference flow that loads this pack.

## Triple-driven orchestration (not raw KV-write triggers)

The ADR's original spec uses pseudo-syntax `when: kv_write { bucket:
AGENT_LOOPS, key_pattern: "<step>.complete.*" }`. The actual rule
engine (`processor/rule/entity_watcher.go`) is entity-state-centric —
it watches a KV bucket and evaluates rules against `EntityState`-
shaped values. The components write per-stage envelopes
(`BaseMessage`) to AGENT_LOOPS for downstream component consumption,
but those aren't EntityState-shaped and can't directly drive rules.

So the chain is wired through **entity-state triples** on the
research-pipeline loop entity instead. Each component stamps a small
atomic batch of triples (e.g., `research.classify.complete`,
`research.route.action`) on the loop entity via the declared
`semstreams.graph.mutation` v1 request port (`graph.mutation.>` family)
after its envelope lands; rules fire on the false→true transition of
those triples' presence via standard entity-state semantics.

Triple builders live in `agentic/research/orchestration.go`; the
shared NATS publisher lives in
`processor/research-graph-llmwrap/triplepub.go`. R0's kickoff triples
are stamped by the `research_graph` tool itself (in
`frameworkcapabilities/graphresearch/executor.go`).

## Rule files

| File | Rule | Fires when | Action |
|------|------|------------|--------|
| `00-kickoff-classify.json` | R0 | `research.requested=true` lands on a `research_pipeline` loop entity | publish to `component.nl_classify.<loopID>` |
| `01-classify-routes.json` | R1 | `research.classify.complete` becomes present | publish to `component.route_search.<loopID>` |
| `02-route-decision-dispatch.json` | R2 | `research.route.complete` becomes present | conditional publish to one of: synthesize_answer (synthesize_directly), nl_classify (retighten — with stage-marker clear), execute_subqueries (walk_seeds or decompose) |
| `03-execute-assesses.json` | R3 | `research.execute.complete` becomes present | publish to `component.assess_sufficiency.<loopID>` |
| `04-assess-dispatch.json` | R4 | `research.assess.complete` becomes present | conditional publish to one of: execute_subqueries (refine, with stage-marker clear — bounded), synthesize_answer (sufficient OR iteration cap) |
| `05-continuation.json` | R6 | `research.search_result.complete` becomes present | publish_agent back to parent loop's role with `read_loop_result(loop_id=<rg_…>)` prompt |

R5 in the ADR is the **synthesis component's terminal write itself**
— there's no separate rule, the synthesize component stamps
`research.search_result.complete` as part of its handler. R6 is the
continuation that fires on that stamp.

## Iteration caps (retighten + refine loops)

Per the ADR-045 spec:

- **R2's retighten branch** has `MaxIterations: 2` — caps the
  classify→retighten ping-pong at 2 round-trips before the chain
  must move forward to multi-hop (walk_seeds / decompose).
- **R4's refine branch** has `MaxIterations: 5` — caps the
  execute→assess→execute refine cycle at 5 rounds before falling
  through to synthesize.

The retighten + refine branches **clear the relevant stage-marker
triples** via `remove_triple` actions before re-dispatching the
upstream component. This lets the rule engine's `on_enter`
(false→true transition) re-fire on the next stamp — the rule engine
doesn't natively re-fire on same-state updates, so the explicit clear
is the iteration-reset mechanism.

## Phase 2 follow-ups

Documented in [`docs/adr/045-graph-search-rule-chain.md`](../../../docs/adr/045-graph-search-rule-chain.md) §Open questions:

- Calibration of MaxIterations from operator trajectory data
  (currently defaults of 2 and 5).
- LLM-driven decomp expansion for novel topics (Phase 1 uses
  template fast-path only).
- A separately composed boot configuration for standalone deployment; saved-flow authoring no longer exposes `start_flow`.
- ops `emit_diagnosis` flags for `route_search` / `assess` /
  `synthesize` quality.
