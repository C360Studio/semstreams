# ADR-054: Indexing Eligibility - Graph Visibility Is Storage, Indexing Is Policy

## Status

**Proposed** — 2026-06-10. Not yet implemented or tagged. Derived from the
graph-clustering anomaly-storm investigation (semspec operational issue) and
three independent converging design reviews. Builds on
[ADR-047](047-lifecycle-harness-substrate.md) (Lifecycle harness),
[ADR-049](049-lifecycle-harness-prime-schema-over-entity-states.md) (schema over
`ENTITY_STATES`), and [ADR-045](045-graph-search-rule-chain.md) (agents do not
reliably *choose* graph-search tools → important context is injected, not merely
queryable). Related: [ADR-028](028-orchestration-architecture.md) (ops agent as
the operational-issue-detection path).

This ADR is **additive** to the graph-ingest write path, the `message` behavior
interfaces, and the embedding/clustering consumers. Two breaking-ish surfaces are
called out in Consequences: (1) the eventual flip from lenient to strict indexing
policy, and (2) the two mutation-API content producers that must declare
`content` or lose embedding (a config/code migration, not a wire break).

The **companion cleanup ("Move 1")** — disabling the anomaly engine (the storm
source) while **retaining structural indexing as a core capability**, and fixing
the `similarity_threshold` / `min_core_level` config drift — is independent of
this ADR and ships first as a standalone PR. This ADR is the durable fix that
lets the semantic substrate be re-enabled safely.

> **Framing principle:** *Graph visibility is a storage/query contract; semantic
> indexing is a retrieval policy.* An entity being in the graph (queryable,
> traversable, rule-addressable, replayable) is independent of whether it is
> embedded or clustered. Conflating the two is what turned the vector/community
> layer into a dump-truck lane for ephemeral trace.
>
> **This is budgeting, not data reduction.** The goal is never "filter entities so
> the graph runs on edge hardware." Every entity stays graph-visible — queryable,
> traversable, rule-addressable, replayable — unconditionally and forever. What is
> budgeted is only the *expensive retrieval substrates* (embedding, community), and
> that budgeting MUST be **observable and reversible**. The failure mode this ADR
> exists to prevent is paying for resource constraints with **silent semantic
> blindness**: after the strict flip, a producer that forgets to declare `content`
> quietly stops participating in embedding/search with no error and no signal. The
> Rollout gates (phase 3) exist to make that specific failure impossible to reach
> silently.

## Context

### The triggering incident

graph-clustering's semantic-gap anomaly detector fires one NATS `FindSimilar`
round-trip **per entity, every 30s**, over every entity in the pivot index
(`graph/inference/semantic_gap.go:62` → `:123` enumerates all pivot entities →
`:139` per-entity query; `processor/graph-clustering/similarity.go:66`). In
semspec that set is dominated (94.6%) by ephemeral `agent.agentic-loop.step`
trace entities that have no usable embedding, so graph-embedding rejects ~90.9%
with `"embedding not ready"` (`processor/graph-embedding/query.go:116`). Result:
~3,852–4,671 wasted round-trips/min, plus — at `auto_apply.min_confidence=0.95` —
junk `inferred.semantic.*` edges minted between unrelated trace entities,
actively degrading the graph that semspec's ADR-036 is trying to repair.

### The root cause is not the detector — it is `ENTITY_STATES` doing three jobs

`ENTITY_STATES` is simultaneously: durable graph state, operational trace/audit,
and the semantic-search corpus. Nothing separates them. Both semantic consumers
enumerate the **entire** bucket with no shared eligibility filter:

- `graph-embedding` watches all of `ENTITY_STATES` and embeds anything with text
  content (`processor/graph-embedding/component.go:936` only skips entities with
  *no* text — a `tool_name` title is enough to qualify).
- `graph-clustering` enumerates all entity IDs
  (`processor/graph-clustering/component.go:996` `GetAllEntityIDs`).

