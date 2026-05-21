# ADR-045: Graph Search Decomp+Fusion via Rule-Chain + Components

## Status

**Proposed — 2026-05-19; amended 2026-05-21.** Doc-only ADR; no
implementation in this tag. Independent of ADR-039 (governance),
ADR-042 (publisher-mode), ADR-043 (detonation corpus), ADR-044 (CS API
framework split). Builds on existing primitives only: rule engine
(`processor/rule/` with per-action `MaxIterations`), component
framework, AGENT_LOOPS KV, ObjectStore via `ContentStorable`, triples
on entities, **and the existing `graph/query.Classifier`** (regex
keyword + embedding + LLM variants composable via
`classifier_chain.go`). Does **not** introduce new KV buckets or new
orchestration primitives.

**2026-05-21 amendment** (rolled into this revision; original v1 was
merged in PR #109): repositioned the first LLM step from
intent-decomposition to **classify-and-route over the existing
classifier's output**. Two driving notes:

1. `graph/query.Classifier` already exists (regex/embedding/LLM
   variants) and is the natural first hit for any natural-language
   topic. The chain should reuse it, not duplicate it.
2. The v1 intent payload required fields (entity IDs, predicates)
   that the caller realistically only knows *after* a graph search
   — circular. v2 takes only what the caller has: a topic + optional
   hints.

The shift strengthens the trained-ergonomics argument: "look at a
classifier's hits and decide what to do next" is one of the most
heavily trained LLM behaviors (humans-using-search-engines is well
represented in training data). Cold-start query planning is much
rarer.

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
BM25 / neural), multi-gateway query access (GraphQL, MCP, NATS
Direct), and an existing classifier-driven NL query path
(`graph/query.Classifier`, with keyword/embedding/LLM variants). The
substrate for unified retrieval exists; the unresolved question is
**where the decomp+fusion step lives** between those primitives and
the agent's prompt.

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

What the LLM *is* trained for: read text, summarize text, examine a
list of search results and decide what to do next, judge relevance,
compose answers from sources. What it is *not* trained for:
orchestrate a sequence of typed graph queries from a cold start,
normalize scores across retrieval tiers, dedup multi-hop walk
results, manage iteration budgets.

The architecture should cut along this seam: **code does what code is
good at, LLM does what LLM is good at**, and the LLM is invoked in
bounded structured-emit calls at the judgment points where its
language strengths apply — including, critically, the one where it
*examines a concrete classifier hit set and decides what to do next*.
Not as a multi-turn agent loop driving graph tools it wasn't trained
to drive.

## Decision

Graph search decomp+fusion is implemented as a **coordinated rule
chain orchestrating typed components**, following the canonical
semstreams multi-step pattern documented in
[docs/concepts/14-orchestration-layers.md](../concepts/14-orchestration-layers.md).
No new orchestration primitive, no new state bucket, no persona dir.

### Parent's surface

One new tool exposed in the parent's allowlist:

```
research_graph(topic, hints?) → async search_result
```

Calling it emits `research.requested.{loop_id}` (a triple +
ContentStorable ref for the intent payload) and terminates the
parent's current iteration. The result arrives on a subsequent
iteration via the standard continuation rule pattern (same shape as
any agent loop completion).

The intent payload contains only fields the **caller actually
knows** — no entity IDs, no predicate names, no tier mix, none of
which a parent agent could supply without doing a graph search
first:

```json
{
  "topic": "drone hover anomalies",
  "hints": {
    "entity_kind": "drone",        // optional, free-form
    "domain": "robotics",          // optional
    "recency": "last_24h"          // optional, classifier-parseable
  },
  "budget_tokens": 4000,
  "max_iterations": 5
}
```

Entity IDs, predicates, and tier selection are **outputs** of the
chain (returned to the parent inside `search_result`), not inputs.

### Internal architecture

