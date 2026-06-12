# Fix Plan: A Write-API Taxonomy for graph-ingest (retiring "mutation API as producer")

> Status: DESIGN + PLAN. No production code here. Companion to
> `docs/proposals/graphable-bypass-audit.md` (the blast-radius audit). Reframed
> from the audit's first-cut "everything must be Graphable" framing onto the
> correct axis: **the defect is metadata-less entity *birth*, not "not
> Graphable."** Grounded in two multi-agent passes (audit + design) with every
> load-bearing fact verified against code; adversarial break-tests folded in.
>
> **AUTHORITATIVE DECISION RECORD: [ADR-055](../adr/055-graph-write-intent-taxonomy.md).**
> ADR-055 was multi-lens reviewed; two of its corrections override this doc where
> they differ: (1) **Participant identity is born on the Entity-create lane
> (`create_with_triples`, create-or-fail), NOT the Fact-arrival append stream** —
> this dissolves the "field-classifier filter" problem (a one-shot create can't
> duplicate single-valued predicates) and makes T1 moot for Participants. (2) The
> **rule-ID governance audit (`rule.deny`/`rule.approve`) is a must-exist casualty**
> that must move to a dedicated governance bucket before the flip — it is NOT in
> the "untouched ~26 stampers" set. Treat ADR-055 §3/§3a/§5/§6 as the corrected
> versions of this doc's §2.2/§5/§6.

## 1. The invariant (corrected)

We are **not** establishing "every first-class entity must be Graphable." That
over-corrects — Graphable is append/event-shaped and a *bad fit for state*
("transition this entity only if it is still at revision R"). The invariant is:

> **Every entity is *born* through a lane that carries a semantic envelope
> (MessageType / domain / category / version). No write may *create* an entity
> without one. State transitions keep their CAS-with-reject semantics as a
> first-class lane.**

The leak is unclassified entity *birth* via `triple.add`'s auto-vivify
(`AddTriple`, `component.go:1417` — creates a record from a bare `triple.Subject`
with no MessageType, no producer contract). Kill *that*, and "invisible
unclassified graph state" becomes structurally impossible — without forcing state
onto an event-shaped wire it doesn't fit.

## 2. The graph-ingest write surface — four intent-typed lanes

This answers the design question directly ("a port/stream for state transitions +
remove `triple.add`"). The surface is four lanes by *write intent*. Critically,
the transition lane stays **request/reply, not a JetStream stream** — see §2.1.

| Lane | Ingress | Shape | Conflict semantics | Creates? | Envelope |
|---|---|---|---|---|---|
| **Fact arrival** | Graphable stream consumer *(exists)* | JetStream consume | CAS-with-retry, async, **never rejects, APPENDS** | yes — merge/create | **carried** (registered payload → `MessageType` stamped `component.go:885`) |
| **Entity create** | `entity.create_with_triples` *(exists)* | request/reply | atomic **create-or-fail** (`ErrKVKeyExists`) | yes | **required** |
| **State transition** | `state.transition` *(promote `update_with_triples`+`ExpectedRevision`)* | request/reply | **CAS-on-condition, REJECTS, sync**, replace-by-predicate | no (must-exist) | inherited |
| **Evidence append** | `evidence.append` *(neuter `triple.add`/`add_batch`)* | request/reply (batched) | append multi-valued, **must-exist** | **no** | inherited |
| ~~Auto-vivify create~~ | ~~`triple.add` bare-Subject~~ | — | — | **DELETED** | — |

Every entity is born only through **Fact arrival** or **Entity create**, both
envelope-bearing. The bottom two lanes are must-exist. An unclassified,
`MessageType`-less entity is unrepresentable.

### 2.1 Why the transition lane is request/reply, not a stream

A state transition's defining need is **synchronous conditional reject** ("apply
iff still at rev R; tell me *now* if I lost so I re-read and retry"). That reject
channel is what `Manager.Transition`'s loop runs on (`manager.go:431-524`). A
JetStream stream is async fire-and-forget — you don't synchronously learn whether
the CAS won. You *can* stream it via `Nats-Expected-Last-Subject-Sequence`
(publish-time optimistic concurrency with a sync `PubAck` error), but that
requires **one subject per entity**, making ENTITY_STATES a *projection* of a
transition stream — i.e. event-sourcing state, KV stops being the source of
truth, restart means replay-rebuild. The **KV-twofer already gives durable
transition audit for free via revision replay** (ADR-049 G4), so streaming buys
nothing here at large cost. The transition lane belongs in the RPC family — and
it already does: `update_with_triples`+`ExpectedRevision` is *already* must-exist
+ CAS-on-condition (`mutations.go:493`, `updateEntityAtRevision:1311`). The work
is promotion/naming + a mandatory envelope on create, not stream-ification.

### 2.2 "Remove `triple.add`" = remove its *create* power (must-exist), keep append

`triple.add` welds two behaviors: (a) append-to-existing (legit evidence — the
~26 cleared rule/inference derived-fact stampers), and (b) auto-vivify a missing
entity (the footgun). Sever (b): make `triple.add`/`add_batch` **must-exist**
(reject "entity not found" when the Subject is absent), optionally renamed
`evidence.append`. This converts **every one of the 7 conjurors into a hard
error**, forcing each onto an enveloped birth lane, while the 26 legit stampers
(which write onto entities that already exist via ENTITY_STATES watch / prior
Graphable origin) keep working untouched. **The ordering risk stops being a
silent auto-vivify and becomes a fail-fast** — which is the point.

> **KEY DECISION (see §6):** this *replaces* the grounding plan's "keep
> auto-vivify as the self-healing ordering safety net" with "must-exist +
> origin-first ordering." It is the higher-integrity choice (structural
> invariant) but costs per-conjuror ordering guarantees. Your "remove
> `triple.add`" steer chooses must-exist; this plan adopts it.