So telemetry leaks into the semantic substrate, while genuinely durable
harness/project facts may still not reach agents (ADR-045: context must be
injected). The fix is **not "less graph"** — it is separating *graph-visible*
from *semantic-indexed*.

### There are two write surfaces, and the registry only sees one

The payload registry (`payloadregistry/registry.go`) cleanly catalogs the
**Graphable-via-JetStream** path (graph-ingest `extractEntityFromMessage`,
`processor/graph-ingest/component.go:857`; stamps `MessageType` at `:885`). But a
larger surface enters via the **mutation API**
(`processor/graph-ingest/mutations.go:19-61`:
`graph.mutation.triple.add` / `.add_batch` / `.entity.create_with_triples` /
`.entity.update_with_triples`), which has **no Graphable producer and often no
`message.Type`**. Verified writers:

| Writer | Writes | Likely indexing profile |
|---|---|---|
| `processor/rule/triple_mutator.go:17` | every rule add/remove | `control` |
| `processor/agentic-loop/graph_writer.go:22` | loop state + **trajectory steps** (`:328`) | trace |
| `pkg/lifecycle/graph_emit.go:77-78` | all Participants (agent-run, missions) | `control` |
| `processor/agentic-tools/decide.go:52`, `write_todos.go:26` | coordinator decisions, todos | `control` |
| `processor/agentic-memory/handlers.go` | memory lessons, layer_approved | `content` |
| `processor/research-graph-llmwrap/triplepub.go:42` | LLM-extracted domain facts | `content` |

**The motivating `agent.agentic-loop.step` entities enter via the mutation API,
not the Graphable path.** A registry-only scheme would miss the exact entities it
is meant to exclude. And the surface is heterogeneous: two writers
(`research-graph-llmwrap`, `agentic-memory`) emit retrieval content through the
same pipe, so "mutation-API ⇒ control" cannot be a blanket rule.

### The hazard this ADR exists to avoid

A naive design collapses indexing eligibility onto a single binary gate
(`content` in, everything else out). That would quietly exclude the **harness
substrate** (lifecycle/mission/run state, ops findings, project context) from all
semantic reach — losing the ability to graph the harness the way we need to. The
core decision below prevents this by separating *profile* (the producer's
retrieval/indexing hint) from *indexing policy* (a configurable,
per-substrate mapping).

## Decision

### 1. Structural graph is never gated by indexing profile

Entities, triples, relationships, lineage edges, traversal, rule evaluation, and
KV-revision history are **unconditionally available** for every entity regardless
of indexing profile. `indexing_profile` informs *only* embedding/community/search
eligibility. "Graph the harness" is never at risk.

### 2. `indexing_profile` — the producer's retrieval hint

A single-valued reserved triple, `entity.indexing.profile`, with values:

- `content` — Domain/context content meant to be retrieved by meaning. Examples:
  docs, code entities, ops findings, memory lessons, profile/project context,
  LLM-extracted facts.
- `control` — Durable harness/control state that is low-cardinality and
  structurally central. Examples: missions, agent-runs, lifecycle Participants,
  rule-written orchestration facts.
- `signal` — Measured signals that are valuable in aggregate, not as prose.
  Examples: sensor readings, metrics, curve samples.
- `trace` — High-cardinality, append-heavy, mechanically generated execution
  detail. Examples: trajectory steps, tool results, model responses.

The profile describes the producer's default **retrieval/indexing treatment**,
not the entity's ontology class, domain type, access policy, or truth status. It
is set once at entity creation and is stable thereafter because the producer's
storage shape and cardinality are expected to be stable (see §5).

This naming is intentional. SemStreams should not expose this as a field-level
"semantic class" taxonomy. It is an indexing profile used by indexing
substrates.

### 3. Indexing policy — a per-substrate, per-`(profile, entity_type)` matrix

