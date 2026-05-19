# ADR-045: Graph Search Decomp+Fusion via Rule-Chain + Components

## Status

**Proposed — 2026-05-19.** Doc-only ADR; no implementation in this tag.
Independent of ADR-039 (governance), ADR-042 (publisher-mode), ADR-043
(detonation corpus), ADR-044 (CS API framework split). Builds on existing
primitives only: rule engine (`processor/rule/` with per-action
`MaxIterations`), component framework, AGENT_LOOPS KV, ObjectStore via
`ContentStorable`, triples on entities. Does **not** introduce new KV
buckets or new orchestration primitives.

Forcing function: the "agent search problem" recurring across the
agentic stack lacks a clean home in semstreams. The naive answers — a
search agent role, a `search()` tool in the parent's allowlist, or a
pre-LLM substrate that runs unconditionally — each fail against our
empirical floor (`feedback_graph_not_for_agent_reasoning`,
`feedback_frontier_floor_changes_role_split_calculus`) or the
reasoning-crimp pathology documented below. This ADR proposes a
**coordinated rule chain orchestrating typed components** as the
graph-search decomp+fusion primitive — the canonical semstreams
"how we do workflows" pattern applied to graph retrieval.

## Context

### The "agent search problem"

[Kumar, 2026](https://dipkumar.dev/posts/agents/agent-search-problem/)
catalogs five failure shapes that converge on one bottleneck: agents
must *discover* the right information source before they can reason —
web search rendering and freshness, RAG multi-hop and chunk
composition, MCP tool discovery context-blowing, skills loading, and
in-context navigation. Kumar's punchline frames the gap as
**infrastructure**, not a reasoning deficit.

### Why this maps hard onto semstreams

semstreams is a knowledge-graph engine with tiered inference (rules /
BM25 / neural) and multi-gateway query access (GraphQL, MCP, NATS
Direct). The substrate for unified retrieval exists; the unresolved
question is **where the decomp+fusion step lives** between the
gateways and the agent's prompt.

Two empirical findings define the floor any solution must clear:

- **`feedback_graph_not_for_agent_reasoning`** (3 instrumented
  incidents). Frontier agents (Gemini 3.x Pro, Sonnet 4.6) do **not**
  navigate the SKG even with `search_graph` / `query_entity` /
  `summarize_graph` in the allowlist and the persona prompt explicitly
  encouraging graph-first lookups. semspec's recovery diagnosis quoted
  verbatim: *"graph_search returned [project] org.sensorhub but agent
  ignored it."* The fix that worked was **injection-side** (lineage
  triples into the prompt payload), not query-side.

- **`feedback_frontier_floor_changes_role_split_calculus`**. semteams
  ADR-040 split researcher+curator on small-model cognitive-load
  grounds; ADR-041 re-collapsed them on frontier-floor grounds. A
  persistent "search agent" chain role would re-pay the orchestration
  cost ADR-041 just amortized down.

### The reasoning crimp (the structural finding)

Independent of those two memories, a third structural problem exists
when search-class tools sit alongside traditional tools (`web_search`,
`bash`, `read_file`, …) in the agent's allowlist: **the agent
systematically does not choose them unless forced.**

This is not prompt tuning. It is selection bias driven by training
ergonomics: web search / bash / file I/O have thousands of trajectories
in training data; multi-hop graph-walk + fuse-results does not. The
LLM lacks trained ergonomics for **graph-shape reasoning loops** even
when only graph tools are present. Prompt-side encouragement has not
closed the gap in instrumented runs.

This rules out any architecture requiring the LLM to *drive* a graph
tool loop — including a constrained-allowlist subagent that has only
graph tools. The model doesn't crimp toward `bash`; it crimps toward
in-context priors, because composing multi-hop graph traversals is
unfamiliar work for it.

### The semspec trap (and why we won't repeat it)

semspec was an early adopter, predating the mature rule engine. To
work around limitations, it built **its own plan + execution state
machines** alongside rules — roughly 7,264 LOC of `workflow/reactive/`
code that imports the now-retired `processor/reactive/` engine and
maintains its own state plumbing. That code is now a migration
blocker (Phase 5 of the reactive-workflow retirement) and is unlikely
to be dug out anytime soon.

