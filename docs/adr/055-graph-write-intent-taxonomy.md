# ADR-055: Write-Intent Taxonomy — Entity Birth Requires an Envelope (The Mutation API Is Not a Producer API)

## Status

**Proposed** — 2026-06-11. Not yet implemented or tagged. Derived from a
two-pass multi-agent audit + design exercise, then a five-lens adversarial
review (architect / breaker / feasibility / code-accuracy / completeness) whose
findings are folded in below; every load-bearing fact is verified against code.
Review verdict: **READY WITH CHANGES** — the four-lane decision is sound; the
amendments correct miscounts and over-claims an implementer would otherwise build
wrong. Evidence base:

- [`docs/proposals/graphable-bypass-audit.md`](../proposals/graphable-bypass-audit.md)
  — the blast-radius audit (9 producer clusters, 55 call sites, 21 confirmed
  anti-pattern writes / 7 entity types, 0 of 21 overturned under adversarial review).
- [`docs/proposals/graphable-fix-plan.md`](../proposals/graphable-fix-plan.md)
  — the per-entity fix plan + the CAS-flavor decision matrix + the transport-guard
  break-test findings (T1/T2/StorageRef).

**Extends and partially amends** [ADR-049](049-lifecycle-harness-prime-schema-over-entity-states.md):
ADR-049's bucket-ownership rubric and its CAS-on-condition state-write choice both
stand and are **validated**. ADR-055 generalizes ADR-049's insight beyond the
lifecycle harness and amends one blind spot — *non-lifecycle* producers still
birth envelope-less entities via `triple.add`. Builds on
[ADR-047](047-lifecycle-harness-substrate.md) (Participant concept),
[ADR-053](053-agent-run-substrate.md) (agent-run as Participant), and
[ADR-054](054-semantic-indexing-eligibility.md) (the create envelope carries the
`IndexingProfile`). Honors [ADR-028](028-orchestration-architecture.md) and the
KV-twofer.

## Context

### The anti-pattern