Policy is deployment config, **not** baked into the stamp. It maps
`(indexing_profile, entity_type)` → which substrates index the entity, plus a
cardinality/shape guard:

```text
(indexing_profile, entity_type) -> { embed?, community?, search?, cardinality_guard }
```

Embedding and community are **distinct substrates**. A mission/run entity can be
valuable in *community* detection (it links the harness graph) while being
low-value *embedded* prose (mostly IDs, phases, timestamps, counters). The policy
must express that difference rather than a single yes/no. (The *anomaly* substrate
is retired by the companion cleanup; the matrix is forward-compatible if it
returns.)

### 4. Three declaration channels (writer declares; ingest enforces; consumers honor)

- **(a) Graphable optional interface** — JetStream payload path. A new additive
  optional interface (joining the `message/behaviors.go` family, alongside
  `ContentStorable`):

  ```go
  // IndexingProfiler lets a payload declare its indexing profile.
  // Optional; absence falls through to the framework registry.
  type IndexingProfiler interface {
      IndexingProfile() string // "content" | "control" | "signal" | "trace"
  }
  ```

- **(b) Mutation-envelope field** — the mutation API request gains an optional
  `indexing_profile` field so rule/lifecycle/loop/memory/research-graph writers
  declare intent. *This is the channel the registry-only design lacked.*

- **(c) graph-ingest fallback** — when neither (a) nor (b) supplies a profile,
  graph-ingest derives a *floor* from `message.Type` (registry) and the parsed
  `EntityID` type segment, defaulting to `control`, and records a metric (§5).
  Consumers **never** re-derive profile from ID deny-lists.

### 5. graph-ingest enforcement mechanics

- **Stamp at create.** Profile is materialized on entity creation
  (`create_with_triples`, first `triple.add` that creates an entity, and the
  referential-integrity stub path `processor/graph-ingest/component.go:1200`).
- **Single-valued, replace-on-write.** `entity.indexing.profile` MUST be
  written `RemoveTriples + AddTriples`, never appended. The merge path currently
  appends (`MergeEntity`), and an appended single-valued predicate accumulates
  `[control, control, …]` where last-match and first-match readers
  disagree — the exact beta.103 bug (PR #235; discipline memory
  `feedback_lifecycle_transition_replace_not_append`).
- **Immutable after create unless explicitly overridden.** A later `triple.add`
  by a *different* writer (e.g., a rule decorating a `content` drone entity)
  does **not** re-profile the entity. Only an explicit `indexing_profile` in the
  mutation envelope overrides.
- **Stub deferral.** Referential-integrity stubs carry `core.identity.stub` and
  defer profile assignment; the real producer's arrival resolves it.
- **Observable defaulting.** When (c) fires, increment
  `indexing_profile_default_total{message_type|subject}` — a real counter, not
  a log line — so registry gaps are visible (discipline memory
  `feedback_warning_not_fail_masks_integration_drift`). Otherwise a new
  *content* type nobody registered silently defaults to `control` and never
  gets embedded.

### 6. Cardinality / content-shape guardrail (defense against the *next* storm)

The current storm is `trace`, but a future `control` type that is
"one-per-event" could reproduce it. Therefore: **high-cardinality, append-heavy,
mechanically generated entities default OUT of semantic indexing unless
explicitly allowed**, independent of their declared profile. The guard is part of
the policy matrix, evaluated by the consumer before it spends embedding compute.
`trace` is excluded by *both* profile and the cardinality guard.

### 7. Default policy

- `content`: embed yes, community yes. This is the retrieval corpus.
- `control`: embed yes for low-cardinality lifecycle/harness/run entities;
  community yes. The harness substrate stays semantically reachable, while the
  cardinality guard excludes high-fan-out control types.
- `signal`: embed only summarized rollup entities; community aggregate only.
  Raw readings stay graph-visible but do not become prose embeddings.
- `trace`: embed no, community no. Trace stays fully graph-visible and queryable.