The lesson is durable: **gaps in the framework surface upstream as
engine work; they never get worked around as app-side state
plumbing.** This ADR commits to using existing primitives only. If
implementation reveals a genuine gap, it gets filed as a discrete
engine-improvement ticket, not patched over with new KV buckets or
custom state machines.

### The trained-ergonomics seam

What the LLM *is* trained for: read text, summarize text, decompose
questions into sub-questions, judge relevance, compose answers from
sources. What it is *not* trained for: orchestrate a sequence of
typed graph queries, normalize scores across retrieval tiers, dedup
multi-hop walk results, manage iteration budgets.

The architecture should cut along this seam: **code does what code
is good at, LLM does what LLM is good at**, and the LLM is invoked
in bounded structured-emit calls at the judgment points where its
language strengths apply — not as a multi-turn agent loop driving
graph tools it wasn't trained to drive.

## Decision

Graph search decomp+fusion is implemented as a **coordinated rule
chain orchestrating typed components**, following the canonical
semstreams multi-step pattern documented in
[docs/concepts/14-orchestration-layers.md](../concepts/14-orchestration-layers.md).
No new orchestration primitive, no new state bucket, no persona dir.

### Parent's surface

One new tool exposed in the parent's allowlist:

```
research_graph(intent) → async search_result
```

Calling it emits `research.requested.{loop_id}` (a triple +
ContentStorable ref for the intent payload) and terminates the
parent's current iteration. The result arrives on a subsequent
iteration via the standard continuation rule pattern (same shape as
any agent loop completion).

The intent payload is structured (not free-form text):

```json
{
  "topic": "drone hover anomalies",
  "entities": ["acme.ops.robotics.gcs.drone.001"],
  "predicates": ["sosa:hasResult", "ssn:hasDeployment"],
  "tiers": ["rules", "bm25"],
  "budget_tokens": 4000,
  "max_iterations": 5
}
```

### Internal architecture

```
Parent → research_graph(intent)  → terminates iteration
            │
            ▼  R1 fires on research.requested.*
[component] decompose_intent
            │  one LLM call, structured emit: typed sub-queries
            ▼  R2 fires on decompose.complete.*
[component] execute_subqueries
            │  CODE: parallel multi-tier fan-out via existing gateways
            │  Tier 0 (rules/predicates) + Tier 1 (BM25) [+ Tier 2 in Phase 2]
            │  emits evidence array to ObjectStore, ref-triples on loop entity
            ▼  R3 fires on execute.complete.*
[component] assess_sufficiency
            │  one LLM call, structured emit: {sufficient: bool, refined?}
            ▼  R4 conditional branch:
            │   - sufficient=false AND iterations<5  → fire execute_subqueries (refined)
            │   - sufficient=true OR iterations=5    → fire R5
            │  (per-action MaxIterations=5 caps the refine loop)
            ▼
[component] synthesize_answer
            │  one LLM call, structured emit: final synthesis
            ▼  R6 emits search_result terminal, fires R-cont
[continuation rule]
            ▼
Parent resumes with search_result evidence in prompt
```

### Components (four small additions, each a single-purpose unit)

| Component | Type | Body |
|---|---|---|
| `decompose_intent` | LLM-wrapping | One bounded structured-emit call: `intent → typed sub-queries[]`. Template fast-path for common shapes (entity_state, predicate_walk, temporal_range, spatial_polygon); LLM call only for novel topics. |
| `execute_subqueries` | Code | Parallel multi-tier fan-out via existing GraphQL/MCP/NATS-Direct gateways. Score normalization, dedup, ranking. Budget enforcement. Writes evidence to ObjectStore; ref-triples to the research-loop entity. |
| `assess_sufficiency` | LLM-wrapping | One bounded structured-emit call: `(intent, evidence) → {sufficient: bool, refined_queries?: []}`. The "refine or finalize" decision. |
| `synthesize_answer` | LLM-wrapping | One bounded structured-emit call: `(intent, evidence) → synthesis`. Refs preserved verbatim. |

