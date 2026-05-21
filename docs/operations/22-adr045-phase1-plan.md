# ADR-045 Phase 1 Implementation Plan

Sequenced implementation plan for Phase 1 of
[ADR-045](../adr/045-graph-search-rule-chain.md) (graph search
decomp+fusion via rule-chain + components). Breaks the work into six
independently mergeable PRs ordered by dependency, with scope,
deliverables, tests, success criteria, and open-question resolution
points for each.

This document is the implementation orchestration layer. The ADR
defines the architecture; this plan defines the path from `main` to a
green Phase 1 reference flow.

## Reading order

1. [ADR-045](../adr/045-graph-search-rule-chain.md) — the architecture
   and rationale (v2 amendment classifier-first chain).
2. [docs/concepts/14](../concepts/14-orchestration-layers.md) — the
   canonical "How we do workflows in semstreams" pattern catalog this
   plan instantiates.
3. This doc — the PR sequence and per-PR checklist.
4. Per-PR design notes (filed as PR description) as each ships.

## Scope

**Phase 1 only.** Phase 2 (Tier 2 neural, token-budget compression,
per-role configuration, ops diagnosis flags, deployable standalone
flow) and Phase 3 (web/external sources with detonation pre-flight)
are out of scope for this plan. They get their own plan docs once
Phase 1 operator validation is complete.

What ships in Phase 1:

- Five components (`nl_classify`, `route_search`, `execute_subqueries`,
  `assess_sufficiency`, `synthesize_answer`).
- One agent-side tool (`research_graph`).
- Three payload types (`research_intent`, `search_result`,
  `route_decision`).
- Seven rules (R0–R6 plus the continuation rule).
- One reference flow config exercising the full chain.
- In-repo smoke test against the deep-research e2e flow.

What does **not** ship in Phase 1:

- Tier 2 (neural) inside `execute_subqueries` — deferred to Phase 2.
- LLM-driven decomp expansion (the `decompose` routing action uses
  template fast-path only in Phase 1; novel-topic LLM decomposition
  is Phase 2).
- Token-budget compression — deferred to Phase 2.
- Standalone deployable flow via `start_flow` — deferred to Phase 2.
- ops `emit_diagnosis` quality flags — deferred to Phase 2.
- Web search / external-tool fan-out — deferred to Phase 3 with
  ADR-043 detonation pre-flight as hard gate.

## PR sequence

Six PRs, ordered so each is independently mergeable and reviewable.
Each builds on prior PRs but does not require all of them to be
merged before review can start on the next.

| PR | Title (proposed) | Depends on | Approx LOC | Operator decision after |
|---|---|---|---|---|
| 1 | `feat(payloads): research_intent / search_result / route_decision + research_graph agent tool` | — | ~400 | Payload schemas (resolves Open Q 6) |
| 2 | `feat(processor): nl_classify component wrapping graph/query.Classifier` | PR 1 | ~300 | Classifier-output schema |
| 3 | `feat(processor): route_search component (first LLM judgment step)` | PR 1, 2 | ~450 | Router prompt + action enum (resolves Open Q 4) |
| 4 | `feat(processor): execute_subqueries multi-tier fan-out (Tier 0+1)` | PR 1 | ~600 | Evidence schema + ranking |
| 5 | `feat(processor): assess_sufficiency + synthesize_answer components` | PR 1, 4 | ~500 | Assess + synthesize prompts |
| 6 | `feat(research-graph): R0-R6 rule chain + reference flow config + smoke test` | PR 1–5 | ~400 | End-to-end validation (resolves Open Q 1, 2, 3) |

Total: ~2,650 LOC across six PRs. Code-to-test ratio expected ~1:1
per the components' single-purpose nature.

## PR 1 — Payload registry + agent tool

### Scope

The schema foundation. Without firm payload contracts, components in
PRs 2–5 will churn. Lands the three new payload types and the
agent-side tool that produces `research_intent`.

### Deliverables

- **`research_intent`** payload type registered in the payload
  registry per [`/new-payload`](../../.claude/skills/new-payload/SKILL.md):
  - Domain: `research`
  - Category: `intent`
  - Version: `v1`
  - Fields: `topic` (string, required), `hints` (optional map),
    `budget_tokens` (int, default 4000), `max_iterations` (int,
    default 5)
  - `MarshalJSON` wrapping in `BaseMessage` (type alias to avoid
    recursion)
  - `init()` registration in `payload_registry.go`