Default fails toward **keeping the substrate reachable** (control in) and
**not embedding the noise** (trace out).

### 8. Measurement rule

If an indexed `(profile, type)` produces low-quality search results or high
fan-out, **tighten the policy** (a config change) — never retroactively change
historical entity profiles. Profile is the stable writer hint; policy is the
tunable. Because gating is consumer-side, every such change is
non-destructive and reversible (re-index on next cycle).

## What gets worse — the cost ledger

Strict indexing is a real cost paid in lost retrieval reach, and this ADR owes an
honest ledger of it, not only the upside. Once `trace`/`signal` drop out of
embedding:

- **Trace/debug questions that *accidentally* worked through embeddings stop
  working that way.** Free-text semantic queries that used to surface trajectory
  steps, tool results, or model responses ("find loops like this one"; a semantic
  search that incidentally matched trace prose) will no longer return those
  entities from the vector substrate. They remain **fully graph-visible** — the
  same questions must be re-expressed as explicit graph traversal / structured
  filters (by entity type, lineage edge, time window, parent loop), which is the
  correct primitive for trace anyway (ADR-045: trace is injected and queried
  structurally, not retrieved by meaning).
- **Any workflow that leaned on embedding-search over operational/trace prose
  degrades.** If something quietly depended on the vector substrate matching
  control/trace text, it gets worse at the flip. The dry-run report (Rollout phase
  3) is how we discover those dependencies *before* the flip — not after a user
  reports a regression.

The line this ADR holds: **all entities stay graph-visible; only the expensive
retrieval substrates are budgeted — and that budgeting is observable, reviewed, and
reversible.** The cost above is acceptable *only* because it is measured and gated,
never discovered in production. If we cannot produce the ledger, we do not flip.

## Consequences

### Positive

- The storm dies structurally: `trace` is excluded from embedding/community by
  profile *and* the cardinality guard.
- The harness substrate is preserved: structural graphing is never gated; ops
  findings / memory / project context are `content` (fully indexed); low-volume
  control state stays indexed by default.
- Community pollution is fixed at the source (signals leave the clustering
  substrate), which directly helps the GraphRAG `search_graph` quality problem.
- One indexing profile, one source of truth on the wire; consumers stay dumb.
- Evolvable without data migration: embed-vs-community, tighten-vs-loosen are all
  policy changes.

### Negative / risk

- **Cold-start / migration (must-handle).** Existing entities have no profile
  triple. Consumers MUST treat *absent profile as legacy-embed* until a
  `strict_indexing_profile` flag flips; graph-ingest backfills via stamp-on-touch
  plus a one-time sweep. Flip to strict **only after the cost-ledger gates pass**
  (Rollout phase 3 — backfill confirmed AND the skipped-entity dry-run report
  reviewed AND the golden-corpus discoverability test green), or the graph goes
  dark.
- **Two mandatory content migrations.** `research-graph-llmwrap` and
  `agentic-memory` write retrieval content through the mutation API and MUST
  declare `indexing_profile=content`, or extracted facts / lessons stop being
  embedded.
- **New reserved predicate namespace.** `entity.indexing.*` requires a
  grammar-collision audit against every `$`-substitution regex and should be
  rule-opaque so it does not leak into `$entity.triple.*` agent surfaces
  (discipline memory `feedback_grammar_collision_audit_on_new_tokens`).
- Producers/mutation-callers now carry a small declaration responsibility; the
  `control` default + metric keeps non-migration writers correct-by-default.

## Alternatives considered

- **graph-ingest decides profile from ID patterns (rejected).** Centralizes a
  brittle heuristic and contradicts Graphable's founding doc
  (`graph/graphable.go:5`: payloads declare, infrastructure does not guess).
  graph-ingest *enforces/materializes*; it does not *decide*.
- **Single binary `content`-in gate (rejected).** Over-excludes the harness
  substrate — the explicit concern this ADR resolves via profile≠policy.