SemStreams' founding architecture is `Events → Graphable interface → Knowledge
Graph`: a domain payload implements `graph.Graphable` (`EntityID()` +
`Triples()`), is published as a `BaseMessage`, flows through a JetStream stream,
and graph-ingest's consumer (`extractEntityFromMessage` `component.go:857` →
`MergeEntity` `:946`) writes it to `ENTITY_STATES`, **stamping `MessageType`
provenance** (`:885`). A domain Go type with domain expertise owns each entity's
existence and shape.

In parallel, graph-ingest hosts a **mutation API** for second-writers. The audit
found this API has crept into use as a **producer API**: 21 confirmed call sites
across 7 entity types synthesize a 6-part entity ID and stamp hand-assembled
triples — **creating first-class domain entities with no backing Graphable type
and, often, no `MessageType`**. The mechanism is auto-vivify: `AddTriple`
(`component.go:1411-1423`) and `AddTriples` (`component.go:1511-1521`) both create
a missing entity from a bare `triple.Subject` as `EntityState{Version:0}` with a
**zero-value `MessageType`** — an envelope-less record. The result is invisible,
unclassified graph state: entities born with no producer contract, no domain,
category, version, or indexing profile.

ADR-054's own note named the canonical case: *"The motivating
`agent.agentic-loop.step` entities enter via the mutation API, not the Graphable
path."* It is not isolated.

### What ADR-049 established, and its blind spot

ADR-049 was right about two things this ADR depends on: (1) state lives in
`ENTITY_STATES`, not a private bucket; (2) state transitions need CAS-on-condition
(reject-if-moved), which the append/merge path cannot give, so lifecycle uses
`UpdateEntityWithTriplesRequest.ExpectedRevision`. The lifecycle `Participant`
struct owns the schema via reflection projection and stamps
`MessageType=lifecycle/harness/v1`.

ADR-049 solved entity *classification* for the lifecycle harness — and as a
consequence **lifecycle entities are already envelope-compliant**: `Manager.Create`
births them via `create_with_triples` (atomic create-or-fail) carrying
`lifecycleMessageType`. **The blind spot:** ADR-049 did not generalize the
requirement. A *non-lifecycle* producer that needs an entity has no Participant
struct and reaches for `triple.add` auto-vivify — birthing an envelope-less
entity. ADR-049's rubric answers *where state lives*; it does not answer *what
guarantees an entity's birth carries*. This ADR adds that layer, and the lifecycle
harness becomes the **exemplar** of the pattern, not an exception to it.

### The verified CAS-flavor facts (the foundation)

- **Graphable stream → `MergeEntity`** (`component.go:946`): `UpdateWithRetry` =
  CAS-with-retry, **async, never rejects, APPENDS** on merge (`:1006`, no (s,p,o)
  dedup). Fact-accumulation.
- **`update_with_triples` default** (`ExpectedRevision=0`): CAS-with-retry +
  `MergeTriples` = **REPLACE-by-(subject,predicate)** (gh#244). Already must-exist
  (`mutations.go:530`).
- **`update_with_triples` + `ExpectedRevision>0`** (`updateEntityAtRevision`
  `:1311`): **CAS-on-condition, REJECTS, synchronous**. The state-machine primitive
  `Manager.Transition` uses.
- **`entity.create_with_triples`** (`CreateEntityStrict`): **atomic create-or-fail**
  (`ErrKVKeyExists` on duplicate). One-shot birth.

## Decision

### 1. graph-ingest's entity-birth-and-mutation write surface is a four-lane taxonomy

This taxonomy governs entity **birth and triple mutation**. Fact retraction
(`triple.remove`) and entity death (`entity.delete`) are real, distinct write
intents that fit none of these lanes and are **out of scope** for this ADR (see
Open Question #6). The external operator gateway (semconnect CS-API: bare
`entity.create`/`entity.update`/`entity.delete`) is a projection over these lanes,
reconciled in §2.

| Lane | Ingress | Conflict semantics | Creates? | Envelope |
|---|---|---|---|---|
| **Fact arrival** | Graphable stream consumer | CAS-with-retry, async, never rejects, **appends** | yes (merge/create) | **carried** (registered payload → `MessageType`) |
| **Entity create** | `entity.create_with_triples` | atomic **create-or-fail** | yes | **required** |
| **State transition** | `state.transition` (= `update_with_triples` + `ExpectedRevision`) | **CAS-on-condition, rejects, sync**, replace-by-predicate | no (must-exist) | inherited |
| **Evidence append** | `evidence.append` (= `triple.add` / `add_batch`, made must-exist) | append multi-valued, **must-exist** | **no** | inherited |

Four *intents* over **three subjects + a discriminator**: the State-transition and
mutation-replace cases are the same `update_with_triples` subject discriminated by
`ExpectedRevision` (0 vs >0, `mutations.go:511`). **Only the Evidence-append lane
changes behavior** under this ADR (`triple.add`/`add_batch` gain must-exist);
`update_with_triples` is already must-exist, and the State-transition lane's only
change is the create-envelope requirement (§2). Entities are born **only** through
*Fact arrival* or *Entity create* — both envelope-bearing.

### 2. Entity creation requires a semantic envelope (the core normative rule)

> **No write may create an entity without a semantic envelope** — a registered
> payload type (Graphable, `MessageType` stamped at ingest) for the Fact-arrival
> lane, or an explicit `MessageType` (domain/category/version) on the
> Entity-create lane. The producer contract includes, at minimum: domain,
> category, version, and (per ADR-054) an `IndexingProfile`.

**External operator gateway.** The bare CS-API subjects (`graph.mutation.entity.create`
`mutations.go:40`, `entity.update` `:53`, `entity.delete` `:66`) are a gateway
projection, NOT additional lanes. To preserve this invariant, **bare
`entity.create` is subject to the envelope rule**: either the gateway default-stamps
a `MessageType`/`IndexingProfile` on the `EntityState` before write, or bare
`entity.create` is deprecated for external creates in favor of
`entity.create_with_triples`. Bare `entity.update` (must-exist replace,
`mutations.go:411`) is a gateway projection over the State-transition lane.
Without this, an external POST through bare `entity.create` can still birth an
envelope-less entity — the one ingress that would otherwise defeat §1.

### 2.1 Why the transition lane is request/reply, not a stream

A state transition's defining need is **synchronous conditional reject** ("apply
iff still at rev R; tell me *now* if I lost so I re-read and retry") — what
`Manager.Transition`'s loop runs on. A JetStream stream is async fire-and-forget;
you don't synchronously learn whether the CAS won. You *can* stream it via
`Nats-Expected-Last-Subject-Sequence`, but that requires one subject per entity,
making ENTITY_STATES a projection of a transition stream — event-sourcing state,
KV stops being the source of truth, restart means replay-rebuild. The KV-twofer
already gives durable transition audit via revision replay (ADR-049 G4), so
streaming buys nothing here at large cost. The transition lane belongs in the RPC
family — and already exists as `update_with_triples`+`ExpectedRevision`.

### 3. `triple.add` and `add_batch` lose auto-vivify-create power (must-exist)

`triple.add` / `add_batch` become **must-exist**: they reject "entity not found"
when the Subject is absent. **Both** auto-vivify-create branches are removed —
`AddTriple` (`component.go:1411-1423`) AND `AddTriples` (`component.go:1511-1521`).
This matters: the 7 conjurors reach the footgun via
`graph.mutation.triple.add_batch` → `AddTriples` (`decide.go:723`,
`write_todos.go:440`, `triplepub.go:99`, `scratchpad.go:217`, `emit_diagnosis.go:199`),
NOT the singular `AddTriple` — deleting only `:1417` would ship the footgun live
on the batch path. (`graph/datamanager.Manager.AddTriple`, `edge_ops.go:16`, is a
distinct GetEntity-first implementation already must-exist-shaped; do not conflate.)

**This converts envelope-less producers into fail-fast errors** while
append-to-existing survives (renamed `evidence.append` for intent). But the
"derived-fact stampers are uniformly unaffected" claim is **not** uniformly true;
three carve-outs:

- **Governance deny/approve audit (a must-exist casualty — see §3a).** Not all
  ~26 cleared stampers target an existing Graphable-origin entity.
- **In-process inference/applier bypass.** `tripleAdderAdapter.AddTriple`
  (`component.go:159-161`) calls `Component.AddTriple` in-process (wired at `:660`),
  invoked at `hierarchy.go:239/:313/:368` and `applier.go:193` — writing inverse
  edges onto *different* subjects (siblingID/containerID) than the one being
  ingested. These normally target pre-existing entities (siblings via
  `ListWithPrefix`; containers pre-created via `ensureContainerExists`→`CreateEntity`),
  but under must-exist any ordering race flips from silent no-op to fail-fast. A
  Wave-0 audit must grep every in-process `Component.AddTriple` caller and confirm
  none relies on auto-vivify ordering — assert it, don't assume it.
- **Subject-override cross-entity stamps.** The `add_triple` rule action supports
  `action.Subject` override (`resolveTripleSubject`, `actions.go:576`) targeting an
  entity OTHER than the trigger. The shipped `configs/rules/example-fan-out/02-stamp-completion-on-parent.json`
  stamps a counter onto a B1-conjured parent loop-execution entity; under must-exist
  the parent's origin must land first. Naturally satisfied by causal ordering
  (parent spawns before child completes) AND by the closing-move sequencing (B1
  lands before the flip), but the example-fan-out pack is explicitly **gated on B1**,
  and the parent origin (`WriteSpawnIdentity`) is best-effort fire-and-forget
  (`graph_writer.go:485`) — a dropped origin converts silent auto-vivify to a hard
  child-counter fail.

**Precise claim:** must-exist is safe for stampers targeting an
independently-existing Graphable-origin entity (rule-engine trigger-entity stamps,
inference-applier edges between pre-existing 6-part entities). The ~8 loop-derived
stampers become safe only after B1 + origin-first ordering. The governance-audit
writes (§3a) and the in-process inference bypass need explicit handling.

**Ordering consequence:** an entity's origin must land before any derived stamp or
transition on it — a fail-fast precondition, not a silent self-heal. Each
conjuror's origin write must be promoted from best-effort to error-propagating
when it becomes the must-exist precondition. Per-producer ordering is in the fix
plan §6–7. An Open Question (#7) records whether benign origin races warrant a
bounded must-exist retry-with-backoff vs hard fail-fast.

**Observability.** "Loud fail-fast" is operationalized: a Wave-0 rejection metric
labelled by subject + reason, plus a structured log, per the ADR-054 cost-ledger
discipline. Every conjuror migrating to a mutation lane becomes a new
`natsclient.Request` caller and MUST check the `error: <msg>` response-payload
prefix (the body-prefix convention) or silently treat rejection as success — the
silent-handler-error class that has shipped 3×. State this as a Wave-0 invariant.

### 3a. Governance deny/approve audit: a must-exist casualty, migrated before the flip

`executeDeny` (`actions.go:1390-1418`) and `executeApprove` (`:1435-1467`) write an
audit triple with `Subject = ec.RuleID()` via `tripleMutator.AddTriple` →
`graph.mutation.triple.add` → `Component.AddTriple`'s auto-vivify branch — the only
write path that skips `validateEntityID`. Rule IDs are bare slugs (zero dots:
`role-gate-rule`, `architect_complete_spawn_editor`), so they fail the 6-part
`entityIDRegex` and **can take no birth lane**. The write is best-effort: on
failure both callers "intentionally fall through — verdict is structural"
(`:1414`, `:1465`) and only log. `configs/agentic.json` uses `approve` in
production. **Under must-exist, the first deny/approve on a never-seen rule-ID
rejects "entity not found" and the ADR-039 audit triple silently vanishes.**

**Decision: a dedicated append-only verdict-EVENT store, not a mutable bucket.**
A verdict is an *event* (N per rule over time, per [[feedback_separate_contract_from_run]]:
the rule is the contract, each verdict is a run/event) — not state keyed by rule.
Two wrong shapes to reject explicitly: (a) it does **not** belong in `ENTITY_STATES`
(rule-IDs are not domain entities; they fail the 6-part contract and would pollute
inference/search), and (b) it must **not** be a KV bucket keyed by `ruleID` with
`History:1` — that is "last-verdict-wins," which silently *recreates* the audit
loss this section exists to prevent. The current audit triples are written to a
phantom non-6-part rule-ID "entity" and have **no code readers** (verified: only
`PredicateRuleDeny`/`PredicateRuleApprove` consts exist; ADR-039's "operators query
the graph" reads these phantom entities). The verdict is *already* an event —
`executeApprove` already publishes one to a routing subject — so the audit triple
is a redundant parallel record.

**The concrete contract (§3a must ship this, not a bare "bucket"):**

- **Store.** A dedicated JetStream stream `GOVERNANCE_VERDICT_AUDIT`, subject
  `governance.verdict.{decision}.{rule_token}` (`decision` ∈ `deny`|`approve`).
  **`rule_token` is a subject-safe stable encoding of the rule ID — NOT the raw
  `rule_id`.** Rule IDs are free-form config strings (`Definition.ID`,
  `rule_factory.go:16`; validated non-empty only, `expression_factory.go:285`), so a
  raw ID can contain `.` (the NATS token separator), spaces, `*`/`>` (wildcards), or
  future product-namespaced IDs — any of which break subject publishing AND
  filtering. `rule_token` is a deterministic hash or token-encoding (e.g. a short
  hash); the **canonical `rule_id` is carried in the payload** for display/query.
  **Append-only by construction** — each verdict is a new stream sequence; no key to
  overwrite, so last-verdict-wins is structurally impossible. Retention = `MaxAge`
  sized to the audit/compliance horizon — its own knob, the reason for a dedicated
  stream rather than riding AGENT. (Considered alternative: KV keyed by a unique
  event ID, written once so `History` is moot — also append-safe, but the key would
  need the same `rule_token` encoding; the stream is preferred because the verdict is
  already an event and ADR-039 values JetStream history for auditability.)
- **Payload (envelope-compliant per §2).** A *registered* verdict-event payload
  type — `domain=governance, category=verdict, version=v1` — published through the
  payload registry, so the audit record carries its own producer contract. Schema:
  `{decision, rule_id, reason, timestamp, entity_id, loop_id?, call_id?}`. The
  always-present fields are at the call site: `ruleID`, `reason`, `EntityID`.
  **`loop_id`/`call_id` are OPTIONAL and sourced from `ExecutionContext.MessageData`
  (`execution_context.go:135`), NOT the routing subject** — `executeDeny` has no
  subject (`actions.go:1390`), and `MessageData` is nil for entity-state-driven and
  cron-fired rules. Emit them when present (omit otherwise); do not assume a
  tool-call message is always in scope.
- **Emit ownership — a dedicated `VerdictAuditor` dependency, not the operator
  `Publisher`.** The framework emits the audit event on **both** deny and approve
  via an explicit, framework-owned `VerdictAuditor` (emitter) interface injected
  into the `ActionExecutor` — distinct from the operator-configured routing
  `Publisher`. This is deliberate: piggybacking the operator `Publisher` would let
  config drift (a missing/misrouted publish target) accidentally disable the audit
  trail. The `VerdictAuditor` is always wired by the rule processor; audit is a
  framework guarantee, not an operator opt-in. This **replaces** the
  `AddTriple(ruleID, rule.deny/approve)` write entirely — so `triple.add` leaves the
  governance path and must-exist never touches it. The operator's routing publish to
  the dispatcher (via `Publisher`) is unchanged.
- **Failure semantics (preserve ADR-039 + make loss observable).** The audit emit
  stays **best-effort — a failed emit MUST NOT flip the verdict** (deny stays
  terminal, approve stays permissive; `actions.go:1414`/`:1465`). But add a
  `governance_verdict_audit_failures_total{decision,mode}` counter + Error log + a
  health signal, so lost audit records are observable — critical in `enforce` mode,
  where a silent audit gap on an enforced denial is a compliance hole.
- **Eligibility (resist scope-creep into "governance state").** This store is for
  rule/tool-call **verdict events only** — non-domain events over rule/call IDs;
  **not** semantic entities, **not** inputs to graph inference/search/rules. The
  name is `GOVERNANCE_VERDICT_AUDIT` (not a generic "governance bucket") precisely
  so it cannot become a side channel for facts that are inconvenient to model. A
  governance fact that needs graph-query semantics is born as a real entity via a
  birth lane, or mirrored intentionally (explicit, ADR-054-indexing-profiled) —
  never defaulted here.
- **Read path.** Operators read verdict history by replaying/filtering the stream
  (`governance.verdict.deny.{rule}` etc.) via a governance gateway query, replacing
  the phantom-rule-ID-entity path. (No code reads the triples today, so no in-repo
  reader migrates; the operator-facing graph query in ops-doc 17 is redirected.)
- **Migration (before the closing-move flip).** (1) register the verdict-event
  payload type + provision the stream; (2) swap `executeDeny`/`executeApprove`'s
  `AddTriple` for the framework verdict-event emit; (3) update the tests asserting
  triple writes (`processor/rule/deny_integration_test.go:329` and the approve
  counterpart) to assert the stream event; (4) update
  `docs/operations/17-tool-call-governance.md:155-179` (promises `rule.deny`/
  `rule.approve` triples + "operators query the graph"); (5) regression test
  asserting a verdict event lands for every deny/approve.

**This amends [ADR-039](039-tool-call-governance-rule-driven.md)'s audit mechanism**
(triple-on-rule-ID → verdict-event-on-stream) while preserving its goal: an
explicit, queryable verdict audit ("show me every tool call we explicitly denied").
ADR-039's "auditability via the graph" rested on phantom rule-ID entities; the
stream is the honest home for an append-only event log.

### 4. The per-predicate decision matrix (classify predicates, not entities)

Most entities are **hybrids** whose predicates split across lanes:

```
For each predicate of an entity:
  1. Phase-transition race or read-modify-write counter race?
        YES → State transition (CAS-on-condition). A Participant predicate.
  2. Single-valued AND re-emitted onto the same Subject over the entity's life?
        YES → update_with_triples default (MergeTriples replace-by-(s,p)).
  3. Else (born-once-and-never-re-emitted identity, accumulating links, immutable bodies)
        → a birth lane: Fact arrival (non-Participant, with guards §5) OR
          Entity create (Participant identity, §6).