- **`search_result`** payload type:
  - Domain: `research`
  - Category: `result`
  - Version: `v1`
  - Fields: `evidence` (array), `synthesis` (string), `decomp_trace`
    (object), `tokens_used` (int), `iterations` (int)
- **`route_decision`** payload type:
  - Domain: `research`
  - Category: `route_decision`
  - Version: `v1`
  - Fields: `action` (enum: `synthesize_directly` |
    `retighten` | `walk_seeds` | `decompose`), `args` (action-specific
    map), `rationale` (string, for trajectory review)
- **`research_graph(topic, hints?)`** agent tool:
  - Registered in `agentic-tools` registry
  - Creates a research-pipeline loop entity in AGENT_LOOPS KV
    (role = `research_pipeline`)
  - Emits `research.requested.{loop_id}` triple with research_intent
    payload via ContentStorable
  - Terminates parent's current iteration (continuation rule
    resumes with search_result on a later iteration)
  - Uses `TryLoopExecutionEntityID` per
    `feedback_try_loop_entity_id_for_runtime`

### Tests

- **Round-trip production-decoder test** per
  `feedback_production_decoder_round_trip_required` for each of the
  three payload types. Use `payloadbuiltins.NewTestDecoder`, not an
  anonymous shape-cast.
- **JSON round-trip test** per
  `feedback_polymorphic_config_needs_json_roundtrip_test` for each
  type (operator-reachable schemas).
- **Tool dispatch test** verifying `research_graph` correctly emits
  the triple and creates the loop entity.
- **Schema validation test** for the `route_decision.action` enum.

### Success criteria

- All three payload types decode via production decoder with
  identical shape to source.
- Agent tool registration visible in flow-discovery output.
- `task lint && go test -race ./...` green.
- Schema regeneration (`task schema:generate`) produces no
  uncommitted diff per CI requirement.

### Open questions resolved

- **Open Q 6 (strict tool calling, ADR-035 interaction).** All three
  payload schemas registered with strict-mode validation; the
  router's `action` enum is enforced at decode.

### Risks

- The `hints` map shape is operator-facing — getting it wrong
  cascades through later PRs. Mitigation: ship `hints` as
  `map[string]string` (free-form) in Phase 1, with examples in the
  ADR. Phase 2 may add typed hint subtypes once trajectory data shows
  what hints operators actually pass.

## PR 2 — `nl_classify` component

### Scope

Thinnest component. Wraps the existing `graph/query.Classifier`
(keyword + embedding + LLM variants composable via
`classifier_chain.go`). Validates the component pattern + rule
plumbing with the cheapest possible stage before any new LLM work.

### Deliverables

- New package `processor/research-graph-classify/` (or named per
  team convention — leaving the bikeshed open):
  - Component implements standard `Component` interface (lifecycle,
    ports, schema)
  - Input port: subscribes to `component.nl_classify.{loop_id}`
  - Output: writes `classifier_output` triple to research-pipeline
    loop entity; emits `classify.complete.{loop_id}`
- **Classifier integration**: instantiate `classifier_chain.go` per
  flow config (keyword + optional embedding + optional LLM variant).
  Operator-configurable.
- **Initial graph query execution**: use the
  `SearchOptions` returned by the classifier to do an initial graph
  query and return candidate matches (entities + scores +
  snippets).
- Candidate set written to ObjectStore via `ContentStorable` with
  ref-triple on the loop entity (per ADR-028 "rules carry references
  not content").

### Tests

- Unit tests for each classifier-variant pathway (keyword, embedding,
  LLM).
- Integration test with an in-memory graph fixture: classify a
  topic, verify expected entity hits land in the candidate set.
- Round-trip test confirming the classifier_output payload survives
  production decoder.

### Success criteria

- Component visible in flow-discovery.
- Integration test against deep-research fixture passes.
- LLM-variant test uses the same `model.NewHTTPClient` plumbing as
  beta.43 unified client invariant.
- Honors the timeout discipline from PR #108 (default 30s; operator
  override via `capability.timeout`).

### Open questions resolved

- None directly. Confirms the component-wraps-existing-primitive
  pattern works for the simplest case.

