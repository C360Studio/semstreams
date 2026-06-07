# ADR-053: Agent-Run Substrate — Nested Agentic Loops as a Lifecycle `Participant`

## Status

**Proposed** — 2026-06-07. Not yet implemented or tagged. Derived from
[docs/proposals/agent-run-substrate.md](../proposals/agent-run-substrate.md)
(full design exercise + two adversarial reviews: architect + Codex, both
RIGHT-WITH-CHANGES, thesis accepted). Builds on
[ADR-047](047-lifecycle-harness-substrate.md) (Lifecycle harness) and
[ADR-049](049-lifecycle-harness-prime-schema-over-entity-states.md) (schema over
ENTITY_STATES). Lifts the reference design from semteams ADR-038 (chain entity).
Companion memory: `project_agent_run_chain_framework_gap`.

This ADR is **additive** to the rule engine and agentic types; the only breaking
surface is the **semteams rule-pack migration** (a downstream consumer change,
lockstep with the framework tag — see Consequences).

## Context

### The gap

A coordinator agentic loop that spawns research/architect/builder child loops
forms a **run**: a tree of nested loops spanning multiple arcs, accumulating
cross-arc state (e.g. `autoresearch.best.value`, research/spec/build artifacts).
The framework models the loop *tree* (`LoopEntity.ParentLoopID`, `agent.loop.parent`
stamped at spawn by `WriteSpawnIdentity`, `processor/agentic-loop/graph_writer.go:451`)
but has **no first-class entity for the run itself** — only an unused
`ChainExecutionEntityID` constructor (`agentic/entity_ids.go:139`). Every
consumer re-derives run identity.

semteams (our reference-design product) hand-rolls the entire run layer (~600
LOC of `cmd/semteams/chain/*`): ancestry-walk resolution, a string-keyed lineage
escape hatch, a completion-event demux, and milestone stampers. The symptom that
opened this thread was semteams #225 asking for the run's 6-part entity ID on the
wire — a request the framework couldn't satisfy because it doesn't own the run.

### Two run-identity mechanisms in the reference design (load-bearing)

semteams carries two, side by side:

1. **Ancestry-walk chain (ADR-038):** `chain_id` = dispatch-root loop UUID, found
   by walking `agent.loop.parent` (`chain/resolver.go:97`).
2. **`run-loop-entity-id` lineage thread:** pinned in `related_loops` at the root
   and re-threaded through every spawn rule, read as
   `$entity.triple.lineage.run-loop-entity-id` (autoresearch `01:48`, `04c:26`;
   sibling `lineage.plan-loop-entity-id` for the gather-join, research `03a`).

Mechanism #2 exists to dodge the gh#159 completion-time race — **which the
framework has already closed**: `WriteSpawnIdentity` now stamps `agent.loop.parent`
atomically *at spawn* (`graph_writer.go:451`, godoc cites the fix). #2 is thus
partly vestigial. A typed `run_id` set at spawn collapses both mechanisms plus
the read-side walk.

### What this is not

- Not a new substrate parallel to the Lifecycle harness — the run REUSES it.
- Not a change to the agentic *loop* model — loops stay in `AGENT_LOOPS` (their
  per-iteration LLM-judgment lifecycle, high write rate), excluded from
  `Participant` exactly as ADR-047 decided.
- Not a milestone-vocabulary owner — milestone predicates stay product-domain.
- Not a workflow engine, event bus, or markdown renderer.

### Why a `Participant`, and why now

