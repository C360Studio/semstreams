# Graph Retention — 10-Product Data-Lifecycle Audit

**Date:** 2026-07-08. **Status:** evidence artifact grounding ADR-073 (graph
ingestion + retention contract) and completing ADR-068 (retention/deletion/GC
mechanism). **Do not drift** — this is a point-in-time forensic audit; the model
it grounds lives in ADR-073 and the eventual `graph-retention` spec.

## Why this exists

SemStreams' knowledge graph grows without bound: nothing reclaims entities,
indexes, or blobs. For a product framework that is disqualifying. Two prior
attempts to design the fix drifted (a "DataClass policy layer" that over-fit a
clean abstraction, and a "lifecycle-as-retention-roots" idea that matched by
accident). To ground the decision in reality rather than a tidy abstraction, we
traced **real data birth → life → death across all 10 sem\* products** and
cross-checked each lifecycle against a **verified prior-art research pass** (git,
Datomic, Cassandra, Kafka, time-series DBs). This document is that audit.

## Method

- **10 parallel per-product traces.** For each product, every distinct data type
  was profiled: birth (who/why/what/when/**where** = stream|KV|ObjectStore),
  storage primitive + **actual config** (retention/MaxAge/MaxBytes/TTL/history),
  life (durable entity | stream | projected current-state | blob; **root |
  satellite | firehose**), death (criterion + whether it reclaims today or grows
  unbounded), and the **prior-art idiom** its death needs. Grounded in code with
  file:line. Products: semops, semlink, semsource, semdev, semdragon, semboids,
  semteams, semconnect, semstreams-ui, semspec (archiving).
- **Prior-art research pass** (110-agent verified harness): how mature systems
  bound growth while preserving referential integrity.
- **Evidence & reproducibility.** Each per-product section below is *distilled*
  from those traces, but **every load-bearing claim carries an inline `file:line`
  citation** into the named repo — that citation IS the durable evidence, and any
  claim is reproducible by reading the cited file at the cited path (`../<product>/…`).
  The raw agent transcripts were the working input, not the record; where a claim
  lacks a cite it is a summary judgement, not a code fact. Codex's independent
  code-grounded pass (PR #509) re-verified the SemStreams-side claims against
  head — they held.

## Prior-art idioms (verified against primary sources)

Mature systems bound growth with **two orthogonal idiom families, and never mix
them on one tier**:

| Family | Systems | Bounds by | Reclamation | Correct for |
|---|---|---|---|---|
| **Reachability** | git mark-sweep from refs + reflog grace; Datomic accretion/supersession + rare excision | proof-of-unreachability from declared roots | **aware** | identity-bearing, relationship-carrying data |
| **Time / cardinality** | Cassandra tombstone + `gc_grace_seconds` + compaction; Kafka log-compaction (latest-per-key); TSDB retention-window + downsample + chunk-drop | wall-clock / key-cardinality | **blind** | high-cardinality firehose |

Universal rules from the prior art: reclamation is **background/batched, never
inline**; gated behind a **grace window sized > worst-case in-flight/recovery
lag**; **accretion + supersession is the normal write path**, true erasure is the
rare exception (git prune, Datomic excision, Cassandra tombstone purge).

**Three failure modes** the composition must design against:
1. **Resurrection race** — GC deletes something a lagging writer/projector
   re-references. Fix: grace > max lag (ties to ADR-066 watermark).
2. **Incomplete root declaration** — any un-enumerated root deletes live data
   (git's root set is deliberately extensive). Fix: complete, explicit roots.
3. **Partial-cluster expiry** — reclaiming a subset of a connected cluster
   strands dangling refs. Fix: **never partial *blind* expiry** — a tree-owned
   cluster reclaims all-or-none, but a **DAG-shared satellite dies with its last
   referrer** (refcount / refuse-if-referenced; ADR-073 §2 supersedes the naive
   all-or-none framing, which breaks on DAG sharing).

## Per-product findings (distilled; inline `file:line` cites are the durable, reproducible evidence — see Method)

### semstreams-ui — clean negative
Owns **no durable graph data**; a read/derive projection. Only durable datum is
browser-local panel layout (`localStorage`). Every "pin"/context-chip is
in-memory, dies on reload → **creates no retention root today.** Bankable rule:
*the `pinned`/"pin" vocabulary is a naming trap* (UI ergonomics, not roots); and
**any future backend-persisted UI reference to an entity ID would be a root.**

### semboids — the firehose pole; first crack in the trichotomy
Two graph roots (boid, zone) in `ENTITY_STATES`; one persisted firehose (`ENTITY`
stream, `file, max_age:168h`, **no `max_bytes`**, `configs/flock.json:16-24`);
ephemeral core-NATS events. **Key finding:** the boid entity is a
**firehose-cadence-rewritten identity root** — one KV key carries (i) disposable
position rewritten ~30×/s, (ii) a must-not-lose `flock.lifecycle.phase`
transition, (iii) reachability-bound existence (cull→delete). **Three idioms on
one key ⇒ the retention unit is the *facet*, not the entity.** Safe only because
D1 bans TTL on graph KV. Observed live: **resurrection race** (in-flight snapshot
re-creates a culled boid as a phase-less zombie) and **births-outrun-GC** (no
population cap; `ENTITY_STATES` grew 590→3266 in 25s — `churn-lifecycle-2026-07-06.md:70-74`).

### semconnect — router; two owned stores
Owns an observations **stream** (`LimitsPolicy, FileStorage, MaxAge 30d,
MaxBytes 0`, `gateway/cs-api/component.go:360`) + a schema-artifact **ObjectStore**
(byte-cap only, **unset**). CS-API entities are written into semstreams KV it
doesn't own. **Misfit:** `SystemEvents`/`Command` are per-event, time-keyed data
modeled as permanent graph identities (no delete/supersession/MaxAge). **Owned
satellite bug:** schema artifacts orphan (graph entity + ObjectStore blob) when
their parent datastream is deleted — no cascade; ControlStreams have no delete at
all. Third storage tier appears: **ObjectStore = reachability/refcount, no TTL.**

### semsource — the durable-root pole; over-retention
Nearly everything is a **root** (code/AST symbols [~21k], git commits/branches,
doc/url/media) in `ENTITY_STATES` (TTL correctly banned; semsource sets none).
Growth control is a **write-boundary ingestion-depth budget per source**
(ADR-0008 §4), *not* eviction — "a graph cannot safely delete by policy;
reference-blind eviction orphans edges." Failure mode **inverted**: not
under-retention but **over-retention** (every version kept forever, demoted via
signed salience −2.0, never deleted; reclamation entirely unbuilt). One firehose: the
`GRAPH` stream (`memory, MaxAge 1h, MaxBytes 256MiB` — the **one** product that
sets a byte backstop). **Loud flag:** source-manifest ingestion **status** is
current-state data with **no KV-twofer home** (1h memory stream + in-memory cache
only) — the readiness signal, the product's whole thesis, is the one datum not
durable. Media bytes live on a **local filestore with zero lifecycle** (worst-
bounded store in the fleet).

### semops — fusion/defense telemetry; the sharpest config case
Owns **zero NATS storage primitives** — all ports are plain core-NATS; all
durable state → semstreams graph KV via mutations. Raw feed frames = **firehose**
in an **in-memory bounded ring buffer wired through port config**
(`1024 records / 8 MiB`, FIFO evict — `pkg/adapters/mavlink/raw_lane.go:11-14`);
never a graph entity — the raw-lane-vs-current-state pattern, done right. Roots
(`asset`, `track`, `task`, `sensor_footprint`) are all `ModeReplaceOwned` —
supersede in place, one entity per real thing — but **none reclaimed**; even
command-intent, richest in TTL/deadline/cancel signals, only **transitions
status** (`StatusExpired`) and lives forever. **Loud flag — CAP `hazard_area`
(`ModeAppendEvidence`, `pkg/cop/contracts.go:548`):** a root by identity but
firehose by internal structure (one evidence-set appended per 5-min poll, no
compaction, ignores its own `expires`). *"append-evidence mode gives products an
identity-tier object with firehose-tier growth and no reclamation idiom for
either half."* Wants decomposition: compacted current-state root + windowed
evidence firehose.

### semlink — MAVLink companion mesh
Firehose (`MAVLINK_RAW`, `memory, MaxAge 5m, MaxMsgs 250k`) fronted by a
`DropOldest` circular buffer — correct. Roots: vehicle current-state, COP
operators/markers (`ModeReplaceOwned`, bounded). **Misfits:** command-intents
(one entity per submission, `unixMilli` suffix) and inbound GeoChat (per-message
UID) are firehose-cardinality entities in KV, no GC — GeoChat worse (content-
profiled → BM25 budget). **Model answer discovered:** the (unwired) **mesh
delta-sync envelope** carries explicit `ExpiresAt` + merge-class (LWW-per-origin /
set-union + **bounded-tombstone**) + HLC causality — the cleanest firehose
lifecycle design in the fleet.

### semdev — agentic dev; forbidden to self-build reclamation
One root: the **run entity** (one per issue, `agentrun` participant) with **no
death path at all** (closed 8-action taxonomy has no delete/despawn; `archive`
is a phase flip). Satellites: bulky `openspec.change.*` **prose stored verbatim
in triple objects** (KV bloat, no ObjectStore offload). **Permanent tier:** the
`evidence.run` ledger **must never die** (G7 honesty) — the one datum whose
unboundedness is correct, and a hard argument against any blanket time-bound.
**Misfit:** `AGENT_TRAJECTORIES` = firehose on a KV tier. **Architectural
constraint:** semdev's port manifest (B1/G2) **forbids hand-rolling a reclaimer**
⇒ reachability-GC **must** be a framework primitive.

### semdragon — agentic RPG; the write-path precondition
Writes entities via **raw `KV.Put`/`Update` straight into `ENTITY_STATES`,
bypassing the mutation API** (`graphclient.go:112-127`) — full-object LWW
overwrite ⇒ **per-predicate supersession is impossible until it migrates**
(its own research: `.../research/01-graph-ownership.md`). Roots: agent, guild
(superseding), quest (accreting). **Intra-entity growth:** `guild.knowledge.lessons`
is an unbounded JSON blob inside one key (`History=10` keeps 10 revisions of an
ever-larger value). **Misfit — `dagunit`:** per-DAG-node-per-run coordination
markers (firehose-shaped) in the identity tier, no time-bound *and* no
reclamation — the purest form of the anti-pattern, and the *only* semdragon write
through the framework mutation lane. Aux buckets **do** carry TTLs
(`BOID_SUGGESTIONS` 5m, `DM_SESSIONS` 7d) — confirming TTL is fine on
operational/aux KV, banned on identity KV.

### semspec (archiving) — the working precedent + the decommission case
**The one product with real identity-tier reclamation:** `lesson-curator`
(ADR-033 Phase 5, `processor/lesson-curator/retirement.go`) retires Lessons on
staleness (evidence gone, code git-rewritten, never-injected-past-grace,
idle-past-threshold) with a `minAgeBeforeRetire` grace, and **tombstones**
(marks `RetiredAt`, keeps for audit, excludes from injection) — never deletes.
**This is Cassandra tombstone+grace, already built — the pattern to lift.**
**Roots split two ways:** run-roots (Plan) vs permanent institutional-memory
roots (Lessons) co-mingled on one tier under one (nonexistent) death policy.
TTL'd operational tier done right (`QUESTIONS`/`RESEARCH` 30d, `GITHUB_ISSUES`
90d). **Decommission (special finding):** *no clean path* — the only mechanism
that removes data is `docker compose down -v` (drops the whole volume = all-or-
none failure mode); **no export exists** (`graph.export.>` is a reserved subject
with zero producers); per-plan "archive" is a phase flip. Git's answer — remove
the roots, GC the transitively unreachable, export the keepers first — **doesn't
map because there is no reachability GC.** New failure mode: **dual-write with
divergent retention** (Plan in `ENTITY_STATES` no-TTL + `PLAN_STATES` no-TTL;
Question in graph no-TTL + `QUESTIONS` 30d-TTL) → TTL'd copy expires while graph
copy persists → resurrection by tier-disagreement (its P0-5 dead-plan re-dispatch
is this already firing).

### semteams — imported components only; textbook tier-collision
One product root: the **chain/run entity** (`agent.chain.execution.<runID>`,
ADR-038). **Misfit (the crux):** **loop-execution entities** are born
one-per-agent-call (N-propose + N-execute per autoresearch run) — firehose shape
— yet each is a permanent identity entity in `ENTITY_STATES`; the operational
twin (`AGENT_LOOPS` `COMPLETE_{loopID}`) correctly expires at **24h TTL**, so the
*same run exists twice with contradictory lifetimes* (24h operational vs immortal
graph). **Off-substrate blobs:** rendered artifacts + sandbox workspaces on the
raw host filesystem with **zero lifecycle** — the substrate's ObjectStore +
ADR-068 owned-blob GC exists but is bypassed. **Live proof of leak: 9.3 MB of
month-old orphan `.tenant-workspaces/` dirs.** Streams correct (USER 1h
deliberate; AGENT/TOOL inherited `MaxAge 7d, MaxBytes 0`).

## Cross-product matrix

| Product | Root(s) | Firehose (right?) | The misfit (firehose-as-identity) | Reclaims today |
|---|---|---|---|---|
| semstreams-ui | none | ephemeral UI stores | — (pins in-memory) | n/a |
| semboids | boid, zone | ENTITY 168h ✓ (no MaxBytes) | boid key: cadence+phase+existence (per-facet) | ✗ births > GC |
| semconnect | CS-API entities | observations 30d ✓ (no MaxBytes) | SystemEvents/Command as entities | ✗ orphan satellites |
| semsource | code/doc/url/media | GRAPH 1h ✓ (256MiB ✓) | dup triples; homeless status | ✗ over-retention |
| semops | asset/track/task | in-mem raw lane ✓ | CAP ModeAppendEvidence | ✗ expiry=bookkeeping |
| semlink | vehicle/operators | MAVLINK_RAW 5m ✓; mesh ✓✓ | command-intents + GeoChat | ✗ |
| semdev | run entity (+perm ledger) | AGENT/TOOL/USER ✓ (no MaxBytes) | AGENT_TRAJECTORIES on KV | ✗ + forbidden to self-build |
| semdragon | agent/guild/quest | AGENT 72h ✓ | dagunit; raw-KV-Put bypass | ✗ |
| semspec | run-root + **perm-root** | AGENT 24h (mem, mis-prov) | loops; dual-write split-brain | **✓ lesson-curator** |
| semteams | chain/run entity | streams 7d ✓ (no MaxBytes) | loop entities; host-FS blobs | ✗ 9.3MB orphans |

## Synthesized model

Three tiers; the tier is chosen by **data shape** and enforced at the
graph-ingest **write boundary**:

| Tier | Bound by | Primitive | Idiom | Status in fleet |
|---|---|---|---|---|
| **Firehose** (per-event) | TIME | stream MaxAge + **MaxBytes backstop** \| bounded buffer | TSDB window / Kafka retention | ✓ mostly right; `MaxBytes:0` is a systemic bad default |
| **Identity** (durable + relationships) | REACHABILITY + per-facet supersession/compaction | `ENTITY_STATES` KV (TTL banned — correct, fail-closes at boot) | git mark-sweep + Datomic supersede + Cassandra tombstone+grace | ✗ unbuilt everywhere; **1 working precedent: `lesson-curator`** |
| **Owned blob** (bulky) | REACHABILITY (refcount) | ObjectStore (ADR-068 owned-blob GC) | git-LFS / refcount | ✗ products bypass to host FS |

Four refinements the traces forced:

1. **The retention unit is the *facet* (predicate-group), not the entity** — one
   key legitimately mixes firehose-cadence facets, durable transitions, and
   reachability-existence. This is **already expressed by projection-contract
   write modes** (`ModeReplaceOwned` vs `ModeAppendEvidence`); the gap is that no
   reclamation idiom is wired to any mode, and `ModeAppendEvidence`-on-KV is an
   unbounded footgun the framework *offers*.
2. **Roots split two ways:** *run-roots* (reachability-GC when the run ends) vs
   *permanent-roots* (institutional memory / audit ledger — keep forever,
   compact-in-place). The permanent tier is the hard argument against any blanket
   time-bound on the graph.
3. **Reclamation must be a framework primitive** (semdev is structurally forbidden
   from building it app-side), and **it presupposes writes flow through the
   contract** (semdragon's raw-KV bypass makes per-facet supersession impossible).
4. **Lifecycle is not the root definition** — roots are reachability/aggregate
   entities (semsource corpus, semdragon agents, semspec Lessons) that mostly
   aren't lifecycle instances. Lifecycle-despawn is *a* root-removal trigger, not
   the definition. **"DataClass" is dead:** no product wanted a producer-declared
   retention class; they wanted firehose kept off the identity tier and the
   identity tier reclaimed by reachability.

## Failure modes — observed live, not hypothetical

- **Resurrection race:** semboids zombie boid; semspec dual-write split-brain
  (P0-5). → grace > max projection/recovery lag.
- **Under-declared roots = data loss:** semops `EdgeNoBirthStub`; the permanent
  audit/institutional roots (semdev ledger, semspec Lessons) **must** be in the
  keep-set. → complete, explicit root declaration.
- **Partial-cluster expiry:** semspec `down -v` (whole namespace only). → never
  partial *blind* expiry: tree-owned clusters all-or-none, DAG-shared satellites
  die-with-last-referrer (refcount; ADR-073 §2).
- **Births outrun GC:** semboids (no population cap; cull 2-RTT vs spawn 1-RTT).
  → the write side must be rate-matched to the reaper.

## Decommission (product/run retirement)

Same primitive as retention, applied to a whole root-set: **remove the roots →
reachability-GC sweeps everything transitively unreachable → export the
permanent-roots first.** semspec proves both halves are missing today (no GC, no
export). This is git "delete a namespace" and it is the correct shape.

## Implications

- **ADR-068** (retention/deletion/GC mechanism) is *validated* by the prior art
  and **amended-and-completed** by ADR-073: D3's *single shared* reverse-index
  becomes a per-owner / tombstone-payload choice, and D5's *central* sweeper is
  demoted to an off-by-default backstop (primary index cleanup is decentralized
  reactive) — with the failure-mode guardrails and `lesson-curator` as the
  identity-tier tombstone-mechanism reference.
- **ADR-073** is repurposed from the dead "DataClass policy layer" to **the
  graph's three-tier ingestion + retention contract**: firehose→time-windowed
  streams (with a MaxBytes backstop default), identity→reachability-GC + per-facet
  compaction, blobs→ObjectStore owned-blob GC, **enforced at the graph-ingest
  write boundary** (via the already-existing projection contract), with roots
  declared (run vs permanent) and decommission as a root-set sweep.