### Risks

- The existing `graph/query.Classifier` returns `*SearchOptions`,
  not search results. PR 2 needs to actually *execute* the resulting
  query. The query execution path through existing gateways must
  match what `execute_subqueries` (PR 4) will use to keep the
  evidence schema consistent.

## PR 3 — `route_search` component

### Scope

The first dedicated LLM judgment step. Examines classifier output and
emits one of four routing decisions. This is the load-bearing piece of
the chain's "not just a better classifier" argument.

### Deliverables

- New component `processor/research-graph-route/`.
- LLM-wrapping component using `model.NewHTTPClient` per beta.43
  unified client invariant.
- **Prompt template** specialized for the routing task. Initial draft
  in PR description for review:
  - Input: research_intent (topic + hints) + classifier_output
    (candidates with entities, scores, snippets)
  - Output: route_decision (action ∈ enum + args + rationale)
  - Structured-emit mode per ADR-035 strict tool calling
- **Action arg schemas** per action:
  - `synthesize_directly`: no args (use classifier output as evidence)
  - `retighten`: refined topic / hint adjustments
  - `walk_seeds`: list of seed entity IDs to expand
  - `decompose`: list of typed sub-queries (entity_state /
    predicate_walk / temporal_range / spatial_polygon)
- **Governance integration** per ADR-039: the LLM call goes through
  the same rule-driven governance layer as any other agent LLM call.
- **`route_decision`** authored predicates default to
  `WithRuleOpaque(true)` per
  `feedback_llm_authored_predicates_rule_opaque` — rules should not
  pattern-match on `rationale` text, only on the typed `action` field.

### Tests

- Unit test for each of the four action paths (fixture
  classifier_output → expected action selection). These are
  prompt-quality smoke tests, not regression assertions — the LLM
  may vary, but the action choice should be reasonable.
- Schema test: the LLM emit conforms to `route_decision` schema;
  invalid emits trigger structured-output retry per ADR-035.
- Governance integration test: a `route_search` call goes through
  governance rule evaluation.

### Success criteria

- All four routing actions exercised in tests with frontier-floor
  models (Gemini 3.x Pro, Sonnet 4.6).
- Schema validation catches malformed emits.
- Trajectory data captures `rationale` for operator review.

### Open questions resolved

- **Open Q 4 (router action enum vs free-form emit).** Phase 1
  commits to the four-action enum. If trajectory data shows missing
  actions (e.g., "give up gracefully"), Phase 2 may add them. Phase 1
  ships with the enum strict.

### Risks

- **Prompt quality is load-bearing.** A poor routing prompt sends
  topics to wrong actions and either wastes work (decompose when
  walk_seeds would have sufficed) or misses opportunities
  (synthesize_directly when more evidence was needed). Mitigation:
  iterate the prompt with operator trajectory review in Phase 2;
  Phase 1 ships with a working draft + room for tuning.
- **Frontier-model dependency.** Router decisions on small models
  (Qwen 7B, DeepSeek 7B) may be unreliable per
  `feedback_frontier_floor_changes_role_split_calculus`. Phase 1
  documents frontier-floor as a soft requirement for production use;
  small-model failure paths fall back to `decompose` as a safe
  default.

## PR 4 — `execute_subqueries` component

### Scope

The code-heavy stage. Multi-tier fan-out, score normalization,
dedup, ranking, budget enforcement. No LLM work in this PR.

### Deliverables

- New component `processor/research-graph-execute/`.
- **Tier 0 (rules / predicate queries)** via existing GraphQL gateway.
- **Tier 1 (BM25)** via existing `graph-index` BM25 surface.
- **Parallel fan-out**: sub-queries execute concurrently with
  `errgroup` or equivalent.
- **Dedup**: same entity ID across tiers collapsed; same
  ObjectStore ref collapsed.
- **Score normalization**: per-tier ordering with tie-break by
  recency (initial proposal; learned ranker deferred to Phase 2).
- **Budget enforcement**: drop lowest-scoring evidence until under
  the `budget_tokens` cap from the intent payload.
- **Provenance preserved**: every result carries `tier` + `source`
  + an entity ID or ObjectStore ref the agent can quote back.
- Accepts either `walk_seeds` args (entity list to expand) or
  `decompose` args (typed sub-query list) — same component, two input
  shapes.