ADR-047 excluded the agentic *loop* from `Participant` ("per-iteration model
judgment, dynamic trajectory… doesn't fit the declared-transitions-table") but
explicitly anticipated this case: *"a future agentic role with declared phases
could implement `Participant` if the fit emerges."* The **run** is a different
entity from the loop: its lifecycle is a small *declarable* state machine
(`dispatched → executing ⇄ awaiting_approval → terminal`); the dynamism (which
arc spawns next, retries) happens *inside* the `executing` phase, not as run-level
phase churn. The fit has emerged.

The `Manager` surface (`pkg/lifecycle`, post-ADR-049) is a superset of what a run
needs: `Create`=mint, `Get`/`LookupByEntityID`=resolve, `Transition`/`Complete`/
`Fail`=lifecycle, `List`/`Watch`/`History`=operator view + KV-revision audit,
`Children`=tree, `UpdateFromOperator`=operator patches, plus restart recovery,
the operator gateway (`GET /workflows`), rule integration (`lifecycle_*` actions
+ `$entity.lifecycle.*`), and ENTITY_STATES storage (`manager.go:182`). Building a
parallel substrate would re-implement all of it.

## Decision

Model the run as a Lifecycle `Participant` (`workflow="agent-run"`), reusing the
harness wholesale. Add a thin agentic adapter for the parts the harness doesn't
provide. Eight decisions:

### D1 — `AgentRun` Participant with a full-EntityID identity field

```go
type AgentRun struct {
    EntityID_         string `json:"-" lifecycle:"id"` // FULL 6-part: ...agent.chain.execution.<loopID>
    Phase_            string `json:"phase"`
    ParentRunEntityID string `json:"parent_run_entity_id,omitempty"`
}
func (r *AgentRun) EntityID() string       { return r.EntityID_ }
func (r *AgentRun) Workflow() string       { return "agent-run" }
func (r *AgentRun) Phase() string          { return r.Phase_ }
func (r *AgentRun) ParentEntityID() string { return r.ParentRunEntityID }
func (r *AgentRun) RunID() (string, bool)  { return runIDFromChainEntityID(r.EntityID_) } // chain entity parser, not LoopIDFromExecutionEntityID
```

The `lifecycle:"id"` field MUST hold the **full** entity ID. `projection.go`
populates it from the entity-state KEY, not a triple; a bare `RunID` tagged
`lifecycle:"id"` would round-trip to the full dotted ID and then panic/garble
when recomposed through `ChainExecutionEntityID` (which rejects dots). The bare
run loop-id is **derived**.

### D2 — Registered via the ADR-049 `Register(Workflow)` API

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
    AuditPredicates: lifecycle.AuditSpec{ /* created-at/source/note — free */ },
})
```

Milestones are NOT projected `Participant` fields (D5). The run gets the full
Manager/recovery/gateway/rule-integration surface for the **phase** dimension.
`Children` is parent→child via a declared link predicate; the loop subtree
(child→parent `agent.loop.parent`) is **not** free — resolve it via the lifted
fallback walk (D6) when needed.

### D3 — Terminal authority: framework creates + observes; product decides terminal

- The **framework CREATES** (mint, D4) and **OBSERVES** (subscriber, D7). It does
  **not** infer run completion from child-loop events.
- The **product/coordinator EMITS the terminal run decision** (it is the
  orchestrator that knows when the run is done) by firing a `lifecycle_transition`
  rule action to `completed`/`failed`/`cancelled`. ADR-028-consistent.
- **Narrow framework fallback:** if the dispatch-ROOT loop terminates
  (fail/cancel) *before any child handoff* (run still `dispatched`, no children),
  the subscriber transitions it to `failed`/`cancelled` to prevent a zombie. This
  is the ONLY framework-initiated terminal transition.
- The adapter calls `Transition` with the **explicit** terminal — **never
  `Manager.Complete`** (with `executing`→3 terminals it picks `reachable[0]`
  non-deterministically, `manager.go:671`).

### D4 — Mint is a declared rule-action field, not convention

Add to the rule `Action` struct (`processor/rule/actions.go`):

```go
RunScope string `json:"run_scope,omitempty"` // "new" | "inherit" | "none"
```

- `new` → mint a run (`Manager.Create(&AgentRun{EntityID_: ChainExecutionEntityID(org,plat,rootLoopID), Phase_: "dispatched"})`), idempotent, at **creation**.
- `inherit` → propagate the firing loop's `RunID` (normal in-run child spawn).
- `none`/empty → no run association.

Validated in `Action.Validate()`. Default: `inherit` when the firing entity has a
run, else `none` — so child spawns are free and only the coordinator's dispatch
declares `new`. Auto-minting every parentless loop is rejected (would mint a run
per CLI-chat/HTTP loop, `agentic-dispatch/http.go:301`).

### D5 — Milestones: two write paths + a cardinality contract

Run **phase** → `Manager.Transition` (low-write, CAS, transition-guarded). Run
**milestones** → product triples on the run entity via the **graph-ingest path**
(`AddTriple`, last-writer-wins per predicate, no CAS) — NOT `Manager`:

- `UpdateFromOperator` is operator-authority gated, default-deny
  (`manager_query.go:451`); routing derived milestone data through it makes them
  operator-patchable and forces product vocab into the framework struct.
- `Manager` writes are CAS (`updateRetries=5`, `manager.go:399`); a subscriber
  stamping on every loop completion in a fan-out arc contends on the hot run
  entity → retry exhaustion. Same rationale ADR-049 used to keep `AGENT_LOOPS`
  off the CAS path.

**Cardinality contract:** distinguish **scalar run-snapshot** predicates
(latest-wins correct, e.g. `chain.dispatched.at`, current-best) from
**cardinality-many per-loop facts** (each child's artifact) — the latter MUST be
loop-qualified (`chain.artifact.<loopID>.*` or a referenced artifact entity) or
parallel children silently erase each other. Milestone **vocabulary** stays
product-domain.

### D6 — Adapter: subscriber + lifted resolution

- **Milestone subscriber** (lifts semteams `subscriber.go`): subscribe to terminal
  subjects, decode once, **demux by payload CATEGORY** (cancellation is published
  on `agent.complete`, not a cancel subject — D8), fan to product handlers under a
  panic guard. The framework pre-resolves the run from the event's `run_id` and
  passes it in:

  ```go
  type MilestoneHandler interface {
      OnLoopTerminal(ctx context.Context, ev agentic.LoopTerminalEvent,
          run *AgentRun, pub TriplePublisher) error
  }
  ```
- **Fallback resolution** (lifts `Resolver.ChainID` walk + the `RequestClassified`
  footgun fix, `chain/resolver.go:209`): for pre-migration / un-threaded loops,
  logged WARN.

### D7 — Typed `run_id` propagation (the redesign), at both spawn sites

- Add `RunID string` to `agentic.TaskMessage` and `agentic.LoopEntity`.
- Set in `rule.executePublishAgent` (`actions.go:1094`) per `RunScope`, mirroring
  `ParentLoopID` inheritance (`actions.go:1114`). **Propagate at BOTH spawn
  sites** — `executePublishAgent` AND the architect→editor sub-spawn — or the
  second falls back to the walk.
- Stamp `agent.run` on the loop entity in `buildSpawnIdentityTriples`
  (`graph_writer.go:506`), atomic at spawn. Upserts read `$entity.triple.agent.run`.
  (`agent.run` token verified collision-free; full grammar-collision audit at
  implementation time per `feedback_grammar_collision_audit_on_new_tokens`.)

This collapses both run-identity mechanisms and retires the read-side walk for
threaded loops.

### D8 — `RunID`/`RunEntityID` on all four loop events

`LoopCreatedEvent` (`handlers.go:468`), `LoopCompletedEvent`, `LoopFailedEvent`,
and `LoopCancelledEvent` (`events.go:165` — currently lacks even `ParentLoopID`)
gain `RunID` (bare) + `RunEntityID` (6-part), populated from `LoopEntity.RunID`
(matching the `ParentLoopID` precedent). Subscribers receive the resolved run on
the wire — no `resolver.ChainEntityID` round-trip. Because cancellation rides
`agent.complete`, the subscriber demuxes by payload category, not subject.

## Consequences

### Storage

Run entity in ENTITY_STATES (low-write: phase transitions + audit; passes the
ADR-049 rubric — graph-reasonable facts, audit free from KV revisions; **no
private bucket**). Loops stay in high-write `AGENT_LOOPS` (the ADR-049
exception). Phase via CAS; milestones via graph-ingest. Clean split.

### What we gain

semteams retires its hand-rolled run layer: the resolver/ancestry-walk as
primary, the three-source fallback, the `run-loop-entity-id` threading, the
hand-wired completion subscriber, and the `DispatchedStamper` (dissolves into
`AuditPredicates`). Net product LOC negative. Runs gain an operator API, audit
history, restart recovery, and rule integration **for free**. The #225 wire-field
asks are answered as a typed contract, not a `loop_wire.go` patch. The next
agentic product gets the run primitive instead of re-plumbing it.

### Breaking surface + migration (the dominant cost/risk)

- Framework changes are additive (`RunID` fields, `RunScope`, `AgentRun`, the
  adapter).
- **semteams rule-pack migration**: ~**35** rule files; a **family** of
  run-anchor predicates (`lineage.run-loop-entity-id` AND
  `lineage.plan-loop-entity-id`); run-anchor vs genuine sibling-lineage is **not
  syntactically distinguishable** — per-site semantic audit, NOT a sed. Retires
  `related_loops`-as-run-anchor; keeps `related_loops`-as-sibling-lineage
  (`lineage.researcher`). Lockstep with the framework tag.
- Touches ingest→entity→graph→query → **`task e2e:agentic` green required before
  tag** (CLAUDE.md hard rule).

### Test gate

`task e2e:agentic` plus focused tests: `AgentRun` projection round-trip (full-ID
identity — guards D1); idempotent mint; `RunScope` schema validation; `RunID`
inheritance at every spawn path (guards the silent-fallback gap); wire fields on
created/completed/failed/cancelled + category demux; fallback-walk WARN; semteams
rule-pack migration coverage via `test/reference_configs_test.go` +
`chain_entity_coverage` contract test (`feedback_reference_configs_verify_triple_stamping`).

### Deferred (non-goals)

Unified Run/Execution substrate shared with the harness; legacy migration of
semspec/semdragon/semsage (prior art); milestone vocabulary as framework schema;
markdown/`write_artifact` (ADR-038 D6); pause/resume semantics (ADR-037);
cross-run analytics; parallel/multi-arc runs (ADR-038 defers).

### Open at implementation time

Grammar-collision audit over all `$`-token regexes + `agvocab` constants for
`agent.run*`; confirm cancellation routing/subject and whether a third spawn site
(dispatcher-direct / MCP) needs `RunID`; finalize the `RunScope` default.

## References

- [docs/proposals/agent-run-substrate.md](../proposals/agent-run-substrate.md) —
  full design exercise + review history.
- [ADR-047](047-lifecycle-harness-substrate.md), [ADR-049](049-lifecycle-harness-prime-schema-over-entity-states.md) — the harness this builds on.
- semteams ADR-038 (chain entity + milestone rendering) — the lifted reference design.
- [ADR-028](028-orchestration-architecture.md) — rules/coordinator orchestrate; framework observes (D3 grounding).