## 3. The per-predicate decision matrix (the durable contribution)

Classify **each predicate**, not each entity — most entities are *hybrids* whose
predicates split across lanes. Keyed on: single-valued vs accumulating, and
second-writer/transition race vs not.

```
For each predicate of an entity:
  1. Phase-transition race or read-modify-write counter race?
        YES → lane (iii) state.transition (CAS-on-condition). It's a Participant.
  2. Single-valued AND re-emitted onto the same Subject over the entity's life
     (restart re-assert, re-populate, per-boot)?
        YES → lane (ii) update_with_triples default (MergeTriples REPLACE-by-(s,p), gh#244).
  3. Else (born-once identity, accumulating links, immutable bodies)
        → lane (i) Graphable stream (with the two transport guards below).
```

- **(i) stream / append** — born-once, never re-emitted. Identity, accumulating
  links, immutable bodies.
- **(ii) mutation-replace** — single-valued, re-emitted, no transition race.
  Config leaves, profile-root version, re-populated content-addressed vertices.
- **(iii) state.transition CAS** — phase advances, RMW counters.

**Do NOT build a fourth "replace mode" on the stream consumer.** The replace need
is already served by lane (ii) (`MergeTriples`, gh#244). A parallel replace-mode
on `MergeEntity` would fork stream-consumer semantics and duplicate gh#244. The
primitive set {append-stream + (T1/T2), mutation-replace, CAS-on-condition} is
complete — this is the deliberate-completion answer, not a gap-of-the-week patch.

## 4. Transport guards the fact lane requires (break-test findings — mandatory)

The fact-arrival lane is **not free**. The design phase's adversarial pass found
two defects that make it unsafe even for born-once predicates without hardening:

- **T1 — IDEMPOTENT PUBLISH (redelivery safety).** `natsclient.PublishToStream`
  sets **no `Nats-Msg-Id`** (`client.go:793-799`) → JetStream publish-dedup is OFF
  and the consumer is at-least-once. On any redelivery, `MergeEntity`'s blind
  append (`component.go:1006`) re-appends — duplicating even "born-once"
  single-valued predicates. **Every stream-riding entity MUST publish with a
  deterministic `Nats-Msg-Id`** (`loopID:spawn`, `entityID:vN`). New framework
  affordance: `PublishToStreamWithMsgID`. Without T1, "written once" is an
  app-layer fiction the transport violates.
- **T2 — SINGLE-SUBJECT GRAPHABLE (mis-file safety).** `extractEntityFromMessage`
  (`component.go:882-887`) writes *all* of `graphable.Triples()` to one KV key =
  `EntityID()`, **no Subject regrouping** (the `bySubject` split lives only in the
  `AddTriples` handler). **Any triple whose `Subject != EntityID()` is silently
  misfiled** and unreachable by a per-ID `Get`. **Every Graphable MUST be
  single-Subject**; cross-entity links (`has_step`, `has_layer`, `has_lesson`)
  live on the Graphable whose EntityID is the link's Subject — never the child.
  Add a framework guard asserting `t.Subject == EntityID()` in
  `extractEntityFromMessage` (reject + log loudly).
- **StorageRef extraction.** `extractEntityFromMessage` never populates
  `entity.StorageRef` from a `ContentStorable` payload (the embedding worker
  branches on `StorageRef != nil`), so offloaded content silently stops being
  embedded. Patch: type-assert `ContentStorable` and set `StorageRef` (MergeEntity
  already merges it on the existing branch, `component.go:1008-1009`). Generic,
  additive.

## 5. ADR-049 reopen — it *validates* ADR-049, it doesn't reverse it

ADR-049 already put lifecycle state **in ENTITY_STATES** (graph-visible; it
reversed the private-bucket predecessor) and chose the CAS-on-condition mutation
wire *because transitions need reject-on-mismatch the merge/stream path can't
give*. Under the corrected taxonomy, that is **lane (iii) used correctly** —
ADR-049 is the *exemplar of the state pattern*, not a violator.

What it didn't do: give Participants a Graphable **origin** for their *identity*.
Mission already does (verified: `mission.Command` is a clean single-Subject
Graphable, registered `mission.command.v1`, phase stamped as a CAS second writer);
`AgentRun` does not (no `Triples()`), so its `chain.execution` entity is conjured
by `Manager.Create`'s wire alone. The reopen's job is to make agent-run look like
mission.

**Recommendation C (hybrid):** Participant **identity + accumulating attributes**
ride lane (i) via a generic `lifecycle.ParticipantEntity` adapter
(`Triples() = projectStructToTriples(...)` filtered to non-single-valued / origin
fields); **phase + audit scalars + projected counters stay on lane (iii)**
(`Manager.Transition`/`TransitionWith`). Walked against the seven guarantees:

| Guarantee | Hybrid verdict |
|---|---|
| **G1** reject-on-mismatch transitions | PRESERVED by construction (transitions never touch the stream). Guard: lint that no phase/audit predicate appears in any adapter `Triples()`. |
| **G2** atomic phase+audit landing | PRESERVED (one Transition delta under one `ExpectedRevision`). |
| **G3** create-or-fail race | PRESERVED (`Manager.Create`→`CreateEntityStrict`), *but the origin publish must be T1-idempotent* or a redelivered origin re-appends identity. |
| **G4** History fidelity (revision replay) | PRESERVED (transitions stay off the append stream; origin appends don't scatter audit). |
| **G5** replace-not-append single-valued phase (beta.103) | **THE FRAGILE ONE.** Stream appends, no replace. PRESERVED **only if** the adapter excludes *every* single-valued predicate. Mandatory guard. |
| **G6** provenance (`MessageType`) | PRESERVED (origin payload is a registered type → stamped at `component.go:885`). |
| **G7** transition-table + terminal/drift validation | PRESERVED (runs synchronously in the req/reply loop; identity-only origin has nothing to validate). |

**Verdict: the hybrid holds iff** the adapter `Triples()` excludes every
single-valued predicate (G1/G4/G5), the origin publish is T1-idempotent (G3), and
single-Subject (T2). Mission satisfies all three in production — the existence
proof. **Decide the reopen independently; it gates B7.**

## 6. The ordering decision (must-exist vs auto-vivify)

This is the one place the corrected plan diverges from the grounding plan, and it
follows from "remove `triple.add`":

- **Grounding plan (auto-vivify safety net):** keep `triple.add` auto-vivify;
  derived stamps that race ahead of the origin self-heal because a late origin
  merge converges on the same key (relies on T1 so convergence doesn't duplicate).
  Less invasive; leaves the footgun loaded.
- **This plan (must-exist + origin-first):** neuter `triple.add` to must-exist;
  each conjuror's **origin must land before any derived stamp / transition on that
  entity**. Ordering becomes a hard precondition enforced by fail-fast errors.
  Higher integrity; costs per-conjuror ordering guarantees.

The 26 legit stampers are unaffected either way (their Subjects already exist).
Only the conjurors must establish origin-first ordering — which is tractable: for
loop-execution, spawn-create lands synchronously before the loop executes, so the
mid-loop stamps (decide/write_todos/scratchpad) always find the entity present.
**This plan adopts must-exist.** The per-entity ordering guarantees are in §7.

## 7. Per-entity application (B1–B7)

Each entity maps its predicates onto the matrix. Condensed from the verified
design pass; effort S < M < L.

- **B1 loop-execution (L)** — `agentic.LoopExecutionEntity`, Phase-discriminated
  (`spawn`/`completed`/`failed`/`cancelled`), single-Subject (`has_step` lives on
  B2). Born-once spawn + terminal predicate sets, disjoint → lane (i) **with T1
  msg-IDs** (`loopID:spawn`, `loopID:terminal`) — without T1, redelivery
  re-appends outcome/tokens/cost (beta.103). Not a Participant (no phase race).
  Folds synthetic-decide into the completion payload; one BaseMessage = one
  revision = one UPDATED event (improves gh#159). **Origin-first:** spawn-create
  before the loop executes. *Highest leverage — legitimizes ~8 downstream
  stampers.* Couples to ADR-054 (`content`). Drop the hierarchy-on-loop promise
  (or make it idempotent-(s,p,o)).
- **B2 trajectory-step (M)** — reuse the EXISTING `agentic.TrajectoryStepEntity`
  (already Graphable+ContentStorable); add `MarshalJSON` + an **exported**
  `StorageReference` field + Schema. Lane (i), single-Subject (`has_step` on the
  loop). Consumes the **StorageRef extraction** framework patch (§4) — that's the
  real work; without it offloaded step text stops embedding (e2e:semantic gate).
  Needs a graph-ingest input wire in every agent-loop config.
- **B3 model-endpoint (S)** — `agentic.ModelEndpointEntity` wrapping
  `model.EndpointConfig`; `Triples()` = `buildModelEndpointTriples`. Re-emitted
  every boot → **lane (ii)** (`update_with_triples` replace), restart-idempotent,
  zero flow wiring (the production agentic flow's graph-ingest input is
  `graph.mutation.>` only — no entity-stream consumer). **The Path-(ii) pilot.**
  Must check the `error:` response prefix on the new mutation caller.
- **B4 web-observation (S–M)** — content-addressed vertex (one per canonical URL),
  single-Subject (loop back-links stay on the loop). Re-fetch re-populates the
  same single-valued predicates → **lane (ii)** replace. Mirrors B3.
- **B6 operating-model (L)** — **FOUR single-Subject Graphables**
  (`Profile`/`Layer`/`Entry`/`Lesson`), one per KV record (T2: a multi-Subject
  payload would misfile every entry into the layer record → entries vanish).
  `handleLayerApproved` publishes 1 profile + 1 layer + N entry BaseMessages.
  Bodies = lane (i) + T1; the **profile root** (single-valued `version`/`last_updated`
  + multi-valued `has_layer`/`has_lesson`) = lane (ii) replace. The custom
  `graph.mutation.{loopID}` envelope is **orphaned in-repo** (no consumer) → the
  migration is a net fix. Leave the `publishGraphMutations` seam for Group-C.
- **B7 workflow-execution example (M)** — **the textbook Participant** and the
  matrix in microcosm: identity → lane (i) (T1); `workflow.phase`+audit → lane
  (iii) `Manager.Transition`; `tokens.total`/`review.rejections` RMW counters →
  lane (iii) `TransitionWith` mutator (plain replace loses concurrent increments).
  **HARD-GATED on the ADR-049 reopen** (rides its origin mechanism). Ships alone
  as the canonical teaching artifact (README is part of the deliverable). Possible
  framework gap: a no-phase-change CAS mutator (`Manager.MutateWith`) for counters;
  interim `update_triple` is safe under the example's `MaxAckPending:1`.

## 8. Sequenced landing plan

**Wave 0 — framework primitives (land first; small; unblock everything):**
1. **Must-exist `triple.add`/`add_batch`** + the four-lane naming
   (`evidence.append`, `state.transition`). The footgun removal; converts every
   conjuror to a fail-fast. *(Breaking-ish for the conjurors — that's the point;
   stage behind the per-entity migrations or land with a deprecation window.)*
2. **T1** `PublishToStreamWithMsgID` + dedup-window config. Unblocks all
   stream-side units.
3. **T2** single-Subject guard + **StorageRef extraction** in
   `extractEntityFromMessage`. Closes the misfile + embedding-drop classes
   generically.
4. **ADR-054 Phase 1** (IndexingProfiler, LENIENT) — soft; lands before B1/B2 so
   they carry the right envelope.

**Wave 0 audit — in-process `Component.AddTriple` callers (CLEARED 2026-06-12).**
ADR-055 §3 required a Wave-0 audit confirming no in-process `Component.AddTriple`
caller relies on auto-vivify ordering before the must-exist flip. Result: **SAFE —
the in-process bypass imposes no blocker on the closing move.**

- The only production in-process caller via `tripleAdderAdapter` (`component.go:691`)
  is `HierarchyInference`, and only for INVERSE edges onto OTHER subjects: sibling
  back-edges (`hierarchy.go:313`, target listed via `ListWithPrefix` = pre-existing)
  and container contains-edges (`hierarchy.go:368`, target pre-created by
  `ensureContainerExists`). Both handle a write failure as Warn-only + `edgesFailed`
  metric — NEVER propagated. So a future must-exist rejection on an inverse write
  degrades gracefully: the entity's own forward triples still land and ingest does
  not fail. Locked by `TestHierarchyInference_InverseEdgeWriteFailureIsNonFatal`.
- `HierarchyInference.OnEntityCreated` (`hierarchy.go:239`, the legacy cascade path)
  has NO production caller — test-only.
- `DirectRelationshipApplier.ApplyRelationship` (`applier.go:193`) has ZERO production
  wiring; production structural inference uses `MutationRelationshipApplier` (the NATS
  `graph.mutation.*` path, subject to the flip at the consumer, not an in-process
  bypass).
- `graph/messagemanager.ProcessMessage` (a second Graphable→EntityState consumer with
  the same T2 single-key misfiling) has ZERO non-test importers — dead/legacy.
  Reclassified as a removal candidate, NOT a regroup target.

**Wave 1 — pilots (parallel, prove the patterns):** B3 (lane-ii replace pilot),
B2 (lane-i ContentStorable pilot, consumes T1/T2/StorageRef).

**Wave 2 — core entity:** B1 loop-execution (after pilots prove T1 + entity-port
wiring; coordinate after B2 so the loop origin precedes `has_step`).

**Wave 3 — replace siblings (parallel after B3):** B4, B6.

**Wave 4 — lifecycle gate:** ADR-049 reopen (Recommendation C + adapter +
agent-run origin), then B7 (hard-gated on it).

## 9. Open questions needing a human call

1. **JetStream dedup window vs restart replay (T1).** A deterministic `Nats-Msg-Id`
   only dedups within the `Duplicates` window; restart replay (`DeliverPolicy all`)
   can re-merge outside it. Size the window to the recovery horizon, accept
   bounded re-merge, or add an (s,p,o) idempotent-merge guard? The one place T1
   alone may be insufficient.
2. **Must-exist rollout shape.** Land the `triple.add` must-exist flip *before*
   the per-entity migrations (forces them, but breaks them loudly in the interim)
   or *after* (each conjuror migrates first, then the flip is a no-op)? Recommend
   after — migrate every conjuror to an enveloped birth lane, then flip must-exist
   as the closing move so nothing is ever broken on `main`.
3. **ParticipantEntity adapter scope (ADR-049).** Generic adapter framework-wide,
   or hand-rolled per-Participant origins (mission-style)? Adapter is cheap but
   adds a field-classifier tag contract.
4. **B7 counter primitive.** Build `Manager.MutateWith` (no-phase-change CAS
   mutator) now, or ship B7 with documented-interim `update_triple`?
5. **Group-C (LLM-authored free facts).** B6 leaves the `publishGraphMutations`
   seam for it. Constrained-ID-space Graphable, or stays on the custom envelope?
   Gates full port deletion. On-by-default today, so it is live blast radius.
6. **`state.transition` / `evidence.append` renaming vs compat.** Promote/rename
   now (breaking, post-1.0 discipline) or keep `update_with_triples`/`triple.add`
   subject names and change only semantics (must-exist)? The semantic change
   (must-exist) is the load-bearing part; the rename is cosmetic clarity.