### Tests

- Unit tests for ranking, dedup, budget enforcement.
- Integration test with an in-memory graph + BM25 fixture:
  - `walk_seeds` path: given seed entities, return evidence from
    multi-hop expansion.
  - `decompose` path: given typed sub-queries, return evidence from
    parallel execution.
- Race-condition test with `-race` flag per
  `feedback_framework_change_needs_branch_integration_sweep`.
- Round-trip test for the evidence array shape.

### Success criteria

- P95 latency under target SLO (TBD in Phase 1 — propose 5s for
  Tier 0+1 against typical fixture).
- Race detector clean.
- Provenance refs validate against actual ObjectStore / entity-graph
  state (no fabricated refs).

### Open questions resolved

- None directly. Phase 1 ships with per-tier ordering + recency
  tie-break; learned ranker is Phase 2.

### Risks

- **Tier coverage gaps.** Phase 1 ships Tier 0 + Tier 1; Tier 2
  (neural) is Phase 2. If neural inference would have answered a
  topic, Phase 1 misses it. Mitigation: documented in Phase 1
  release notes; operators with neural inference can request Phase 2
  prioritization.
- **Sub-query schema drift.** The four sub-query types
  (entity_state / predicate_walk / temporal_range / spatial_polygon)
  must match what `route_search`'s `decompose` action emits. Schema
  contract enforced by PR 1's payload registration.

## PR 5 — `assess_sufficiency` + `synthesize_answer` components

### Scope

The final two LLM-wrapping components. Bundled because they share
LLM-call infrastructure and are paired in the rule chain (synthesize
fires when assess says sufficient or refine cap is hit).

### Deliverables

- New components `processor/research-graph-assess/` and
  `processor/research-graph-synthesize/`.
- Both LLM-wrap via `model.NewHTTPClient`.
- **`assess_sufficiency`** input: research_intent + evidence array.
  Output: `{sufficient: bool, refined_queries?: []}`.
- **`synthesize_answer`** input: research_intent + evidence array.
  Output: `search_result` with synthesis text + evidence refs +
  decomp_trace.
- Shared helper package (`processor/research-graph-llmwrap/` or
  inline depending on scope) for the structured-emit boilerplate.
- **Refs preserved verbatim**: synthesis must quote ObjectStore refs
  exactly as they appear in evidence; quote-back validation enforces
  no fabrication.
- Authored predicates on synthesis output default to
  `WithRuleOpaque(true)`.

### Tests

- Unit tests for sufficient=true / sufficient=false / refined_queries
  paths in assess.
- Unit test for synthesis quote-back validation: synthesis cannot
  reference an ObjectStore ref that wasn't in the input evidence.
- Schema tests for both emit payloads.
- Integration test: full assess → synthesize sequence against
  evidence fixture.

### Success criteria

- Assess decisions correlate with operator's manual judgment on a
  small fixture set.
- Synthesis preserves all refs without fabrication.
- Round-trip tests green.

### Open questions resolved

- None directly. Calibration of assess vs refine boundary comes from
  Phase 2 trajectory data.

### Risks

- **Synthesis hallucination.** Even with quote-back validation, the
  prose around quoted refs may misrepresent them. Mitigation:
  trajectory review in Phase 2; consider adding a sentence-level
  citation requirement (each claim cites a ref) if Phase 1 shows
  drift.
- **Assess over-eagerness.** If assess says sufficient too easily,
  the refine loop doesn't engage and answers are thin. If assess is
  too strict, refine runs to its `MaxIterations=5` cap on every
  query and latency suffers. Mitigation: Phase 1 ships with a
  middle-ground prompt; Phase 2 tunes from trajectories.

## PR 6 — Rule chain + reference flow config + smoke test

### Scope

The wiring PR. Brings everything together into a runnable reference
flow with the seven rules in place. This is the integration moment
and the largest risk PR — but every component is already merged and
tested.

### Deliverables

- **`configs/flows/research-graph-pipeline.yaml`** reference flow:
  - All five components instantiated.
  - All seven rules (R0–R6 plus the continuation rule R-cont).
  - `entity_watch_buckets` config for AGENT_LOOPS patterns.
  - Per-action `MaxIterations`: 2 on R2's retighten branch, 5 on
    R4's refine branch.