The three LLM-wrapping components share underlying infrastructure
(`model.NewHTTPClient`, beta.43 unified LLM HTTP client invariant).
Each has its own prompt template specialized for its task — not a
free-form agent persona, structured input/output schemas. No persona
dir.

### State storage (uses existing buckets only — no new plumbing)

| State | Where it lives | Reuses |
|---|---|---|
| Research operation's identity + iteration count | AGENT_LOOPS KV entry (role = `research_pipeline`) | Existing bucket; same primitives as any agent loop |
| Original intent + refined-query history | Triples on the research-loop entity | `Graphable` interface; standard pattern |
| Evidence (potentially bulky) | ObjectStore via `ContentStorable`; ref-triples on loop entity | Existing pattern per ADR-028 ("rules carry references, never content") |
| Decomposition output, assessment result, synthesis | Triples on the research-loop entity + ObjectStore refs for large payloads | `Graphable` interface |
| Iteration cap for refine loop | Per-action `MaxIterations=5` on R4 | Existing rule-engine primitive |
| Search result returned to parent | ObjectStore object + ref-triple; continuation rule fires parent | Same pattern as any agent loop completion |

**No new KV buckets. No new state machines. No new orchestration
primitive.** Every load-bearing primitive cited is already in `main`.

### Rule chain

Six rules in the flow config (R1–R6 plus the continuation rule):

```yaml
# R1: kick off the chain
- name: research_decompose
  when: kv_write
    bucket: AGENT_LOOPS
    key_pattern: "research.requested.*"
  actions:
    - type: publish
      subject: "component.decompose_intent.{loop_id}"

# R2: after decompose, fan out
- name: research_execute
  when: kv_write
    bucket: AGENT_LOOPS
    key_pattern: "decompose.complete.*"
  actions:
    - type: publish
      subject: "component.execute_subqueries.{loop_id}"

# R3: after execute, assess
- name: research_assess
  when: kv_write
    bucket: AGENT_LOOPS
    key_pattern: "execute.complete.*"
  actions:
    - type: publish
      subject: "component.assess_sufficiency.{loop_id}"

# R4: conditional refine OR synthesize
- name: research_refine
  when: kv_write
    bucket: AGENT_LOOPS
    key_pattern: "assess.complete.*"
  actions:
    - type: publish
      subject: "component.execute_subqueries.{loop_id}"
      when: "$state.assess.sufficient == false"
      max_iterations: 5                       # per-action cap is the loop limit
    - type: publish
      subject: "component.synthesize_answer.{loop_id}"
      when: "$state.assess.sufficient == true OR $state.iterations >= 5"

# R5: terminal emit (synthesize component writes search_result.complete on success)

# R6: continuation back to parent
- name: research_continuation
  when: kv_write
    bucket: AGENT_LOOPS
    key_pattern: "search_result.complete.*"
  actions:
    - type: publish_agent
      role: "{state.parent_role}"
      payload_ref: "{state.search_result_objstore_ref}"
```

All six rules use existing JSON rule definitions and existing action
types (`publish`, `publish_agent`). The `when` clauses use the
unified condition evaluator (ADR-041). `MaxIterations` is the
existing per-action firing cap.

### Why this is not just-a-better-GraphQL

The chain has **agent reasoning at every judgment point** —
decomposition (decide which sub-queries to issue from a fuzzy topic),
assessment (judge whether evidence answers the question, decide
whether to refine), and synthesis (compose the answer with context).
A pure deterministic pipeline can't do any of these well. What's
different from a free-form agent loop is that each LLM invocation is
**bounded, structured, single-turn** — the LLM never *drives* a tool
loop, it answers a specific structured question and emits a typed
result. That's the trained-ergonomics seam.

### Why this avoids the semspec trap

Every load-bearing primitive is already in the framework:

- Coordinated rule chain → existing `processor/rule/`
- Per-action iteration cap → existing `MaxIterations`
- Conditional branching → existing action `when` clauses (ADR-041
  unified evaluator)
- State on entity → existing `Graphable` interface + triples
- Bulky payload handling → existing `ContentStorable` + ObjectStore
- Continuation back to parent → existing rule + `publish_agent`
  pattern

The four new components are normal components — same lifecycle, same
port discipline, same flow-discovery, same flowgraph validation as
every other component. No bypass paths, no parallel state machine,
no app-side workflow engine.

