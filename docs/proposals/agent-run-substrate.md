# Agent-Run Substrate — Nested Agentic Loops as a Lifecycle `Participant`

## Status

**PROMOTED to [ADR-053](../adr/053-agent-run-substrate.md) (2026-06-07).** This
proposal remains the full design-exercise record; the ADR is the authoritative
decision. Authored 2026-06-07 from the semteams #225 follow-up thread. Two independent adversarial reviews complete
(architect + Codex), both verdict **RIGHT-WITH-CHANGES, thesis accepted**; this
document is the single-narrative rewrite around the resolved design (the earlier
body + appendix split was replaced per Codex P1). All API claims verified
against HEAD (`pkg/lifecycle` post-ADR-049). Companion memory:
`project_agent_run_chain_framework_gap`. Review history at the end.

## Summary

A coordinator agentic loop that spawns research/architect/builder child loops
forms a **run** (semteams calls it a "chain"): a tree of nested loops spanning
multiple arcs, accumulating cross-arc state. The framework has **no first-class
primitive** for it — only an unused `ChainExecutionEntityID` constructor
(`agentic/entity_ids.go:139`). semteams (our reference-design product) hand-rolls
the whole run layer (~600 LOC of `cmd/semteams/chain/*`).

**Decision:** model the run as a **Lifecycle harness `Participant`**
(`pkg/lifecycle`). The run has a genuinely *declarable generic* lifecycle, so it
reuses the harness Manager/recovery/operator-gateway/rule-integration wholesale.
The agentic *loop* stays excluded from `Participant` (per-iteration LLM
dynamism, high-write `AGENT_LOOPS` bucket — ADR-047); only the *run* — low-write,
declarable — participates. This is exactly the case ADR-047 left the door open
for: *"a future agentic role with declared phases could implement `Participant`
if the fit emerges."*

The net-new framework code is a **thin agentic adapter**: an `AgentRun`
Participant, a structurally-declared mint trigger, typed `run_id` propagation,
and a completion-event milestone subscriber. The one genuine redesign — typed
`run_id` inherited at spawn — collapses *two* parallel run-identity mechanisms
into one and retires a race the framework has already closed.

## The gap

| Construct | Status |
|---|---|
| `LoopExecutionEntityID`, `ChainExecutionEntityID` (`entity_ids.go`) | ID constructors — HAS (chain one never called by framework) |
| `LoopEntity.ParentLoopID`, `agent.loop.parent` at spawn (`graph_writer.go:451` `WriteSpawnIdentity`) | walkable loop-tree spine; gh#159 race closed — HAS |
| **The run as a first-class entity** | minted nowhere, owned by no one, no resolver, no operator view — **MISSING** |

### The reference design has TWO run-identity mechanisms (load-bearing)

1. **Ancestry-walk chain (ADR-038):** `chain_id` = dispatch-root loop UUID, found
   by `Resolver.ChainID` walking `agent.loop.parent` (`chain/resolver.go:97`,
   `maxAncestryHops=64`). Milestone-stamping spine.
2. **Historical related-loop run-anchor thread (retired):** the reference design
   pinned an untyped run entity ID at the root and re-threaded it through every
   spawn rule. The framework contract replaces that mechanism with
   `agent.loop.run` (bare run ID) and `agent.run.entity-id` (full entity ID).