```

A **mutable single-valued predicate may NOT ride the Fact-arrival stream** — the
stream appends, so `GetFieldValue` first-match would read the stale value
(beta.103 class). `mission.Command`'s re-emitted `mission.command` (launch→abort)
is the canonical example: such predicates belong on the replace lane, not the
stream.

**No fourth "replace mode" is added to the stream consumer.** The replace need is
served by `update_with_triples` default. The primitive set {append-stream + guards,
mutation-replace, CAS-on-condition} is complete *on the create/append/replace/
transition axis* — a deliberate-completion decision
(`feedback_reactive_patches_vs_engine_completion`), not a claim that the four lanes
exhaust every NATS verb (retraction/death are out of scope, §1).

### 5. Transport guards the Fact-arrival lane requires (preconditions, not options)

The break-tests proved the Fact lane unsafe without these. They apply to
**non-Participant** Fact-arrival entities (trajectory-step, web-observation,
operating-model bodies, loop-execution); Participant identity rides the
create-or-fail lane (§6) and is exempt.

- **T1 — idempotent publish + dedup window (THREE parts).** `PublishToStream`
  sets no `Nats-Msg-Id` (`client.go:793-799`); the consumer is at-least-once and
  `MergeEntity` appends. Wave 0 must deliver: **(a)** `PublishToStreamWithMsgID`
  with a deterministic ID (`loopID:spawn`, `entityID:vN`); **(b)** a `Duplicates`
  field on `config.StreamConfig` (`config/streams.go:19-26` has none today) plumbed
  into BOTH stream builders (`config/streams.go` createStream, `natsclient/stream.go`
  ensureStreamForConsumer), defaulted to the recovery horizon; **(c)** an explicit
  UpdateStream-or-recreate decision for already-existing prod streams (AGENT,
  entity-ingest) — `createStream` only calls `UpdateStream` on subject changes
  (`config/streams.go:380-388`), so a window-only change needs a new path. **Without
  (b)+(c) the window is the NATS server default of 2 minutes** — adequate for
  steady-state redelivery, shorter than the `DeliverPolicy:all` restart-replay
  horizon. The window size is now a Wave-0 config default, so Open Question #1 is a
  **must-decide-in-Wave-0** item, not deferred. Because the consumer is durable with
  a stable name, ordinary restarts resume from last-ack; the exposure is
  consumer-recreation / fresh-deploy catch-up replay, which can re-append born-once
  predicates outside the window — see §7-G3 and Open Question #1 ((s,p,o)-idempotent
  merge is the belt-and-suspenders fix).
- **T2 — single-Subject Graphable (target invariant + exception list, not a
  universal fact).** `extractEntityFromMessage` files all `Triples()` under one key
  = `EntityID()` with no Subject regrouping (`component.go:882-887`); a triple whose
  `Subject != EntityID()` is silently misfiled. **Known violators today:**
  `sensorml.Asset` (deliberate inverse `isHostedBy` edge on the child subject,
  `graphable.go:124`), `federation.EventPayload` (`event_payload.go:41`),
  `objectstore.StoredMessage` (`stored_message.go:143`, verbatim pass-throughs).
  Therefore: **WARN-and-regroup-by-Subject** in `extractEntityFromMessage` (mirror
  the `AddTriples` `bySubject` split at `component.go:1478-1484`) so cross-entity
  edges land on the right key — NOT a hard reject. New single-Subject Graphables are
  the target; the regroup keeps existing multi-Subject producers correct.
  (`sensorml.Asset` is currently unwired — no `MarshalJSON`, no payload registration
  — so the regression is latent-on-wiring, not live-on-main; that is why the guard
  must regroup rather than reject before the ADR-044 Phase 5 bridge lands.)
  **Sibling consumer — `graph/messagemanager` (DEAD, reclassified 2026-06-12).**
  `Manager.ProcessMessage` (`graph/messagemanager/processor.go:274`) is a second
  Graphable→EntityState consumer that files all `triples` under one `actualEntityID`
  with the same single-key misfiling (it ALREADY lifts `StorageRef` via the
  `Storable` assertion `:200-204`). The Wave-0 caller audit found it has **zero
  non-test importers in semstreams** — it is dead/legacy, not a live ingest path.
  Reclassified from "must regroup" to **removal candidate**: do NOT fix
  speculatively (per `feedback_framework_vs_product_boundary` — unwired ≠ broken).
  If a sister product wires it, it carries the same T2 defect, but that is its
  call to make.
- **StorageRef extraction (two halves).** Consumer side: `extractEntityFromMessage`
  must type-assert `ContentStorable` and set `StorageRef` (MergeEntity merges it on
  the existing branch, `:1008-1009`). Producer side: the canonical pilot
  `TrajectoryStepEntity` stores its ref in an **unexported** `storageRef` field
  (`trajectory_entity.go:22`) that won't JSON-round-trip — it needs an exported,
  round-tripping field + offload-before-publish ordering, with a production-decoder
  round-trip test.

### 6. Lifecycle: already envelope-compliant; identity born via the create-or-fail lane

ADR-049's CAS-for-transitions is **correct and stays** — the State-transition lane
used as designed; lifecycle is the **exemplar**. And lifecycle entities are
**already envelope-compliant**: `Manager.Create` births them via
`create_with_triples` (Entity-create lane, create-or-fail) carrying
`lifecycleMessageType`. So lifecycle is not a violator of §2's invariant.

The one enrichment this ADR proposes: give a Participant a **richer typed envelope**
(per-Participant domain/category/version + `IndexingProfile`) instead of the generic
`lifecycleMessageType`, by having `Manager.Create` build its `create_with_triples`
payload from a registered `lifecycle.ParticipantEntity` adapter rather than the
generic reflection projection. **Critically, this origin is born on the Entity-create
lane (create-or-fail), NOT the Fact-arrival append stream.** That binding (the
review's load-bearing correction) has three consequences:

- **No field-classifier is needed.** A one-shot create-or-fail cannot duplicate
  single-valued predicates, so the adapter may include the full initial triple set
  exactly as `buildInitialTriples` does today (identity + initial phase + audit).
  The "exclude every single-valued predicate" rule that an *append-stream* origin
  would have required does not apply here.
- **T1 is moot for Participants.** Create-or-fail is not the append path, so there
  is no redelivery-duplication exposure; the §5 guards govern only non-Participant
  Fact-arrival entities.
- **agent-run reaches parity with mission.** `AgentRun` (`agentrun.go`, ADR-053)
  is created via `Manager.Create` like mission, so it is *already* enveloped; the
  adapter upgrades its envelope from generic `lifecycleMessageType` to a typed
  `agent.run.*` payload. This is an enhancement, not a fix.

**Mission, honestly framed.** Mission demonstrates the State-transition lane (CAS)
and a single-Subject stream Graphable (`mission.Command`) in an **e2e harness** —
but `mission.Command.Triples()` stamps a *command* predicate (`mission.command`,
`command.go:71-76`), not the Participant identity (born via `Manager.Create` on the
create-or-fail lane), and it publishes on the un-guarded `PublishToStream` path
(`command.go:318`) — a **T1 target to fix, not a T1 exemplar**. No production
Participant yet has a typed identity envelope via the adapter. **The hybrid is
therefore DESIGNED and validated against the guarantee table (§7), not PROVEN in
production.**

### 7. Guarantee-preservation walkthrough (validated against ADR-049's seven guarantees)

With Participant identity on the create-or-fail lane and transitions on CAS:

| Guarantee | Verdict |
|---|---|
| **G1** reject-on-mismatch transitions | PRESERVED by construction — transitions ride `ExpectedRevision`, never the stream. |
| **G2** atomic phase+audit landing | PRESERVED — one Transition delta under one `ExpectedRevision`. |
| **G3** create-or-fail race | PRESERVED **natively** — `Manager.Create` has two race-safe paths: absent → `CreateEntityStrict` (atomic create-or-fail, `ErrKVKeyExists`); present-without-phase → `update_with_triples`+`ExpectedRevision` attach (rejects concurrent attach). Identity on the create-or-fail lane means **G3 no longer depends on T1** (the prior append-stream framing did). |
| **G4** History fidelity (revision replay) | PRESERVED — transitions stay off the append stream; bounded per-transition revisions. |
| **G5** replace-not-append single-valued phase (beta.103) | PRESERVED — phase/audit/counter scalars ride CAS-replace, never the append stream. The beta.103 break is `extractTripleScalar` last-match vs rule-engine `GetFieldValue` first-match under blind-append; keeping single-valued predicates off the append path is the fix. |
| **G6** provenance (`MessageType`) | PRESERVED and **enriched** — the typed adapter payload carries a per-Participant envelope, stamped at create. |
| **G7** transition-table + terminal/drift validation | PRESERVED — runs synchronously in the req/reply loop. |

For **non-Participant** Fact-arrival entities (B1 loop-execution, B2
trajectory-step, etc.), born-once identity safety holds **iff** the origin publish
is T1-idempotent AND redelivery stays within the JetStream Duplicates window;
`DeliverPolicy:all` replay outside the window (consumer recreation / fresh-deploy
catch-up) re-appends born-once predicates unless `MergeEntity` gains
(s,p,o)-set-idempotent merge — see Open Question #1. Either promote (s,p,o)-idempotent
merge to a Wave-0 primitive alongside T1, or scope the guarantee to "within the
Duplicates window" with the accepted bounded-re-merge tradeoff. Do not present T1
as unconditionally sufficient.

## Migration

Sequenced waves (per-entity detail in the fix plan §7–8). **Closing-move ordering:**
migrate every producer to a birth lane first, then flip `triple.add`/`add_batch` to
must-exist last, so `main` is never broken in between.

- **Wave 0 — framework primitives:**
  - **B-1 governance-audit migration** (§3a) — **[IMPLEMENTED]** moved
    `rule.deny`/`rule.approve` from the rule-ID triple write to the
    `GOVERNANCE_VERDICT_AUDIT` verdict-event stream (registered
    `governance.verdict.v1` payload in the new `governance` package, append-only,
    subject `governance.verdict.{decision}.{rule_token}`) via a framework-owned
    `VerdictAuditor` injected into the rule `ActionExecutor` (independent of the
    operator `Publisher`). Adds `semstreams_rule_governance_verdict_audit_failures_total{decision}`,
    deletes the dead `PredicateRuleDeny`/`PredicateRuleApprove` consts, and
    redirects the ops-doc-17 read path to stream replay. The metric `mode`
    dimension from the §3a sketch is deferred — the governance mode
    (`audit`/`enforce`) is a loop-level setting not threaded to the rule-action
    emit site, so a `mode` label would be permanently `unknown`; revisit if the
    mode is propagated into the proposed-call message. Gates the closing move.
  - **T1 (three parts)** — `PublishToStreamWithMsgID` + `config.StreamConfig.Duplicates`
    plumbed into both builders + the existing-stream update path; decide the window
    size (Open Question #1). Optionally (s,p,o)-idempotent merge in `MergeEntity`.
  - **T2** WARN-and-regroup-by-Subject + **StorageRef extraction** (both halves) in
    `extractEntityFromMessage`.
  - Lane naming + the §3 rejection metric + the body-prefix-error Wave-0 invariant.
  - In-process `Component.AddTriple` caller audit (§3).
  - **ADR-054 Phase 1** (IndexingProfiler, lenient) so birth envelopes carry it.
- **Wave 1 — pilots:** model-endpoint (mutation-replace pilot), trajectory-step
  (Fact-arrival + ContentStorable pilot).
- **Wave 2:** loop-execution. **Precondition:** the agentic graph-ingest input is
  `sensor.processed.entity` only (`configs/agentic.json`) — agentic entities reach
  graph-ingest via `graph.mutation.*` today, so B1/B2 must ADD a graph-ingest
  entity-stream input + an agentic-loop publish leg to every agentic config. Surface
  this in B1/B2 effort.
- **Wave 3 — replace siblings:** web-observation, operating-model family.
- **Wave 4 — lifecycle enrichment:** the `ParticipantEntity` adapter (+ the
  `lifecycle:"origin"` tag contract if the adapter is taken generic — Open Question
  #2), then agent-run's typed envelope, then the github-pr-workflow example. **Gated
  on ADR-053 landing** for the agent-run leg; the adapter + example can ship
  independently if ADR-053 slips.
- **Closing move:** delete BOTH auto-vivify-create branches
  (`component.go:1411-1423` and `:1511-1521`); flip `triple.add`/`add_batch` to
  must-exist. Gated on a relevant e2e tier green (breaking-change hard rule) and on
  the **LLM-authored free-facts** path (audit §4.7, on-by-default) being routed
  through a constrained-ID birth lane or quarantined — it must not silently fail the
  flip.

### Backward compatibility — existing envelope-less entities

The must-exist flip governs only **future** births; the ~21 already-conjured records
in `ENTITY_STATES` (LLMAssisted defaults on) persist with `MessageType`=zero. The
**indexing** dimension is already covered by ADR-054's lenient-default + stamp-on-touch
sweep (ADR-054 lines 282-288, 354-356) — cross-reference, don't re-solve. The
**provenance** dimension (MessageType/domain/category/version) is not covered by
ADR-054's profile backfill: either (a) fold a provenance stamp into ADR-054's
stamp-on-touch sweep (one pass, both predicates), (b) a one-time sentinel
(`legacy/unclassified/v0`) backfill, or (c) accept legacy records stay
`MessageType`-less with documented reader behavior (any `message_type`-branching
reader — including ADR-054's dry-run report — must tolerate empty), noting
self-retiring types (loop/trajectory die with `COMPLETE_*`) shrink the durable
residual. Decide before the flip.

## Consequences

### Positive

- **Unclassified entity birth becomes unrepresentable** (for future births; see
  Backward compatibility for the existing population, and §1 for the
  retraction/death scope boundary — this guarantee is about birth, not death).
- **State keeps its correct primitive.** CAS-on-condition transitions stay
  first-class (validates ADR-049). No state is forced onto an event-shaped wire.
- **The catalog unifies.** agent-run gains a typed envelope like mission; the 7
  conjured types gain typed schema owners.
- **The footgun is removed at the source** — `triple.add`/`add_batch` can no longer
  birth an entity; the failure mode is a metered fail-fast, not silent invisible state.
- **ADR-054 coupling is natural** — the birth envelope is where `IndexingProfile` lives.

### Negative (accepted tradeoffs)

- **Producer-side migration cost** across 7 entity types + one example; operating-model
  is an L-effort four-type conversion.
- **Origin-first ordering becomes a hard precondition** per conjuror (was a silent
  self-heal). The integrity gain restated as a cost.
- **Fact-lane hardening is mandatory before anything rides it** (T1 three-part / T2
  regroup / StorageRef).
- **Governance-audit relocation** is required before the flip (§3a).
- **`state.transition` / `evidence.append` renaming is breaking** if done as a subject
  rename (post-1.0 discipline); the load-bearing change (must-exist) is separable from
  the cosmetic rename.

### Neutral / changed

- `update_with_triples` + `ExpectedRevision` is unchanged in mechanism — only promoted
  in name and given a mandatory create envelope.
- The Graphable stream consumer is unchanged except the additive T1/T2/StorageRef guards.
- Lifecycle Participants are unchanged in birth mechanism (already create-or-fail +
  envelope); the adapter only enriches the envelope.

## Open questions deferred

1. **JetStream dedup window vs restart replay (T1) — must decide in Wave 0.** Size the
   `Duplicates` window to the recovery horizon, accept bounded re-merge, or add an
   (s,p,o)-idempotent merge guard in `MergeEntity`? The window size is now a Wave-0
   config default.
2. **`ParticipantEntity` adapter scope (Wave-4 prerequisite).** Generic adapter
   framework-wide (needs a `lifecycle:"origin"` field-classifier tag with parse-time
   `origin ⇒ readonly+born-once` validation + a lint that no phase/audit predicate is
   origin-tagged) vs hand-rolled per-Participant origins (mission-style). The classifier
   is a prerequisite for the generic adapter, not deferrable past Wave 4.
3. **A no-phase-change CAS mutator (`Manager.MutateWith`)** for RMW counters — build now
   or ship the example with a documented interim?
4. **LLM-authored free facts (audit §4.7) — hard gate on the closing move.** The purest
   conjure (uncontrolled ID space, on by default) must be routed through a constrained-ID
   birth lane or quarantined before must-exist lands.
5. **Rename vs compat** for the two must-exist lanes — promote/rename now or change only
   semantics under existing subject names?
6. **Retraction/death lane.** `triple.remove` of a single-valued phase/audit predicate
   can corrupt a transition guard the same way append did (beta.103 G5); `entity.delete`
   has no lane. Does delete/remove need must-exist + CAS-eligibility + envelope-inheritance,
   or a separate governed lane? Deferred.
7. **Benign origin races** — when an origin lands late, is a bounded must-exist
   retry-with-backoff warranted vs hard fail-fast? Affects every conjuror's derived
   stampers.

## Relationship to other ADRs

- **Extends and partially amends** [ADR-049](049-lifecycle-harness-prime-schema-over-entity-states.md).
  Its bucket-ownership rubric and CAS-on-condition state-write choice stand and are
  validated; rubric item 5 ("don't need fine-grained CAS") remains the per-predicate
  lane test. ADR-055 generalizes the schema-owner principle beyond lifecycle, adds the
  write-intent taxonomy + envelope-on-create requirement, and frames lifecycle as the
  exemplar (already create-or-fail + enveloped).
- **Completes** [ADR-053](053-agent-run-substrate.md). agent-run is born via
  `Manager.Create` (enveloped); the typed adapter upgrades its envelope. The Wave-4
  agent-run leg is **gated on ADR-053 landing**.
- **Couples to** [ADR-054](054-semantic-indexing-eligibility.md). The birth envelope
  carries `IndexingProfile`; Wave 0 sequences ADR-054 Phase 1 first. The existing-entity
  provenance backfill should ride ADR-054's stamp-on-touch sweep.
- **Amends** [ADR-039](039-tool-call-governance-rule-driven.md)'s audit mechanism
  (§3a): the `rule.deny`/`rule.approve` triple-on-rule-ID becomes a registered
  verdict event on the `GOVERNANCE_VERDICT_AUDIT` stream. ADR-039's explicit-verdict-
  audit goal is preserved; its phantom-rule-ID-entity read path is retired.
- **Honors** [ADR-028](028-orchestration-architecture.md) and **reinforces** the
  single-writer-to-ENTITY_STATES invariant and the KV-twofer.

## What this ADR is NOT

- **Not "everything must be Graphable."** State legitimately needs CAS-with-reject the
  event-shaped Graphable stream cannot give. The invariant is envelope-on-birth.
- **Not a reversal of ADR-049.** It validates ADR-049's CAS-for-state and extends its
  schema-owner principle; the lifecycle harness is the exemplar.
- **Not a claim the mutation API is wrong.** Derived-fact stamps, state transitions, and
  the external operator gateway are first-class — but the gateway's bare `entity.create`
  is brought under the envelope rule (§2), and *envelope-less entity creation* (`triple.add`/
  `add_batch` auto-vivify) is retired. Retiring auto-vivify also affects the rule-ID
  governance audit (§3a), which is migrated, not left in the "untouched" set.
- **Not the per-entity implementation.** B1–B7 designs live in
  [`graphable-fix-plan.md`](../proposals/graphable-fix-plan.md).

## References

- Evidence base: the two proposals above (audit + fix plan).
- Code anchors:
  - `processor/graph-ingest/component.go:857` (`extractEntityFromMessage`), `:946`
    (`MergeEntity`), `:1006` (append), `:1311` (`updateEntityAtRevision`), `:885`
    (`MessageType` stamp), `:1411-1423` (`AddTriple` auto-vivify — deleted) and
    `:1511-1521` (`AddTriples`/add_batch auto-vivify — deleted), `:1478-1484`
    (`AddTriples` bySubject regroup — the T2 model)
  - `processor/graph-ingest/mutations.go` (subjects + handlers; `:40/:53/:66` bare CS-API)
  - `processor/rule/actions.go:1390-1467` (deny/approve), `:576` (subject override)
  - `natsclient/client.go:793-799` (`PublishToStream`, the T1 gap)
  - `config/streams.go:19-26` (`StreamConfig`, no `Duplicates`)
  - `pkg/lifecycle/manager.go` (`Create`, `Transition`), `pkg/lifecycle/projection.go:202`
    (`projectStructToTriples`), `pkg/lifecycle/tags.go:14-67` (`fieldMeta` — no origin classifier)
  - `parser/sensorml/graphable.go:124`, `federation/event_payload.go:41`,
    `storage/objectstore/stored_message.go:143` (T2 exceptions)
- Discipline memories applied: `feedback_write_api_taxonomy_envelope_on_create`,
  `feedback_bucket_ownership_rubric`, `feedback_reactive_patches_vs_engine_completion`,
  `feedback_lifecycle_transition_replace_not_append`, `feedback_silent_handler_error_payload_audit`,
  `feedback_e2e_required_for_breaking_changes`.
- Principles: `docs/concepts/02-kv-twofer.md`, `docs/concepts/15-payload-registry.md`,
  CLAUDE.md "Architectural Identity (Not an Event Bus)".