- **Rule definitions** in JSON or YAML per existing rule-config
  format. All seven rules use existing action types (`publish`,
  `publish_agent`) — no new action types.
- **Continuation rule** (R-cont) that resumes the parent loop with
  the search_result payload ref.
- **Smoke test** in `e2e/research-graph/` (or appropriate path):
  - Spins up the reference flow + a minimal parent flow.
  - Parent calls `research_graph` with a fixture topic.
  - Verifies search_result lands in parent's next iteration.
  - Exercises all four routing actions across fixture variations.
- **Updated documentation**:
  - ADR-045 status section updated to "Phase 1 implementation
    landed; Phase 2 gated on operator validation."
  - doc 14 worked example unchanged (already matches).
  - This plan doc updated with shipped tag.

### Tests

- Smoke test exercising the full chain.
- Per-action smoke variants (one fixture per routing action).
- Pre-tag e2e green per
  `feedback_e2e_required_for_breaking_changes`.

### Success criteria

- Smoke test green end-to-end.
- `task e2e:agentic` passes including the new fixture.
- No new KV buckets registered (per ADR-045 discipline). Confirm with
  a grep audit before merge.
- Documentation updates land in the same PR.

### Open questions resolved

- **Open Q 1 (cache key for hot intents).** Phase 1 implementation
  defines the cache predicate (proposed: wall-clock TTL on the loop
  entity, invalidated on entity revisions for referenced entities).
- **Open Q 2 (continuation deadline + fallback).** Phase 1 wires
  the deadline rule + degraded `search_result.error` schema.
- **Open Q 3 (retighten vs refine cap calibration).** Phase 1 ships
  with defaults (2 and 5); calibration is a Phase 2 trajectory-data
  exercise.

### Risks

- **Integration surface.** Five components, seven rules, payload
  contracts — drift between PRs 1–5 surfaces here. Mitigation:
  contract tests per PR keep schemas firm; PR 6 reviewer focus is on
  wiring, not component internals.
- **Smoke test fragility.** New e2e fixtures historically flake
  (per `project_websocket_flake_diagnosis` and
  `project_agentic_loop_ctx_cancel_flake`). Mitigation: design the
  smoke to use explicit synchronization, not arbitrary sleeps;
  follow `task e2e:check-ports` discipline.

## Cross-cutting concerns

### CI requirements (per CLAUDE.md)

Every PR must pass before merge:

- `task lint` — `go vet`, `go fmt`, `revive` clean (warnings = fail).
- `go test -race ./...` — unit + integration tests with race
  detector.
- `task schema:generate` — no uncommitted diff in `schemas/` or
  `specs/`.
- `go test ./test/contract/...` — contract tests green.
- For PRs touching `pkg/`, `natsclient/`, or vocabulary: branch
  integration sweep per
  `feedback_framework_change_needs_branch_integration_sweep`.

### Build tag sweep before tagging

Per `feedback_pre_tag_sweep_includes_build_tags`: every tag eligible
for release must pass `go vet -tags=integration` AND `-tags=live_llm`.
Phase 1's terminal merge (PR 6) is the tag-eligible point.

### Detonation pre-flight (ADR-043)

Phase 1 is **out of scope** for detonation pre-flight integration.
The chain reads from graph + ObjectStore only; web sources arrive in
Phase 3 with detonation as a hard gate. PR 4 (`execute_subqueries`)
should leave a clean integration seam for Phase 3 to add the
detonator call without restructuring.

### Governance (ADR-039)

All three LLM-wrapping components (`route_search`,
`assess_sufficiency`, `synthesize_answer`) route through the
rule-driven governance layer. Per-loop vs per-role boundaries per
`feedback_per_loop_vs_per_role_safety` apply.

### Strict tool calling (ADR-035)

All structured emits from the LLM-wrapping components use strict-mode
output validation. Schema violations trigger retry; persistent
violations surface as `route_search.error` / `assess.error` /
`synthesize.error` triples for ops review.

### NATS publishes use payload registry

Per `feedback_nats_publishes_use_payload_registry`: every NATS publish
inside the chain wraps in BaseMessage via the payload registry, even
for known-consumer paths. No raw-shape publishes.

## Out of scope for Phase 1 (Phase 2+ candidates)