The thread (#2) exists to dodge the gh#159 completion-time race — **which the
framework already closed**: `WriteSpawnIdentity` (`graph_writer.go:451`) now
stamps `agent.loop.parent` atomically *at spawn*, godoc citing the exact fix.
So #2 is partly vestigial. A typed `run_id` set at spawn collapses both
mechanisms plus the read-side walk.

## Why framework, why now

- **semteams is the reference-design product**, driving new semstreams patterns;
  its run layer is proven (smoke #8/#13, ADR-038). Lift the structured contract
  from the reference product (`feedback_lift_structured_contract_not_friendly_projection`).
- **Engine gaps file as engine work** (CLAUDE.md orchestration boundaries). The
  run is a coordination primitive; leaving it product-side means the next
  agentic product re-plumbs it (and is what produced #225's wire-field asks).
- Pre-1.0, we own every consumer; take the break (`feedback_greenfield_cross_product_break_now`).

## Design

### 1. `AgentRun` — a `lifecycle.Participant` (agentic-side, imports `pkg/lifecycle`)

```go
// AgentRun is the coordinating envelope over a nested-loop tree.
// Lives in an agentic-side package that imports pkg/lifecycle (legal
// direction; the harness MUST NOT import agentic — ADR-047:133).
type AgentRun struct {
    // Identity: the lifecycle:"id" field is populated by projection FROM
    // THE FULL ENTITY-STATE KEY, not a triple (pkg/lifecycle/projection.go).
    // So it MUST hold the full 6-part ID; the bare run loop-id is DERIVED.
    EntityID_ string `json:"-" lifecycle:"id"` // c360.<plat>.agent.chain.execution.<loopID>
    Phase_    string `json:"phase"`
    ParentRunEntityID string `json:"parent_run_entity_id,omitempty"`
    // NOTE: no milestone fields here — milestones are product triples on the
    // run entity, NOT projected Participant fields (see §5).
}

func (r *AgentRun) EntityID() string { return r.EntityID_ }
func (r *AgentRun) Workflow() string { return "agent-run" }
func (r *AgentRun) Phase() string    { return r.Phase_ }
func (r *AgentRun) IsTerminal() bool { /* via registered Transitions */ }
func (r *AgentRun) ParentEntityID() string { return r.ParentRunEntityID }
// bare run loop-id is derived, never the lifecycle:"id" field:
func (r *AgentRun) RunID() string { id, _ := agentic.LoopIDFromExecutionEntityID(r.EntityID_); return id }
```

> **Identity fix (Codex P1, verified):** `projection.go` populates the
> `lifecycle:"id"` field from the entity-state KEY (full 6-part ID), not a
> triple. Tagging a *bare* `RunID` as `lifecycle:"id"` would round-trip to the
> full dotted ID and then panic/garble when recomposed through
> `ChainExecutionEntityID(org,platform,RunID)` (`entity_ids.go:139` rejects
> dots). Therefore the `lifecycle:"id"` field holds the **full EntityID**; the
> bare run loop-id is derived via `LoopIDFromExecutionEntityID`.

Registered once at startup via the **ADR-049 `Register(Workflow)`** signature
(`manager.go:116` — NOT the old 3-arg ADR-047 form):

```go
mgr.Register(lifecycle.Workflow{
    Name:    "agent-run",
    Factory: func() lifecycle.Participant { return &AgentRun{} },
    Transitions: lifecycle.Transitions{
        "dispatched":        {"executing", "failed", "cancelled"},
        "executing":         {"awaiting_approval", "completed", "failed", "cancelled"},
        "awaiting_approval": {"executing", "cancelled"},
        "completed": {}, "failed": {}, "cancelled": {},
    },
    AuditPredicates: lifecycle.AuditSpec{ /* At/Source/Note — created-at free */ },
    // ChildWorkflows / EntityIDPattern as needed for gateway + Children.
})
```

### 2. What the harness provides (verified against ADR-049 `Manager`)

`Create` (mint), `Get`/`LookupByEntityID` (resolve), `Transition`/
`TransitionWith` (phase), `Complete`/`Fail`, `UpdateFromOperator` (operator
patches, default-deny), `List`/`Watch`/`History` (operator view + KV-revision
audit), `Children` (parent→child via declared `LinkPredicate`), restart
recovery, the **existing lifecycle gateway** (`GET /workflows/agent-run`), and
rule integration (`lifecycle_*` actions + `$entity.lifecycle.*` substitutions).
Storage is ENTITY_STATES (`manager.go:182`), ADR-049-compliant.

**Corrections vs the first draft (verified):** there is **no `Manager.Update`**
(use `Transition`/`TransitionWith`/`UpdateFromOperator`); **no `Ancestors`**;
`Children` runs **parent→child**, while the loop tree is **child→parent**
(`agent.loop.parent`) — so the run does **NOT** get the loop subtree for free;
`KVBucket()/KVKey()` are **removed** from `Participant`.

### 3. The agentic adapter (the only net-new framework code)

- **Mint** — at the dispatch/rule layer, structurally declared (§4), calling
  `Manager.Create(&AgentRun{EntityID_: ChainExecutionEntityID(org,plat,rootLoopID), Phase_: "dispatched"})`.
  Idempotent (entity ID stable). At **creation**, not completion.
- **`run_id` propagation** at spawn (§6) — the redesign.
- **Milestone subscriber** — the one piece the harness doesn't provide:
  subscribe to terminal subjects, decode once, demux by payload category to
  product handlers under a panic guard (lifts semteams `subscriber.go`, zero
  domain). The framework pre-resolves the run from the event's `run_id` and
  hands it to the product handler, which writes milestone triples via a
  publisher — **not** via `Manager` (§5):

```go
type MilestoneHandler interface {
    OnLoopTerminal(ctx context.Context, ev agentic.LoopTerminalEvent,
        run *AgentRun, pub TriplePublisher) error
}
```

Resolution fallback (`Resolver.ChainID` walk + the `RequestClassified` footgun
fix, `chain/resolver.go:209`) is lifted into the framework for pre-migration /
un-threaded loops, logged WARN.

### 4. Terminal authority + structural mint (Codex P1)

**Who closes a run** must be explicit, or the generic subscriber gets it wrong
(closing on any child failure kills recoverable chains; closing on any child
success completes early). Resolution:

- **Framework CREATES and OBSERVES.** It mints the run and stamps audit/phase
  on declared transitions. It does **not** infer run completion from child
  loop events.
- **Product/coordinator EMITS the terminal run decision** — the coordinator is
  the orchestrator that knows when the run is done. It fires a
  `lifecycle_transition` rule action (or its terminal tool stamps a trigger a
  rule matches) to move the run to `completed`/`failed`/`cancelled`. This is
  ADR-028-consistent (rules/coordinator orchestrate; framework observes).
- **Narrow framework fallback:** if the dispatch-ROOT loop terminates
  (fail/cancel) **before any child handoff** (run still `dispatched`, no
  children), the subscriber transitions the run to `failed`/`cancelled` to
  prevent a zombie. This is the ONLY framework-initiated terminal transition,
  and it's why mint-at-creation requires the failure subscriber.
- The adapter calls `Transition` with the **explicit** terminal — **never
  `Manager.Complete`** (which, with `executing`→3 terminals, picks
  `reachable[0]` non-deterministically, `manager.go:671`).

**Mint is a declared rule-action field, not prose/convention.** Auto-minting
every parentless loop would mint a "run" per CLI-chat/HTTP loop
(`agentic-dispatch/http.go:301`, `component.go:718`). Add a typed field to the
rule `Action` (`processor/rule/actions.go:180`, today only `RelatedLoops`):

```go
RunScope string `json:"run_scope,omitempty"` // "new" | "inherit" | "none"
```

- `new` → mint a run; this spawn is the root.
- `inherit` → propagate the firing loop's `RunID` (normal in-run child spawn).
- `none`/empty → no run association (ordinary loops).

Validated in `Action.Validate()`. (Open: default — lean `inherit` when the
firing entity has a run, else `none`, so child spawns are free and only the
coordinator's dispatch declares `new`.)

### 5. Milestones — two write paths + a cardinality contract (architect OQ#0 + Codex P2)

Milestones are **product triples on the run entity**, written through the
**graph-ingest path** (`AddTriple` → `graph.mutation.triple.add`,
last-writer-wins per predicate, no CAS) — the path semteams uses today — **NOT**
through `Manager`. Two reasons (both verified):

1. **Authority:** `UpdateFromOperator` is operator-authority gated, default-deny
   (`AssertRuleWritable`, `manager_query.go:451`). Routing derived milestone
   data through it would make every milestone operator-patchable and force the
   product vocabulary into the framework struct — contradicting the non-goal.
2. **Contention:** `Manager` writes are CAS-on-revision (`updateRetries=5`,
   `manager.go:399`). A subscriber stamping on every loop completion in a
   fan-out arc contends on the one hot run entity → retry exhaustion. The
   graph-ingest path has no CAS (same rationale ADR-049 used to keep AGENT_LOOPS
   off the CAS path).

So: **run PHASE = `Manager.Transition`** (low-write, transition-guarded);
**run MILESTONES = `AddTriple`** (higher-frequency, derived, latest-wins).

**Cardinality contract (Codex P2):** "last-writer-wins per predicate" silently
erases parallel artifacts if N children write the same predicate. The ADR must
distinguish:
- **Scalar run-snapshot predicates** (`chain.dispatched.at`, current best
  value) — single-valued, latest-wins is correct.
- **Cardinality-many per-loop facts** (each child's artifact) — must be
  **loop-qualified** (predicate or object carries the loop id, e.g.
  `chain.artifact.<loopID>.path`, or a separate artifact entity referenced from
  the run). Fan-out arcs MUST use the cardinality-many shape or they self-erase.

Milestone *vocabulary* stays product-domain; the framework only provides the
run entity as the home and the resolved run to the handler.

### 6. `run_id` propagation (the redesign) + wire fields

**Typed field, inherited at spawn** — replaces both string-keyed mechanisms:

- Add `RunID string` to `agentic.TaskMessage` and `agentic.LoopEntity`.
- Set in `rule.executePublishAgent` (`actions.go:1094`) per `RunScope` (§4),
  mirroring the existing `ParentLoopID` inheritance (`actions.go:1114`).
  **Propagate at BOTH spawn sites** — `executePublishAgent` AND the
  architect→editor sub-spawn — or the second silently falls back to the walk
  (Codex P2).
- Stamp `agent.loop.run` on the loop entity in `buildSpawnIdentityTriples`
  (`graph_writer.go:506`), atomic at spawn (no gh#159 race). Upserts read
  `$entity.triple.agent.loop.run`. (`agent.loop.run`
  token verified collision-free; full grammar-collision audit still TODO at ADR
  time per `feedback_grammar_collision_audit_on_new_tokens`.)

**Wire fields on ALL FOUR events (Codex P2), not just the terminals:**
`LoopCreatedEvent` (`handlers.go:468`, published `agent.created`),
`LoopCompletedEvent`, `LoopFailedEvent`, and `LoopCancelledEvent`
(`events.go:165` — currently lacks even `ParentLoopID`) gain `RunID` +
`RunEntityID` (bare + 6-part, matching the `ParentLoopID` precedent). Populated
from `LoopEntity.RunID`. Creation is a real event surface the run subscriber may
need; cancellation is published on **`agent.complete`** (not a cancel subject),
so the subscriber **must demux by payload category** — it cannot assume subject
= category.

This kills the read-side walk: handlers receive the resolved run; no
`resolver.ChainEntityID` round-trip, no walk-failure branch.

## Framework / product boundary

| Piece | Verdict |
|---|---|
| `ChainExecutionEntityID`, `LoopIDFromExecutionEntityID`, `agent.loop.parent` at spawn | HAS |
| Mint / resolve / operator view / recovery / audit / rule-integration | REUSE harness (`Create`/`Get`/gateway/`AuditPredicates`/`lifecycle_*`) |
| `CompletionSubscriber` demux + panic guard; `RequestClassified` footgun fix; fallback walk | LIFT (adapter) |
| typed `RunID` propagation; `RunScope` rule field; wire fields on 4 events | NEW |
| `AgentRun` Participant + Transitions | NEW (thin) |
| `Manager.Complete` on a run | FORBIDDEN (use explicit `Transition`) |
| milestone vocab (`chain.*`), role→action gates, stampers | LEAVE (product) |
| `DispatchedStamper` | DISSOLVES into `AuditPredicates` (created-at free) |
| markdown rendering; pause/resume decision HTTP | LEAVE (ADR-038 D6 / ADR-037) |

## Storage / write-rate

Run entity in ENTITY_STATES (low-write: a handful of phase transitions + audit;
passes the ADR-049 rubric — facts the graph reasons over, audit free from KV
revisions; **no private bucket**). Loops stay in high-write `AGENT_LOOPS` (the
ADR-049 exception; not Participants). Phase via CAS; milestones via graph-ingest
(§5). Clean split, both rationales from ADR-049.

## Breaking-change surface + migration (re-scoped — Codex/architect)

- `TaskMessage`/`LoopEntity`/4 events gain fields — additive on structs.
- New `RunScope` rule-action field — additive, validated.
- **semteams rule-pack migration is the dominant cost/risk (~2.3× the first
  estimate):** ~**35** rule files. Historical untyped run anchors migrate to
  `agent.loop.run` / `agent.run.entity-id`; genuine sibling-loop relationships
  migrate to `agent.lineage.<role-key>`. The old rule JSON does not distinguish
  those meanings syntactically, so this requires a per-site semantic audit,
  not a mechanical replacement.
- Touches ingest→entity→graph→query → **`task e2e:agentic` green required before
  tag** (CLAUDE.md hard rule). Net product LOC negative.

## Test gate (Codex P2 — promote contract tests into the gate)

Beyond `task e2e:agentic`, focused tests required:
- `AgentRun` lifecycle **projection** round-trip (full-ID identity, no
  corruption — guards the P1 fix).
- **Idempotent mint** (duplicate dispatch → one run).
- **`RunScope` rule-action schema validation** (new/inherit/none + invalid).
- **`RunID` inheritance at EVERY spawn path** (`executePublishAgent` +
  architect→editor) — guards the silent-fallback gap.
- **Wire fields on created/completed/failed/cancelled** + category-demux on
  `agent.complete`.
- **Fallback-walk WARN** behavior for un-threaded loops.
- **semteams rule-pack migration coverage** — `test/reference_configs_test.go`
  + `chain_entity_coverage` contract test re-run post-migration
  (`feedback_reference_configs_verify_triple_stamping`).

## Open / deferred / remaining TODO

- **Deferred (non-goals):** unified Run/Execution substrate shared with the
  harness; legacy migration of semspec/semdragon/semsage (prior art); milestone
  vocabulary as framework schema; markdown/`write_artifact`; pause/resume
  semantics; cross-run analytics; parallel/multi-arc runs (ADR-038 defers).
- **TODO at ADR time:** grammar-collision audit over all `$`-token regexes +
  `agvocab` constants for `agent.loop.run` / `agent.run.*`; confirm cancellation routing/subject and
  whether a third spawn site (dispatcher-direct/MCP) needs `RunID`; finalize the
  `RunScope` default.

## Review history

- **Architect (2026-06-07):** RIGHT-WITH-CHANGES. Caught the ADR-047→ADR-049 API
  staleness, the two-write-path resolution, mint-at-rule-layer, the
  `Manager.Complete` ambiguity, and the migration under-scope.
- **Codex (2026-06-07):** RIGHT direction, not-yet-ADR-ready. Added the P1
  identity round-trip corruption, terminal-authority gap, structural-mint-API
  requirement, the 4-event wire surface + cancellation demux, the milestone
  cardinality contract, and the test-gate expansion. Directed a body rewrite
  (this document) over an appendix.

## Next steps

1. Optional third critique on this converged rewrite, else promote to ADR.
2. ADR with the resolved API + re-scoped semteams migration plan + test gate.
3. Implement framework adapter + semteams migration as **lockstep PRs**; gate on
   `task e2e:agentic` + the focused test list.
