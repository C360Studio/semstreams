# ADR-040: Retire Boid Coordination Subsystem; Reintroduce via Stigmergy If Needed

## Status

**Proposed — 2026-05-13.** Retires the `processor/rule/boid/` package,
the agentic-loop boid handler, the `publish_boid_signal` action, the
three rule configs under `configs/rules/boid/`, and the unwired
`test/e2e/scenarios/boids/` scenario. Tag scope: a future beta.72
BREAKING. Forcing function: the beta.71 `executePublishBoidSignal`
Properties-substitution parity fix (PR #66) shipped a behaviour
correction through dead code — no e2e exercises this path, no
production config invokes the action, no external project depends on
it. The subsystem accumulates maintenance tax with no offsetting
production value.

Records the 2026-05-13 research summary so a future reintroduction
starts from learnings, not a blank page. The recommended re-entry
path is **stigmergy** (environment-mediated coordination on KV trails),
not Reynolds-style boids; rationale in §"Why stigmergy if we bring it
back."

## Context

### What the boid subsystem currently is

Approximately 5,168 LOC across:

| Location | Purpose | LOC |
|---|---|---|
| `processor/rule/boid/` | Boid rule types (separation/cohesion/alignment), config, position tracker, providers | 1,902 |
| `processor/agentic-loop/boid_handler.go` (+test) | Signal ingestion, TTL'd SignalStore, context reorder | 1,404 |
| `processor/rule/actions.go` (`executePublishBoidSignal` + ActionTypePublishBoidSignal) | Generic publish action variant for boid signals | ~60 |
| `configs/rules/boid/*.json` | Three example configs (never loaded by any agentic config) | 94 |
| `test/e2e/scenarios/boids/scenario.go` | A/B test harness, not imported by `cmd/e2e/main.go` | 334 |
| `processor/agentic-loop/context_manager.go::ApplyBoidSteering` + `BoidSteeringConfig` | Effect site: reorder graph-entity context slots | ~75 |

Design references **Reynolds' three rules** ([Reynolds 1986](https://www.red3d.com/cwr/boids/))
applied to graph topology rather than Euclidean space:

- **Separation** — k-hop overlap detection via `PivotIndex.IsWithinHops()`;
  emits `AvoidEntities` when agents share neighborhoods
- **Cohesion** — PageRank centrality over reachable candidates within
  `searchRadius := 3` hops; emits `SuggestedFocus` toward high-rank entities
- **Alignment** — most-common relationship predicates among same-role
  peers; emits `AlignWith` to laggards

Signal flow: rule fires on entity state change → builds
`SteeringSignal` → publishes to `agent.boid.<loopID>` → handler stores
with 30s TTL → `ContextManager.ApplyBoidSteering` reorders
`RegionGraphEntities` (prioritized first, avoided last). **Reordering
graph-context prompt slots is the entire enforcement mechanism.** The
LLM is free to ignore.

### Indicators of rushed shipping

- `processor/rule/boid/doc.go:44` references `docs/research/boids-hypothesis.md`
  which **does not exist** in the tree
- `test/e2e/scenarios/boids/scenario.go` exists as a compiling Go
  package but `cmd/e2e/main.go` imports `agentic`, `crud-tools`,
  `deep-research`, `ops`, `throughput` — never `boids`
- No Taskfile target invokes the boid scenario; `task --list | grep -i boid` returns nothing
- Default constants (`searchRadius=3`, `CentralityWeight*0.1` threshold,
  `AlignmentWindow=5`) are unmotivated — no calibration data, no
  tuning guide, no benchmark
- `configs/rules/boid/{separation,cohesion,alignment}.json` are not
  loaded by `configs/agentic.json` or any other production config

### Two boid systems exist, structurally different

semstreams `processor/rule/boid/` is **not** the same system as
semdragon `processor/boidengine/`:

| Aspect | semstreams boid | semdragon boidengine |
|---|---|---|
| Domain | Multi-agent graph exploration | Agent-quest assignment |
| Rules | 3 (sep/coh/align) | 6 (+ hunger/affinity/caution) |
| Trigger | Reactive on entity state change | Batch, every `update_interval_ms` (default 1s) |
| Distance metric | k-hop graph via PivotIndex | Discrete feature scoring (skills/guild/tier) |
| "Centre of mass" | PageRank centrality | Skill-cluster density |
| Output | TTL'd advisory steering on `agent.boid.<loopID>` | Ranked quest suggestions |
| Consumer | `ContextManager` reorders graph context | Autonomy loop claims quests |
| Production status | Inert — no consumers wired | Active — autonomy/questdagexec pipeline |

semdragon's boidengine is **not affected** by this retirement. The
two share only Reynolds' rule names and conceptual inspiration; no
code, no contracts. semdragon's system is a multi-objective
recommender that borrows boid vocabulary for legibility.

### The signal-effect problem

Per [feedback_graph_not_for_agent_reasoning.md](../../memory/feedback_graph_not_for_agent_reasoning.md)
(verified across semspec recovery-agent and semteams smokes #8/#19):
agents largely ignore graph results even when explicitly injected
into context. Since the boid signal's *entire* enforcement is
reordering graph-context prompt slots, the effect on actual agent
behaviour is probably small to zero. No measurement exists today —
the scenario isn't wired into CI — so this is an assertion, not a
benchmark. But the asymmetry is strong enough to question the design
premise even before retirement: signals that only reorder ignored
context have no leverage.

## Why retire now

1. **Maintenance tax with no offsetting production value.** Every
   breaking change (e.g., beta.69 metadata propagation, beta.70 filter
   retirement, beta.71 properties substitution) has to consider the
   boid surface even though no real workload exercises it. Three tags
   in three weeks all paid this cost.

2. **The class of bug we just fixed.** beta.71's
   `executePublishBoidSignal` properties-substitution fix ran through
   dead code — same class as the `config.ExpandEnvWithDefaults` "exported
   helper with no internal callers" bug. The boid subsystem is a
   larger instance of the same pattern: shipped framework code never
   exercised by the framework's own integration tests.

3. **Research-labeled code in production framework.** Package doc
   explicitly calls itself "an experimental implementation for
   validating the hypothesis that explicit workflow choreography can
   be replaced by intent-weighted local rules" (`processor/rule/boid/doc.go:42-44`).
   Research artifacts and production framework code carry incompatible
   stability commitments. The right home for the research is a branch
   or a separate experimental package, not the main framework's
   processor tree.

4. **The rules engine has matured past it.** When boid was built, the
   rules engine couldn't host stateful evaluators with hidden
   accumulators cleanly. Today it has `MatchState`, expression
   conditions, `$message.*`/`$entity.*`/`$state.*` substitution,
   `publish`/`deny`/`approve` actions, and JetStream routing. A
   cleaner boid rebuild — if it returns — fits as standard rules +
   `publish_steering_signal` action, no custom rule type.

## Decision

**Retire the boid subsystem in beta.72 (BREAKING).** Remove:

- `processor/rule/boid/` (entire package)
- `processor/agentic-loop/boid_handler.go` and `boid_handler_test.go`
- `processor/agentic-loop/context_manager.go::BoidSteeringConfig` and
  `ContextManager.ApplyBoidSteering`
- `ActionTypePublishBoidSignal` constant and `executePublishBoidSignal`
  function in `processor/rule/actions.go`
- Boid signal handling in `processor/agentic-loop/component.go`
  (subscription on `agent.boid.>`, position lifecycle hooks)
- `configs/rules/boid/{separation,cohesion,alignment}.json`
- `test/e2e/scenarios/boids/scenario.go` (package)
- `payloadbuiltins/register.go` boid registration
- Boid references in migration docs

**Keep:**

- Nothing. The `AGENT_POSITIONS` KV bucket can also go — it has no
  other consumers. If future multi-agent telemetry needs a position
  primitive, it can re-emerge with explicit scope.

**Out of scope for this retirement:**

- semdragon `processor/boidengine/` (different project, different
  system, active consumers — untouched)
- The general lesson "exported helpers need internal callers or
  contract tests" — captured separately in
  [feedback_grammar_collision_audit_on_new_tokens.md](../../memory/feedback_grammar_collision_audit_on_new_tokens.md)

## Why stigmergy if we bring it back

If a real production workload appears that needs N≥3 agents
coordinating on a shared graph, **start with stigmergy, not boids.**
Rationale:

### Stigmergy aligns with the framework's existing primitives

[CLAUDE.md's KV-twofer principle](../concepts/02-kv-twofer.md) already
makes every KV write a "pheromone": the write IS the event, watchers
sense the trail, restart re-delivers current state. Stigmergy —
environment-mediated coordination via persistent traces ([Nature 2024
"Automatic design of stigmergy-based behaviours"](https://www.nature.com/articles/s44172-024-00175-7))
— is what NATS KV gives us natively. Boid coordination requires
explicit neighbor lookup; stigmergy needs only state-watch + decay,
both of which we already have (KV with TTL is a decaying pheromone).

### Stigmergy outperforms boids on resilience

Stigmergic systems are robust to agent failure — agents leave
indications in the environment that stimulate/inhibit peers' behaviour,
no direct neighbor query required. A dead agent stops reinforcing its
trail; nothing else breaks. Boid systems require an alive
`PositionProvider.ListOthers()` to function — partial failures cascade.

### Stigmergy is the production-deployed pattern

Ant Colony Optimization (ACO) has decades of production deployment in
routing, scheduling, and resource allocation. Reynolds' boids remain
mostly a graphics/animation/research technique. The asymmetry in
real-world track record is large.

### The 2025 LLM-swarm literature picks stigmergy too

[LLM-Powered Swarms: A New Frontier or a Conceptual Stretch? (arxiv 2506.14496)](https://arxiv.org/abs/2506.14496)
measured LLM-driven Boids at **36,000× slowdown** vs classical (10
iterations: 0.0019s classical vs 68.61s with LLM-in-loop). LLM-driven
ACO ran 9.7× slower than classical but converged to *better* optima.
The conclusion: pure LLM swarms are conceptual stretch; hybrid systems
(classical coordination, LLM strategic reasoning) are the productive
niche. Stigmergy plays well with that split because the trail-update
loop runs at framework speed, not LLM speed.

### Stigmergy maps cleanly onto our rules engine

A stigmergic primitive in semstreams looks like:

```
- Agent makes a graph access → writes a TRAIL_<entityID> entry to a
  TTL'd KV bucket (the "pheromone deposit").
- Rule subscribes via KV watch on TRAIL_*; condition matches
  "TTL remaining > threshold" (the trail is "fresh").
- When another agent's loop emits a planned-access proposal (subject
  pattern, ADR-039-style), a rule sees the trail and the proposal,
  emits a `deny`/`publish` verdict or a re-route hint.
```

No custom rule type, no out-of-band accumulators, no
`SetPositionProvider`/`SetCentralityProvider` injection plumbing. The
existing rules engine + KV watcher + `publish` action are sufficient.

### Distance metric becomes pluggable, not architectural

If the stigmergy primitive needs a distance test ("is this entity
'close' to the trail?"), it goes in as a rule expression operator, not
a hardcoded `PivotIndex.IsWithinHops` call. semstreams already has
tier-1 BM25 and tier-2 neural embeddings (CLAUDE.md); the right
default for "semantic closeness" is embedding-distance, and the right
default for "structural reachability" is k-hop. Different rules pick
different operators.

## Lessons retained from the 2026-05-13 boid research

The original research deep-dive surfaced learnings worth keeping even
as the code retires:

### Which rule needs which distance metric

If boid (or its successor) returns, the design instinct from the
research is:

| Rule | Right distance metric | Why |
|---|---|---|
| Separation | k-hop reachability | "Are we working on the same physical neighborhood?" — structural |
| Cohesion | Embedding similarity | "Pull toward semantically related high-value entities" — meaning, not reachability |
| Alignment | Set similarity (Hamming/Jaccard on predicate multisets) | Sparse data; vector machinery overkill |

The current implementation uses k-hop for everything. That's defensible
for separation, wrong for cohesion (centrality can pull toward
structurally central but topically irrelevant nodes), and overengineered
for alignment.

### Use cases that justify multi-agent coordination

In rough order of plausibility for semstreams' positioning:

1. **Multi-agent research/coverage on a document corpus.** Strongest fit.
   Researcher fleet exploring documentation in parallel: separation
   prevents duplicate work, cohesion pulls toward high-PageRank entry
   pages, alignment lets newcomers learn from veterans' traversal
   patterns. This is decades-old in IR ("swarm crawlers").
2. **Distributed code-modification fleet (semteams territory).**
   File-region overlap detection. Currently blocked by ADR-041 role
   compression (4 roles, MVP small-N).
3. **Anomaly investigation triage.** SOC-analyst-swarm precedent;
   narrow audience.
4. **Multi-sensor IoT correlation.** Adjacent to existing semstreams
   pitch but no concrete near-term consumer.

### Where coordination is wrong

- Single-agent flows (today's reality)
- Small-N (≤3 agents) — overhead exceeds benefit
- Deterministic / audit-critical workflows
- Real-time / latency-sensitive paths

### Reading the 2025 literature

- **LLM-Flock (arxiv 2505.06513, May 2025)** — needs classical
  consensus underneath; pure LLM unstable, "collapses to centroid or
  diverges chaotically"
- **LLM-Powered Swarms (arxiv 2506.14496, June 2025)** — 36,000×
  slowdown vs classical Boids; hybrid systems are the only productive
  niche
- **Challenges in LLM Flocking (arxiv 2404.04752, April 2024)** —
  direct LLM application to flocking produces "lack of collective
  awareness"
- **Stigmergy automatic design (Nature 2024)** — production-deployed
  pattern for robot swarms; ant colony precedents
- **Knowledge graph embeddings (Node2Vec, GraphSAGE)** — substrate
  for semantic-distance metrics if we ever need them; not coordination
  algorithms themselves

## Migration / retirement plan

### Tag beta.72 — BREAKING

Single PR removes everything listed under "Decision". Migration impact:

- **External consumers:** none found. Grep of all C360Studio
  repositories for `ActionTypePublishBoidSignal`, `boid.SteeringSignal`,
  `AGENT_POSITIONS`, etc. returns only semstreams internal references
  and semdragon's independent `processor/boidengine/` (different
  package, no shared types).
- **Schemas:** boid payload registrations in `payloadbuiltins/register.go`
  removed; downstream consumers using the registry directly (none
  identified) would see "unknown type domain=boid" decode errors at
  load and must update their configs.
- **Configs:** operators with custom `configs/rules/boid/*.json` files
  (none in the C360Studio org) must remove them before upgrading.
- **Release notes:** loud BREAKING with rationale linking this ADR.

### Memory updates

The existing project memory
[project_boid_subsystem_unused_for_mvp.md](../../memory/project_boid_subsystem_unused_for_mvp.md)
transitions from "open question" to "retired in beta.72 per ADR-040";
update on tag.

## When to reintroduce

Trigger conditions:

1. Concrete production workload identified with ≥3 concurrent agents
   operating on the same graph
2. The cost of duplicate-work or coordination failure in that workload
   is measurable (not aesthetic)
3. Architectural review confirms stigmergy is more expensive to build
   than boid for the specific use case (rare — see "Why stigmergy" above)

If those hold, the reintroduction approach:

- Start with stigmergy (KV-trail + decay + rule-mediated coordination)
- Build incrementally: position-tracking KV first, single rule next,
  prove signal effect with a measurement before adding rule #2
- Cohesion-style "pull toward" gets embedding-distance, not k-hop
- All effect goes through standard rule actions
  (`publish`/`deny`/`update_kv`); no out-of-band messaging
- Custom rule types only as last resort

## Architectural debt acknowledged

This retirement leaves the framework without any built-in multi-agent
coordination primitive. That's a deliberate choice — the previous
primitive was wrong for our communication model and not exercised by
any workload — but it's a real gap. If multi-agent becomes a roadmap
item, the rebuild is non-trivial work even with stigmergy as the
starting point. Document the gap explicitly so future scoping
decisions can account for it.

## Decisions deferred to reintroduction

- Whether `AGENT_POSITIONS` KV bucket has independent value as a
  position-tracking primitive (telemetry, lineage) — out of scope
  for this ADR
- Whether `publish_steering_signal` is the right action name for a
  future stigmergy-or-boid-or-whatever signaling action — defer to
  reintroduction PR
- Vector-distance-as-rule-operator surface — separate ADR if/when
  needed

## Related ADRs and docs

- [ADR-028 Orchestration Architecture](028-orchestration-architecture.md)
  — rules vs workflows vs components boundaries; the basis for
  arguing boid logic belongs in standard rules, not a custom type
- [ADR-031 Time-Trigger Primitive](031-time-trigger-primitive.md) —
  cron rules; precedent for periodic evaluators
- [ADR-032 Policy/Tenancy/Cluster](032-policy-tenancy-cluster.md) —
  `$caller.*` namespace and `deny` action; precedent for migrating
  enforcement into the rules engine
- [ADR-039 Tool-Call Governance Rule-Driven](039-tool-call-governance-rule-driven.md)
  — direct precedent for retiring a parallel subsystem in favour of
  the rules engine
- [docs/concepts/02-kv-twofer.md](../concepts/02-kv-twofer.md) — the
  stigmergic substrate
- [semdragon docs/05-BOIDS.md](https://github.com/C360Studio/semdragons/blob/main/docs/05-BOIDS.md)
  — sibling system; independent, unaffected
- [feedback_grammar_collision_audit_on_new_tokens.md](../../memory/feedback_grammar_collision_audit_on_new_tokens.md)
  — same bug class (exported framework code with no internal callers)

## Sources

- [Reynolds 1986 Boids original](https://www.red3d.com/cwr/boids/)
- [LLM-Flock: Decentralized Multi-Robot Flocking via LLMs (arxiv 2505.06513)](https://arxiv.org/abs/2505.06513)
- [LLM-Powered Swarms: A New Frontier or a Conceptual Stretch? (arxiv 2506.14496)](https://arxiv.org/abs/2506.14496)
- [Challenges Faced by LLMs in Solving Multi-Agent Flocking (arxiv 2404.04752)](https://arxiv.org/html/2404.04752v1)
- [Multi-agent systems powered by LLMs: applications in swarm intelligence (Frontiers AI 2025)](https://www.frontiersin.org/journals/artificial-intelligence/articles/10.3389/frai.2025.1593017/full)
- [Automatic design of stigmergy-based behaviours for robot swarms (Nature Comm Eng 2024)](https://www.nature.com/articles/s44172-024-00175-7)
- [From Pheromones to Policies: RL for Engineered Biological Swarms (arxiv 2509.20095)](https://arxiv.org/html/2509.20095)
- [Revisiting Boids for Emergent Intelligence via Multi-Agent RL (OpenReview)](https://openreview.net/pdf?id=46LJ81Yqm2)
- [Knowledge Graph Embeddings fundamentals (Ontotext)](https://www.ontotext.com/knowledgehub/fundamentals/what-are-knowledge-graph-embeddings/)
- [Node2Vec and GraphSAGE primer](https://mhaske-padmajeet.medium.com/graph-embeddings-node2vec-and-graphsage-812e8f147a32)