```
Parent → research_graph(topic, hints?)  → terminates iteration
            │
            ▼  R0 fires on research.requested.*
[component] nl_classify
            │  wraps existing graph/query.Classifier (regex + embedding + LLM
            │  chain via classifier_chain.go); produces initial candidate
            │  matches with entities, scores, snippets
            ▼  R1 fires on classify.complete.*
[component] route_search
            │  FIRST LLM JUDGMENT STEP — examines classifier output, emits
            │  one of four routing decisions:
            │    (a) "classification sufficient"  → short-circuit to synthesize
            │    (b) "retighten"                  → refined nl_classify call
            │                                       (max_iterations on this branch)
            │    (c) "walk_seeds"                 → execute_subqueries with the
            │                                       entities the classifier surfaced
            │    (d) "decompose"                  → execute_subqueries with a
            │                                       typed sub-query list
            ▼  R2 conditional branch on route.decision:
   ┌────────────────────┬──────────────────────────┬───────────────────┐
   ▼ (a)                ▼ (b)                      ▼ (c)/(d)
[synthesize_answer]  [nl_classify w/refined]    [execute_subqueries]
   │                    │                          │
   │                    │ (loops back to R1)       ▼  R3 fires on execute.complete.*
   │                    │                       [assess_sufficiency]
   │                    │                          │  LLM judges relevance, emits
   │                    │                          │  {sufficient, refined_queries?}
   │                    │                          ▼  R4 conditional refine OR synthesize
   │                    │                       ┌──┴──┐
   │                    │                       ▼     ▼
   │                    │       [execute_subqueries] [synthesize_answer]
   │                    │           (max_iter=5)
   │                    │                       │     │
   ▼                    ▼                       ▼     ▼
   └────────────────────┴───────────────────────┴─────┘
            ▼  R5 emits search_result terminal, fires R-cont
[continuation rule]
            ▼
Parent resumes with search_result evidence in prompt
```

### Components (five small additions, each a single-purpose unit)

| Component | Type | Body |
|---|---|---|
| `nl_classify` | Wraps existing primitive | Calls `graph/query.Classifier.ClassifyQuery(topic)` (using `classifier_chain.go` to compose keyword + embedding + LLM variants per operator config) and executes the resulting `SearchOptions` against the graph. Returns initial candidate set: entities + scores + snippets. No new LLM calls beyond what the existing classifier already does. |
| `route_search` | LLM-wrapping | One bounded structured-emit call: `(topic, classifier_output) → {action, args}`. Action ∈ {`synthesize_directly`, `retighten`, `walk_seeds`, `decompose`}. This is the first dedicated LLM judgment in the chain, and it is the one most aligned with trained ergonomics (read results, decide next move). |
| `execute_subqueries` | Code | Parallel multi-tier fan-out via existing GraphQL/MCP/NATS-Direct gateways. Accepts either a seed-entity list (from `walk_seeds`) or a typed sub-query list (from `decompose`). Score normalization, dedup, ranking. Budget enforcement. Writes evidence to ObjectStore; ref-triples to the research-loop entity. |
| `assess_sufficiency` | LLM-wrapping | One bounded structured-emit call: `(topic, evidence) → {sufficient: bool, refined_queries?: []}`. The "refine or finalize" decision after multi-hop execution. |
| `synthesize_answer` | LLM-wrapping | One bounded structured-emit call: `(topic, evidence) → synthesis`. Refs preserved verbatim. Reached either via short-circuit from `route_search` or via the standard `assess → synthesize` path. |

The three LLM-wrapping components (`route_search`,
`assess_sufficiency`, `synthesize_answer`) share underlying
infrastructure (`model.NewHTTPClient`, beta.43 unified LLM HTTP
client invariant). Each has its own prompt template specialized for
its task — not a free-form agent persona, structured input/output
schemas. No persona dir.

### State storage (uses existing buckets only — no new plumbing)

| State | Where it lives | Reuses |
|---|---|---|
| Research operation's identity + iteration counts | AGENT_LOOPS KV entry (role = `research_pipeline`) | Existing bucket; same primitives as any agent loop |
| Original topic + hints + retighten history | Triples on the research-loop entity | `Graphable` interface; standard pattern |
| Classifier candidate set (initial + retightened) | Triples on the loop entity (refs to ObjectStore for snippet bodies) | `ContentStorable`; standard pattern |
| Routing decisions emitted by `route_search` | Triples on the loop entity | `Graphable` interface |
| Multi-hop evidence (potentially bulky) | ObjectStore via `ContentStorable`; ref-triples on loop entity | Existing pattern per ADR-028 ("rules carry references, never content") |
| Assessment result, synthesis | Triples on the loop entity + ObjectStore refs for large payloads | `Graphable` interface |
| Iteration cap for refine loop | Per-action `MaxIterations=5` on R4 | Existing rule-engine primitive |
| Iteration cap for retighten loop | Per-action `MaxIterations=2` on R2's retighten branch | Existing rule-engine primitive |
| Search result returned to parent | ObjectStore object + ref-triple; continuation rule fires parent | Same pattern as any agent loop completion |

**No new KV buckets. No new state machines. No new orchestration
primitive.** Every load-bearing primitive cited is already in `main`.

### Rule chain

Six rules plus the continuation rule:

```yaml
# R0: kick off the chain — run the existing classifier on the topic
- name: research_classify
  when: kv_write
    bucket: AGENT_LOOPS
    key_pattern: "research.requested.*"
  actions:
    - type: publish
      subject: "component.nl_classify.{loop_id}"

# R1: after classification, route on results
- name: research_route
  when: kv_write
    bucket: AGENT_LOOPS
    key_pattern: "classify.complete.*"
  actions:
    - type: publish
      subject: "component.route_search.{loop_id}"

# R2: route_search emits one of four decisions; dispatch accordingly
- name: research_dispatch
  when: kv_write
    bucket: AGENT_LOOPS
    key_pattern: "route.complete.*"
  actions:
    - type: publish
      subject: "component.synthesize_answer.{loop_id}"
      when: '$state.route.action == "synthesize_directly"'
    - type: publish
      subject: "component.nl_classify.{loop_id}"
      when: '$state.route.action == "retighten"'
      max_iterations: 2          # cap retighten loop separately from refine loop
    - type: publish
      subject: "component.execute_subqueries.{loop_id}"
      when: '$state.route.action == "walk_seeds" OR $state.route.action == "decompose"'

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
      when: '$state.assess.sufficient == false'
      max_iterations: 5         # per-action cap is the refine loop limit
    - type: publish
      subject: "component.synthesize_answer.{loop_id}"
      when: '$state.assess.sufficient == true OR $state.iterations >= 5'

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

All rules use existing JSON rule definitions and existing action
types (`publish`, `publish_agent`). The `when` clauses use the
unified condition evaluator (ADR-041). `MaxIterations` is the
existing per-action firing cap. **Two independent loop caps** —
retighten (R2, default 2) and refine (R4, default 5) — because
they bound different failure modes.

### Why this is not just-a-better-classifier

The chain leads with `nl_classify` (existing primitive) but the
load-bearing work happens in **`route_search`**, which is the LLM
judgment step that turns a classifier hit set into one of four
typed actions. Without it, this *would* be just-a-better-classifier
or just-a-better-GraphQL. With it:

- The chain can **short-circuit** when classification is sufficient
  — many parent topics will not need multi-hop work, and the chain
  doesn't pay for it.
- The chain can **retighten** when classification produced noisy or
  empty hits — a refined `nl_classify` re-run rather than a
  brute-force multi-hop walk.
- The chain can **walk seeds** when the classifier surfaced
  promising entities but the parent needs their context — multi-hop
  expansion from specific seed entities is structurally different
  from a topic-wide decomp.
- The chain can **decompose** when the topic genuinely needs
  multiple typed sub-queries — the original v1 path, but now used
  only when the classifier shows it's necessary, not by default.

Each of these is a judgment the classifier alone cannot make and a
free-form ReAct loop cannot make fluently. `route_search` is the
single LLM call where the model does exactly what it was trained for:
read search results, decide next action.

`assess_sufficiency` is the second LLM judgment (after multi-hop
execution): did we get what we need, or do we refine? `synthesize_answer`
is the third: compose the answer with provenance. Three LLM judgment
calls, each at a point well-aligned with trained ergonomics, none of
which asks the LLM to drive a tool loop.

### Why this avoids the semspec trap

Every load-bearing primitive is already in the framework:

- Coordinated rule chain → existing `processor/rule/`
- Per-action iteration cap → existing `MaxIterations`
- Conditional branching → existing action `when` clauses (ADR-041
  unified evaluator)
- NL classification → existing `graph/query.Classifier` +
  `classifier_chain.go`
- State on entity → existing `Graphable` interface + triples
- Bulky payload handling → existing `ContentStorable` + ObjectStore
- Continuation back to parent → existing rule + `publish_agent`
  pattern

The five new components are normal components — same lifecycle, same
port discipline, same flow-discovery, same flowgraph validation as
every other component. No bypass paths, no parallel state machine,
no app-side workflow engine.

### Flow packaging (two valid paths, decided at implementation)

Two equally valid ways to package the rule chain:

1. **Inline rules in the parent's flow config** (Phase 1 recommended).
   Add R0–R6 + five components to whichever flow needs research.
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
  is invoked at three language tasks it's trained for (examine
  classifier hits and route, judge multi-hop evidence sufficiency,
  synthesize); code does the multi-tier orchestration the LLM isn't
  trained to drive.
- **Reuses the existing `graph/query.Classifier` instead of
  duplicating it.** Phase 1 ships without re-implementing
  NL-to-search-options logic — that work is years old and well-tested.
- **Caller's surface matches what the caller actually knows.** Topic
  + hints, no required entity IDs or predicate names. No circular
  dependency on a prior graph search.
- **Short-circuit path.** Many parent topics are answerable from the
  classifier's output alone; the chain doesn't pay for multi-hop
  work when it isn't needed. Full chain is the expensive path, not
  the default.
- **Dodges the graph-not-for-reasoning failure mode.** The agent
  never decides to navigate the graph — it just gets fused evidence
  in the next loop's prompt. Same injection-side pattern that worked
  for lineage triples in beta.51.
- **Dodges role-split orchestration cost.** No persona dir, no chain
  role, no per-role rule wiring beyond R0–R6.
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
- **Operator-configurable.** Per-role enable, budget caps, classifier
  variant choice (keyword/embedding/LLM via existing
  `classifier_chain.go` config) are all flow config — not code.

### Costs

- **Multi-stage latency.** Up to five rule fires + three LLM calls
  + N code stages per research call on the full path. Short-circuit
  path is much cheaper (classifier + route_search only).
  Mitigation: parallel fan-out inside `execute_subqueries`; KV cache
  for hot intents; P95 SLO.
- **Parent residual crimp.** Parent still chooses whether to call
  `research_graph`. Smaller surface than multi-tool but not zero.
  Mitigation: one-line persona hint ("when in doubt, delegate via
  `research_graph`"); ops `emit_diagnosis` flags missed delegations.
- **Five new components.** Maintenance surface. Mitigation: each is
  single-purpose, small; the three LLM-wrapping ones share
  infrastructure; `nl_classify` is a thin wrapper over an existing
  primitive.
- **Router can hallucinate the routing decision.** Mitigation:
  structured emit + schema-validated `action ∈ enum`; ops
  observability of router decisions.
- **Synthesis can fabricate refs.** Mitigation: refs must validate
  against actual evidence in ObjectStore; schema-level enforcement,
  not prompt-level.
- **Two independent iteration caps to tune.** Retighten (R2) and
  refine (R4) defaults will need calibration from Phase 1
  trajectories. Mitigation: sensible starting values (2 and 5);
  operator-configurable.
- **Per-role configuration surface grows.** Each parent role gets
  `research_graph` enable + budget caps + classifier-variant
  selection. Mitigation: sensible defaults; opt-in.

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
multi-hop graph-walk + fuse-results trajectories. Asking the LLM to
drive a graph tool loop is the failure mode, not the constraint on
alternatives. Cleaner to invoke the LLM at structured language
tasks (route classifier output, assess multi-hop evidence,
synthesize) than to ask it to drive a loop it wasn't trained for.

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
parent calls `research_graph(topic, hints?)`, components emit
`search_result(evidence)`. Both are auditable artifacts.

### F. Prompt-side encouragement of existing graph tools

"You should use `search_graph` as a first step for any question
about entities."

**Rejected — already tried.** semspec's recovery diagnosis quotes
the agent ignoring `graph_search` results even with explicit prompt
encouragement. Persona prompts already say "Try FIRST for project
lookups." Behavior unchanged across three instrumented incidents.

### G. Decomposition-first chain (v1 of this ADR, superseded)

The v1 design used a `decompose_intent` LLM step as the chain
opener, asking the LLM to expand a structured intent into typed
sub-queries from a cold start.

**Rejected in v2.** Two issues:
- The intent payload required entity IDs and predicates the caller
  realistically doesn't know without a prior graph search —
  circular.
- Cold-start query planning ("decompose this topic into typed
  sub-queries") is much rarer in training data than "look at search
  results and decide next move." The classifier-first chain hits
  the trained-ergonomics seam more squarely.

The v2 chain (this revision) reuses `decompose` as one of four
routing actions inside `route_search`, invoked only when the
classifier output indicates the topic genuinely needs typed
sub-query expansion.

### H. Status quo (do nothing)

Continue with direct-tool exposure; treat failures as prompt tuning.

**Rejected.** Three instrumented incidents across two sister projects
converging on the same shape. Tuning has not moved the floor.

## Phased rollout

**Phase 1** (proposed for next eligible tag — not beta-blocking):

- Five new components: `nl_classify`, `route_search`,
  `execute_subqueries`, `assess_sufficiency`, `synthesize_answer`.
  Each ships with unit tests against the production decoder per
  `feedback_production_decoder_round_trip_required`.
- `nl_classify` integration with `graph/query.Classifier` reuses
  the existing keyword + embedding + LLM chain
  (`classifier_chain.go`); the LLM variant of the classifier has
  been operator-validated as of beta.76 (timeout bump to 30s in
  PR #108 hardened it for cold-start hardware).
- One new parent-side tool: `research_graph(topic, hints?)`
  registered per `/new-payload` discipline.
- Two new payload types: `research_intent` + `search_result`
  registered in the payload registry.
- Six rules (R0–R6) bundled in a reference flow config; parent
  flows opt in by including the rules or by spawning the flow via
  `start_flow` (Phase 2 path).
- `route_search` action enum: `{synthesize_directly, retighten,
  walk_seeds, decompose}`. Phase 1 implements all four; calibration
  of which decisions the router favors comes from operator
  trajectories.
- Tier coverage inside `execute_subqueries`: Tier 0 (rules) + Tier 1
  (BM25). Tier 2 (neural) deferred to Phase 2.
- In-repo smoke test against the deep-research e2e flow.
- Pre-tag e2e green per
  `feedback_e2e_required_for_breaking_changes` (this changes the
  framework-level pattern catalog; touch is contained but the new
  components need flow-discovery exposure).

**Phase 2** (gated on Phase 1 operator validation):

- Tier 2 (neural) added to `execute_subqueries`.
- Token-budget compression via small-LLM emit (refs preserved).
- Per-parent-role enablement (researcher, curator, ops opt in with
  different budgets).
- Deployable as a standalone flow (Phase 2 spawn path via
  `start_flow`).
- ops `emit_diagnosis` flags for `route_search` / `assess` /
  `synthesize` quality (e.g., "router chose `decompose` when
  `walk_seeds` would have sufficed").

**Phase 3** (gated on Phase 2 + external use case):

- Web search and external-tool fan-out added to `execute_subqueries`
  *with detonation pre-flight* (ADR-043) — hard gate, not optional.
- Cross-source dedup and provenance harmonization.

**Phase 4** (deferred — research):

- Cross-corpus research-to-research escalation.
- Real-time evidence freshening during a single research call.
- Multi-modal evidence sources.

## Open questions

1. **Cache key for hot topics.** Same topic + hints from the same
   chain-entity should not re-run the full chain every iteration.
   Invalidation predicate: wall-clock TTL, revision count on
   referenced entities, or both? Composes with the existing
   classifier's own caching (if any). Resolved in Phase 1
   implementation.

2. **Continuation-rule deadline + fallback.** If any stage in the
   chain stalls or fails, the parent must resume with
   `search_result.error` evidence so it can make a degraded
   decision rather than wedge. Deadline value and degraded-result
   schema TBD in Phase 1.

3. **Retighten vs refine cap calibration.** Phase 1 proposes
   `MaxIterations=2` on the retighten branch (R2) and
   `MaxIterations=5` on the refine branch (R4). Are these the right
   defaults? Tuning from Phase 1 trajectories.

4. **Router action enum vs free-form emit.** Phase 1 commits to a
   four-action enum; do we need a fifth "give up" action that
   short-circuits to `search_result.error`, or does the deadline
   fallback (Q2) cover it adequately?

5. **Engine gaps surfaced during implementation.** If implementation
   reveals that the current rule engine cannot express something
   needed (e.g., reading classifier-candidate-count in a `when`
   clause to gate the route decision), the gap gets filed as a
   discrete engine-improvement ticket — not as inline state
   plumbing in the research chain. The semspec trap is explicit
   prior art.

6. **Interaction with strict tool calling (ADR-035).** Both
   `research_intent` and `search_result` payloads need formal
   schema registration per `/new-payload`. Phase 1 includes the
   registry entries + round-trip production-decoder test. The
   router's emit schema (the four-action enum) likewise needs
   strict-mode validation.

7. **Interaction with governance (ADR-039).** The four LLM-wrapping
   components' calls go through the same rule-driven governance
   layer as any other LLM call. Per-loop vs per-role boundaries per
   `feedback_per_loop_vs_per_role_safety` apply.

8. **Detonation corpus coverage.** Phase 1 reads from graph +
   ObjectStore (lower prompt-injection risk than web sources, but
   not zero — BM25 snippets from ingested docs are a possible
   vector that surfaces through the classifier). Phase 3 web
   fan-out *requires* detonation pre-flight integration — track as
   a hard gate.

9. **Parent-side delegation hygiene.** How aggressively to prompt
   the parent toward calling `research_graph`. Initial proposal:
   one-line persona hint; ops flags missed delegations for
   trajectory review.

## References

- [Kumar, "The Agent Search Problem,"
  2026](https://dipkumar.dev/posts/agents/agent-search-problem/)
- [`graph/query.Classifier`](../../graph/query/classifier.go) +
  [classifier_chain.go](../../graph/query/classifier_chain.go) —
  the existing NL classification primitive Phase 1 reuses
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