### Flow packaging (two valid paths, decided at implementation)

Two equally valid ways to package the rule chain:

1. **Inline rules in the parent's flow config** (Phase 1 recommended).
   Add R1–R6 + four components to whichever flow needs research.
   Simplest; one new flow, no new spawn primitive.
2. **Standalone deployable flow** spawned via
   `start_flow(research_graph_flow_id, intent)` from ADR-042 Phase 4
   (Phase 2+ recommended). Multiple flows reuse the same research
   chain; one canonical implementation.

Both use existing primitives. The Phase 1 → Phase 2 transition is a
configuration change, not an architecture change.

## Consequences

### Wins

- **Dodges the reasoning crimp and trained-ergonomics gap.** The LLM
  is invoked at the language tasks it's trained for (decompose,
  assess, synthesize); code does the multi-tier orchestration the
  LLM isn't trained to drive.
- **Dodges the graph-not-for-reasoning failure mode.** The agent
  never decides to navigate the graph — it just gets fused evidence
  in the next loop's prompt. Same injection-side pattern that worked
  for lineage triples in beta.51.
- **Dodges role-split orchestration cost.** No persona dir, no chain
  role, no per-role rule wiring beyond R1–R6.
- **Dodges the semspec trap.** Every primitive is existing. No new
  KV buckets, no parallel state machines, no app-side workflow
  engine.
- **First-class observability.** Every stage transition is a triple
  written to AGENT_LOOPS; the rule engine's audit trail covers the
  whole chain natively.
- **Restart-safe by default.** Each component completes and writes
  state before the next fires; partial failures resume from the
  last completed stage via standard rule re-evaluation.
- **Testable.** Each component has unit-test boundaries (input
  payload → output emit). The chain itself is integration-testable
  with mock components.
- **Operator-configurable.** Per-role enable, budget caps, tier mix
  are flow config — not code.

### Costs

- **Multi-stage latency.** Three to four rule fires + three LLM
  calls + N code stages per research call. Mitigation: parallel
  fan-out inside `execute_subqueries`; KV cache for hot intents;
  P95 SLO.