Items deferred to Phase 2 (gated on Phase 1 operator validation):

- Tier 2 (neural) integration in `execute_subqueries`.
- LLM-driven decompose for novel topics in `route_search`'s
  `decompose` action (Phase 1 uses template fast-path only).
- Token-budget compression via small-LLM emit (refs preserved).
- Per-parent-role enablement matrix (researcher, curator, ops with
  different budgets).
- Standalone deployable flow spawned via `start_flow`
  (ADR-042 Phase 4).
- ops `emit_diagnosis` flags for `route_search` / `assess` /
  `synthesize` quality (e.g., "router chose `decompose` when
  `walk_seeds` would have sufficed").
- Learned ranker for cross-tier score normalization (Phase 1 uses
  per-tier ordering + recency tie-break).

Items deferred to Phase 3 (gated on Phase 2 + external use case):

- Web search / external-tool fan-out inside `execute_subqueries`
  **with ADR-043 detonation pre-flight as hard gate** (not optional).
- Cross-source dedup and provenance harmonization.

Items deferred to Phase 4 (research):

- Cross-corpus research-to-research escalation.
- Real-time evidence freshening during a single research call.
- Multi-modal evidence sources.

## Operator validation checkpoints

The Phase 2 gate is operator validation of Phase 1 in production.
Concrete checkpoints:

1. **Schema durability**: do the three payload schemas survive a
   week of operator trajectories without churn requests? If churn
   surfaces, Phase 2 starts with a schema migration PR.
2. **Routing distribution**: which of the four `route_search`
   actions does the router favor in practice? If the distribution
   is wildly skewed (e.g., 95% `decompose`), the prompt needs
   tuning before Phase 2 expansion.
3. **Refine-loop saturation**: how often does the refine loop hit
   `MaxIterations=5` vs terminate on `sufficient=true`? If
   saturation is high, either Tier 0+1 coverage is insufficient
   (argues for Phase 2's Tier 2 priority) or assess is too strict
   (argues for prompt tuning).
4. **Latency P95**: is P95 under the target SLO from PR 4? If not,
   parallel fan-out in `execute_subqueries` needs hardening.
5. **Synthesis fidelity**: are there fabricated refs in synthesis
   output? Quote-back validation should catch them; if it doesn't,
   the validator needs strengthening before Phase 2.

Phase 2 work begins when these five checkpoints have a green
operator signal.

## References

- [ADR-045: Graph Search Decomp+Fusion via Rule-Chain +
  Components](../adr/045-graph-search-rule-chain.md)
- [docs/concepts/14: Orchestration Layers — How We Do Workflows in
  semstreams](../concepts/14-orchestration-layers.md)
- [ADR-028: Agentic Orchestration Architecture](../adr/028-orchestration-architecture.md)
- [ADR-035: Strict Tool Calling](../adr/035-strict-tool-calling.md)
- [ADR-039: Tool-call Governance Rule-Driven](../adr/039-tool-call-governance-rule-driven.md)
- [ADR-041: Unified Condition Evaluator](../adr/041-unified-condition-evaluator.md)
- [ADR-042: OASF Taxonomy Adoption (Phase 4 deploy_flow agent-tools)](../adr/042-oasf-taxonomy-adoption.md)
- [ADR-043 Rollout Playbook](20-adr043-rollout.md) — adjacent ops
  playbook format reference
- [`/new-payload`](../../.claude/skills/new-payload/SKILL.md) — payload
  registration checklist
- [`/orchestration-check`](../../.claude/skills/orchestration-check/SKILL.md)
  — pattern decision skill
- Memory: `feedback_nats_publishes_use_payload_registry`
- Memory: `feedback_production_decoder_round_trip_required`
- Memory: `feedback_polymorphic_config_needs_json_roundtrip_test`
- Memory: `feedback_try_loop_entity_id_for_runtime`
- Memory: `feedback_llm_authored_predicates_rule_opaque`
- Memory: `feedback_framework_change_needs_branch_integration_sweep`
- Memory: `feedback_pre_tag_sweep_includes_build_tags`
- Memory: `feedback_e2e_required_for_breaking_changes`
- Memory: `feedback_per_loop_vs_per_role_safety`
- Memory: `feedback_frontier_floor_changes_role_split_calculus`