- **Drop trace from `ENTITY_STATES` (rejected).** The Ops Agent and audit need
  trace queryable; `emit_diagnosis` evidence is entity IDs of loops/trajectories.
- **Per-consumer ID deny-lists (rejected).** Drift every time a new entity type
  appears; we have been bitten by exactly that maintenance pattern.
- **Embedding-driven iteration for the anomaly detector (deferred).** Would fix
  the storm's symptom but keep an unproven, graph-degrading feature. The
  companion cleanup removes the engine instead; re-enable only on measured value.

## Rollout

1. **Move 1 (independent, ships first):** disable the **anomaly engine only** —
   `enable_anomaly_detection → false` in checked-in configs `semantic.json` /
   `statistical.json` (the graph-clustering component `DefaultConfig` already
   ships it off). **Structural indexing is retained** as a core capability:
   k-core + pivot distance are valuable graph-search signals whose only *current*
   consumer is the now-disabled anomaly engine, so wiring them into search
   ranking is a tracked follow-up. Fix the phantom-key config drift —
   `similarity_threshold` → `min_semantic_similarity` and `min_core_level` →
   `min_core_for_hub_analysis` (vs `graph/inference/config.go:49,67`) — so the
   operator knobs actually bind, locked by a strict-decode regression test.
2. **This ADR, phase 1:** add `IndexingProfiler` interface + mutation-envelope
   field + graph-ingest stamp (replace-semantics) + defaulting metric.
   Consumers honor profile in **lenient** mode (absent = legacy-embed).
3. **This ADR, phase 2:** migrate `research-graph-llmwrap` + `agentic-memory` to
   declare `content`; seed the framework registry; backfill existing entities.
4. **This ADR, phase 3 — gated, not a flag flip.** Flip `strict_indexing_profile`
   ONLY after ALL of the following hold (the cost-ledger / measured-migration
   gate). This phase MUST NOT ship as a bare config toggle:
   - **Dry-run / shadow report produced and reviewed.** Before strict mode,
     graph-ingest (or an offline pass over `ENTITY_STATES`) emits a shadow report
     of what strict mode *would* do: counts broken down by `message_type`, entity
     type, inferred profile, and the resulting embed/community decision per
     `(profile, type)`. No flip until a human has read this report and signed off
     on the blast radius.
   - **Per-substrate skipped-entity metrics live (not just defaulted profiles).**
     Beyond `indexing_profile_default_total` (§5), each substrate emits a
     *skipped* counter — `embedding_skipped_total{message_type|profile|reason}`,
     `community_skipped_total{...}` — for entities it declined to index. Silent
     skips are the footgun; an entity leaving a substrate must increment a counter,
     not vanish quietly.
   - **Golden-corpus discoverability test green (CI gate).** A fixed test corpus
     proves that *after* strict mode, **memory lessons, ops findings, project /
     profile context, lifecycle / run state, and LLM-extracted research facts
     remain discoverable** via embedding/search. This is the regression gate
     against silent semantic blindness; it must pass before the flip and stay in
     CI so a later producer change can't quietly re-break it.
   - **Backfill complete.** Every existing entity carries a profile triple
     (stamp-on-touch + one-time sweep) before strict; otherwise absent-profile
     entities go dark at the flip.

   Only when all four hold do `trace`/`signal` drop out of embedding. Then measure
   `search_graph` quality to decide community detection's long-term fate (the
   ADR-036 / "is it helping" gate). The flip is reversible: gating is consumer-side,
   so reverting `strict_indexing_profile` re-includes everything on the next index
   cycle — but the gates exist so we never need to.

## Open questions

- Final registry seed coverage — minimal + metric-driven (preferred) vs full
  taxonomy enumeration up front.
- Whether `signal` summarization (embed a rollup entity) is in-scope here or a
  follow-up.
- Exact policy-config surface (per-component vs a shared `graph` policy block).