- **Parent residual crimp.** Parent still chooses whether to call
  `research_graph`. Smaller surface than multi-tool but not zero.
  Mitigation: one-line persona hint ("when in doubt, delegate via
  `research_graph`"); ops `emit_diagnosis` flags missed
  delegations.
- **Four new components.** Maintenance surface. Mitigation: each is
  single-purpose, small, shares LLM-call infrastructure.
- **Decomposer can hallucinate sub-queries.** Mitigation: structured
  emit + schema validation + template fast path covers common
  intents without LLM.
- **Synthesis can fabricate refs.** Mitigation: refs must validate
  against actual evidence in ObjectStore; schema-level enforcement,
  not prompt-level.
- **Per-role configuration surface grows.** Each parent role gets
  `research_graph` enable + budget caps. Mitigation: sensible
  defaults; opt-in.

### Out of scope (deferred)

- LLM-driven inference of intent from unstructured prior parent
  output (re-introduces a search choice via the back door).
- Mid-chain escalation to a peer research operation for cross-corpus
  fusion (Phase 4).
- Cross-tenant evidence pooling (ADR-032 territory).
- Multi-modal evidence (images, audio, structured tables).
- Real-time evidence freshening during a single research call.

## Alternatives considered

### A. Persistent search-agent chain role

A permanent persona dir + rules + contract for a search-specialist
role participating alongside coordinator/researcher/curator.

**Rejected.** Re-pays the role-split orchestration cost ADR-041 just
amortized. Persistent chain role = persistent rule wiring + persona
fragment per chain + transition contracts upstream. Rule chain
gets the same benefit at a fraction of the cost.

### B. Tool-among-tools (`search()` next to `bash`, `web_search`, …)

Expose decomp+fusion as one tool in the parent's standard allowlist.

**Rejected.** The reasoning crimp. Agents do not choose search tools
when traditional tools are co-available. Prompt-side encouragement
has not closed the gap.

### C. Constrained-allowlist subagent loop

Spawn a transient subagent (via `start_flow`) with a constrained
allowlist of only graph + fusion tools; the subagent's LLM drives
a ReAct loop over the constrained surface.

**Rejected.** The trained-ergonomics gap. Even with no alternative
tools to crimp toward, the LLM lacks fluent trained behavior for
multi-hop graph-walk + fuse-results trajectories. Asking the LLM
to drive a graph tool loop is the failure mode, not the constraint
on alternatives. Cleaner to invoke the LLM at structured language
tasks (decompose, assess, synthesize) than to ask it to drive a
loop it wasn't trained for.

### D. Smarter graph-side queries (gateway-only)

Improve GraphQL/MCP/NATS-Direct gateways to do query rewriting,
multi-hop expansion, fusion at the gateway. Parent calls
`search_graph` as today; gateway does the decomp transparently.

**Rejected as primary; adopted as complementary.** Two issues as
primary: still requires the parent to choose `search_graph`
(reasoning crimp applies); mixes query-pattern boundaries
(`feedback_gateway_first_only_for_new_capabilities`). But gateway
improvements *do* increase `execute_subqueries`'s output density and
reduce iteration count; they ship on their own cadence as supporting
work.

### E. Pre-LLM substrate enrichment (deterministic decomp+fusion)

Run decomp+fusion as a framework-level component *before* the LLM
call, unconditionally. Rule-triggered on every loop input creation.

**Rejected.** Three issues:
- Wasted work when not needed. Substrate runs whether the parent
  required fresh evidence or not.
- Deterministic decomp+fusion is hard to write well. Score
  normalization, sub-query expansion, semantic relevance — all
  under-specified without LLM reasoning at judgment points.
- Loses the principal-agent boundary. Substrate enrichment blurs
  "what the parent asked for" vs "what the framework pre-stuffed."

The rule chain keeps the request/response boundary crisp:
parent calls `research_graph(intent)`, components emit
`search_result(evidence)`. Both are auditable artifacts.

### F. Prompt-side encouragement of existing graph tools

"You should use `search_graph` as a first step for any question
about entities."

**Rejected — already tried.** semspec's recovery diagnosis quotes
the agent ignoring `graph_search` results even with explicit prompt
encouragement. Persona prompts already say "Try FIRST for project
lookups." Behavior unchanged across three instrumented incidents.

### G. Status quo (do nothing)

Continue with direct-tool exposure; treat failures as prompt tuning.

**Rejected.** Three instrumented incidents across two sister projects
converging on the same shape. Tuning has not moved the floor.

## Phased rollout

**Phase 1** (proposed for next eligible tag — not beta-blocking):

- Four new components: `decompose_intent`, `execute_subqueries`,
  `assess_sufficiency`, `synthesize_answer`. Each ships with unit
  tests against the production decoder per
  `feedback_production_decoder_round_trip_required`.
- One new parent-side tool: `research_graph(intent)` registered per
  `/new-payload` discipline.
- Two new payload types: `research_intent` + `search_result`
  registered in the payload registry.
- Six rules (R1–R6) bundled in a reference flow config; parent flows
  opt in by including the rules or by spawning the flow via
  `start_flow` (Phase 2 path).
- Tier coverage: Tier 0 (rules) + Tier 1 (BM25) inside
  `execute_subqueries`. Tier 2 (neural) deferred to Phase 2.
- Decomposer: template fast-path only for `entity_state` /
  `predicate_walk` / `temporal_range` / `spatial_polygon`. LLM-driven
  decomp for novel topics deferred to Phase 2 (Phase 1 falls through
  to a degraded "full-text search of intent topic" path for
  un-templated intents).
- In-repo smoke test against the deep-research e2e flow.
- Pre-tag e2e green per
  `feedback_e2e_required_for_breaking_changes` (this changes the
  framework-level pattern catalog; touch is contained but the new
  components need flow-discovery exposure).

**Phase 2** (gated on Phase 1 operator validation):

- Tier 2 (neural) added to `execute_subqueries`.
- LLM-driven decomp inside `decompose_intent` for novel topics.
- Token-budget compression via small-LLM emit (refs preserved).
- Per-parent-role enablement (researcher, curator, ops opt in with
  different budgets).
- Deployable as a standalone flow (Phase 2 spawn path via
  `start_flow`).
- ops `emit_diagnosis` flags for decomp/assess/synthesis quality.

**Phase 3** (gated on Phase 2 + external use case):

- Web search and external-tool fan-out added to `execute_subqueries`
  *with detonation pre-flight* (ADR-043) — hard gate, not optional.
- Cross-source dedup and provenance harmonization.

**Phase 4** (deferred — research):

- Cross-corpus research-to-research escalation.
- Real-time evidence freshening during a single research call.
- Multi-modal evidence sources.

## Open questions

1. **Cache key for hot intents.** Same intent from the same
   chain-entity should not re-run the full chain every iteration.
   Invalidation predicate: wall-clock TTL, revision count on
   referenced entities, or both? Resolved in Phase 1 implementation.

2. **Continuation-rule deadline + fallback.** If any stage in the
   chain stalls or fails, the parent must resume with
   `search_result.error` evidence so it can make a degraded
   decision rather than wedge. Deadline value and degraded-result
   schema TBD in Phase 1.

3. **Engine gaps surfaced during implementation.** If implementation
   reveals that the current rule engine cannot express something
   needed (e.g., reading evidence-array length in a `when` clause to
   gate the refine branch), the gap gets filed as a discrete
   engine-improvement ticket — not as inline state plumbing in the
   research chain. The semspec trap is explicit prior art.

4. **Interaction with strict tool calling (ADR-035).** Both
   `research_intent` and `search_result` payloads need formal
   schema registration per `/new-payload`. Phase 1 includes the
   registry entries + round-trip production-decoder test.

5. **Interaction with governance (ADR-039).** The four components'
   LLM calls go through the same rule-driven governance layer as
   any other LLM call. Per-loop vs per-role boundaries per
   `feedback_per_loop_vs_per_role_safety` apply.

6. **Detonation corpus coverage.** Phase 1 reads from graph +
   ObjectStore (lower prompt-injection risk than web sources, but
   not zero — BM25 snippets from ingested docs are a possible
   vector). Phase 3 web fan-out *requires* detonation pre-flight
   integration — track as a hard gate.

7. **Parent-side delegation hygiene.** How aggressively to prompt
   the parent toward calling `research_graph`. Initial proposal:
   one-line persona hint; ops flags missed delegations for
   trajectory review.

## References

- [Kumar, "The Agent Search Problem,"
  2026](https://dipkumar.dev/posts/agents/agent-search-problem/)
- [ADR-027: Ops Agent Meta-Harness](027-ops-agent-meta-harness.md)
- [ADR-028: Agentic Orchestration Architecture](028-orchestration-architecture.md)
- [ADR-035: Strict Tool Calling](035-strict-tool-calling.md)
- [ADR-039: Tool-call Governance Rule-Driven](039-tool-call-governance-rule-driven.md)
- [ADR-041: Unified Condition Evaluator](041-unified-condition-evaluator.md)
- [ADR-042: OASF Taxonomy Adoption (Phase 4 deploy_flow agent-tools)](042-oasf-taxonomy-adoption.md)
- [ADR-043: Prompt-Injection Defense Detonation Corpus](043-prompt-injection-defense-detonation-corpus.md)
- [docs/concepts/14: Orchestration Layers — How We Do Workflows in
  semstreams](../concepts/14-orchestration-layers.md) — the canonical
  pattern catalog this ADR's rule chain instantiates
- `/kv-or-stream`, `/query-pattern`, `/orchestration-check`,
  `/new-payload` skills
- Memory: `feedback_graph_not_for_agent_reasoning`
- Memory: `feedback_frontier_floor_changes_role_split_calculus`
- Memory: `feedback_gateway_first_only_for_new_capabilities`
- Memory: `feedback_llm_authored_predicates_rule_opaque`
- Memory: `feedback_per_loop_vs_per_role_safety`
- Memory: `feedback_production_decoder_round_trip_required`
- Memory: `feedback_e2e_required_for_breaking_changes`
- Memory: `project_reactive_workflow_retirement` — context for why
  this ADR uses rule-chain + components rather than a workflow
  primitive
