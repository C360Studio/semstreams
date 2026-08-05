# ADR-046: Parallel Fan-Out — `for_each` primitive + gated-DAG dispatch

## Status

**Phase 1 (`for_each`) Accepted + shipped** (beta.82/83). **Phase 2
(gated-DAG dispatch) design Accepted — amended 2026-06-27 (GH #357).**
Phase 2 lands as a generic **component** composing ADR-047/048 substrate +
a dependency-free selection brain — **not** a rule action or condition
operator (see Phase 2 below for the pressure-tested rationale across all
eight correctness wedges). Implementation pending a dedicated session.
Closes ADR-026 milestone 2 (parallel flow composition), deferred since
ADR-026 shipped Phase 1. References GH #134, GH #357.

**ADR-091 amendment (current mutation behavior):** the gated-DAG decision remains
accepted, but its historical `replace_owned` claim/marker mechanism is retired.
Current implementations reconcile the component's declared predicate set through
the canonical request/reply mutation port and use an expected entity revision for
CAS conflict detection. There is no owner token, owner claim, or replace-owned lane.
References to `replace_owned` below describe the superseded implementation design.

## Context

SemTeams's deep-research and dev-via-spec packs hit the same shape: a
coordinator role decides "split this work into N independent subtasks"
and the framework executes them sequentially. With N=4 subtopics and a
per-investigator wall-clock of T, the wall-clock cost is **4T** even
though every subtopic is independent by construction (that's what the
decomposer determined).

The existing fan-out pattern in `configs/rules/deep-research/03-fan-out-subtopics.json`
is explicitly a milestone artifact:

> *"True parallel fan-out (N agents at once) is out of scope for this
> milestone — that needs the coordinator's flow-composition tools
> (ADR-026 milestone 2). See ADR-026 + ADR-028."*

GH #134 surfaces the gap with concrete SemTeams smoke6 evidence
(2026-05-22): 4-subtopic investigation runs serial through coordinator
re-iteration because the rule engine has no primitive for "iterate this
list, spawn N concurrent agents."

### Prior art — semspec's `scenario-orchestrator`

`semspec/processor/scenario-orchestrator/component.go` ships a working
implementation of the harder shape (gated-DAG dispatch with bounded
concurrency). Key patterns worth lifting verbatim:

1. **Stateless re-evaluation on completion.** A completion KV watcher
   re-triggers `dispatchRequirements`, which re-evaluates from KV truth.
   No in-flight tracking; the dispatcher is idempotent.
2. **Synchronous reconcile from KV before dispatch.** Closes the race
   where the completion cache lags the truth on rapid re-fires.
3. **DAG gating filter** — `filterReadyRequirements` keeps only items
   whose `DependsOn` deps are completed AND themselves not yet completed.
4. **Bounded-concurrency semaphore** — `chan struct{}, MaxConcurrent` +
   `sync.WaitGroup` + error channel.
5. **Join is implicit.** When `len(ready) == 0`, dispatch is done; no
   explicit counter, no aggregation primitive.

The semspec executor handles the no-deps case (all-ready immediately,
all dispatched in parallel) as a degenerate instance of the gated-DAG
case — empty DAG edges.

### Why two primitives, not one

The semspec gated-DAG pattern subsumes the simple-iteration case
mathematically. But it carries operational complexity that the
no-deps use case (SemTeams's deep-research 4-subtopic shape) doesn't
need:

- Completion-watcher infrastructure
- Stateless re-eval scheduler
- Bounded-concurrency tuning per flow
- DAG validation at decompose-time

For the **no-deps subset** (decompose → N independent investigators
→ synthesizer), declarative `for_each` over a list is the minimum
viable surface. Rule author writes:

```json
{
  "type": "publish_agent",
  "for_each": "$entity.triple.coordinator.decision.subtopics",
  "for_each_var": "subtopic",
  "role": "researcher-investigate",
  "prompt": "Investigate: $subtopic"
}
```

Framework iterates the list, publishes N TaskMessages, returns. The
agentic-loop's existing JetStream consumer parallelism handles N
concurrent executions automatically — no scheduler needed.

The **DependsOn-shaped case** (decomposer emits a DAG; some subtasks
gate others; recover from partial failure mid-DAG) needs the full
gated-DAG executor. That's a richer primitive worth its own session.

## Decision

Two complementary primitives shipped in two phases.

### Phase 1 — `for_each` (beta.82 target, this ADR's first PR)

Declarative list-iteration on `rule.Action`:

- New fields on `rule.Action`:
  - `ForEach string` — substitution-resolvable reference to a list-typed value (e.g. `$entity.triple.coordinator.decision.subtopics`)
  - `ForEachVar string` — variable name bound per-iteration (e.g. `subtopic` → `$subtopic` in `prompt` / `properties` substitutes the current item)
- New substitution path that resolves list-typed triple objects to
  `[]any` instead of stringifying via `fmt.Sprintf("%v", ...)`.
- New `ExecutionContext.SubstituteVariablesWithIterVar(template,
  varName, value)` overlay method — the for_each loop passes the
  current iteration's value as a string-typed overlay binding.
- `decide` tool stamps a `coordinator.decision.subtopics` triple (JSON-
  encoded `[]string`) when `args.Subtopics` is non-empty. Without this
  there's no list-typed triple to iterate against from a coordinator
  fan-out decision.
- `executePublishAgent` checks `ForEach`; if set, resolves the list and
  iterates, calling the existing publish logic per item with the
  iter-var overlay.

Constraints:
- `ForEach` works only on `publish_agent` (the only action with the
  use case today). Wider applicability is a follow-up.
- No `DependsOn` between iterations — pure broadcast. Iterations are
  independent by contract.
- No bounded concurrency at the dispatch layer. Each iteration is a
  NATS publish (non-blocking); concurrency is determined by the
  downstream consumer's `MaxAckPending` and worker pool. Operators
  who need a cap set it on the JetStream consumer.
- No framework-side join. The coordinator-as-counter pattern (issue
  #134 Option 3) handles join semantics rule-side: a downstream rule
  fires on each child completion, stamps a counter triple onto the
  parent loop entity, and a separate rule matches via `length_eq`
  when the counter equals the expected size. Worked example:
  [`configs/rules/example-fan-out/`](../../configs/rules/example-fan-out/README.md).
  **Note (#147)**: this pattern needs the `subject` override on
  triple-write actions (`add_triple` / `update_triple` /
  `remove_triple`) and the array operators (`length_eq`,
  `length_gt`, `length_lt`, `array_contains`) registered. Both
  shipped in beta.83 alongside this amendment. Prior to #147 the
  initial Phase 1 implementation in beta.82 documented the counter
  pattern but the primitives needed to write it didn't yet exist —
  a documentation-vs-reality discipline failure caught by semteams
  during their research-pack wiring. The sequential pattern that
  shipped before parallel fan-out is **coordinator-as-iterator**
  (the coordinator respawns and re-judges on each child completion),
  not the counter pattern; the two are distinct join shapes.

Forward-compat: Phase 2's gated-DAG **component** (see below) is additive.
Rule packs that use Phase 1's `for_each` for no-deps flows don't need
to migrate; they coexist (semteams research stays on `for_each`).

### Phase 2 — gated-DAG dispatch (amended 2026-06-27, GH #357)

**Decision: gated-DAG dispatch lands as a generic _component_, not a
rule action (`fan_out_gated`) and not a condition operator.** The
component composes substrate that shipped *after* this ADR was first
written, each in a **specific** role (an adversarial review of an earlier
draft of this amendment, 2026-06-27, caught it leaning on the wrong
pieces — corrected here):

- **ADR-047 `pkg/lifecycle`** — authoritative `Manager` reads
  (`GetWithRevision` is a direct JetStream `Get`) + `Participant`
  instances, and crucially `Watch`/`WatchAll`, whose bootstrap
  re-delivery (NATS KV replays all current values on subscribe) is **the
  re-eval driver and the restart-recovery substrate**.
- **ADR-048 `pkg/dispatch` BoundedDispatcher** — the bounded-concurrency
  *dispatch* leg **only**. Its internal completion-watcher is per-`Submit`
  request/response and **suppresses bootstrap replay**, so it is *not*
  the whole-set re-eval driver and *not* restart recovery — those are the
  lifecycle `WatchAll` above. (Reusing the completion-watcher for re-eval
  was the earlier draft's mistake.)
- **`replace_owned` owned-marker action** — a **durable in-flight
  record**, NOT a single-claimant CAS lock (`ExpectedRevision` is always
  0 — last-write-wins reconciliation). See "Load-bearing invariants"
  below for what actually provides mutual exclusion.
- **`graph.query.prefix` contract** (beta.113) — authoritative whole-set
  enumeration; returns full `EntityState`s (triples/markers included), so
  the executor reads the unit set + markers in one query.

The dependency-free **selection brain** is generalized from semspec's
*pure-but-currently-unwired* `workflow/coordinator` (its live
`scenario-orchestrator` is a separate, bespoke executor with its own
stall-blind filter — we lift and generalize the brain, not that
component). The framework hosts the brain + executor + stall detection;
the consumer keeps its domain layer and pre-resolves the edge set. This
shrinks the "~600 LOC executor worth its own session" framing above —
much of the substrate now exists — but the brain wiring, stall
detection, and the periodic backstop are genuinely net-new (no
production reference implements them; see below).

#### Why a component — three surfaces pressure-tested (GH #357)

Each candidate was stress-tested against the eight correctness
requirements below (trying to break each, not confirm it):

- **A DAG-aware `fan_out_gated` rule action** — rejected. It is the most
  workflow-engine-shaped thing the rule language would carry, against the
  "no separate workflow engine" identity (ADR-028).
- **A single `all_complete` condition operator** — rejected as
  insufficient. The rule engine evaluates conditions against the
  *changed* entity plus one `ec.Related`
  (`processor/rule/entity_watcher.go` → `evaluateRulesForEntityState`);
  it has no read over a *set* of N prerequisites, and — decisively — a
  gate on a dependent X never re-fires when a prerequisite P completes
  (the engine evaluates rules against P, not X). The operator silently
  needs both a set-quantified condition and a cross-entity re-eval
  trigger the rule model lacks.
- **Reverse-edge propagation + the existing counter-join** (`for_each
  P.required_by` stamps a `prereq_done` marker on each dependent; gate via
  `length_eq`) — rejected for this use case. It passes the
  no-reset/one-shot wedges (those `synthesize_when_all_gathers_complete`
  already proves) but **fails the four load-bearing wedges
  (derived-not-mutated, reset, failure-release, stall)** for one root
  reason: a rule on the dependent cannot read its N prerequisites'
  authoritative markers, so it must *project* prereq state onto the
  dependent — which is exactly the "separately-mutated status field that
  races the markers" that correctness requirement #1 names as "the root
  of the whole wedge family."

Shared root cause: gated-DAG dispatch needs a **whole-set, authoritative,
re-evaluated-on-every-change** view; the rule model is
**per-entity-change + single-related-read**. A component watching the
unit set provides the whole-set view natively — which is why semspec's
prior art is itself a component.

#### Design

**1. Selection brain — a dependency-free pure pkg (framework).**
Generalize semspec's `coordinator` to opaque unit IDs, stripping all
`Story`/owner/edge-resolution coupling (it `import`s
`c360studio/semspec/workflow` and bakes M:N owner-gating into `Evaluate`
— that stays in semspec). Contract:

- inputs: `unitIDs []string`; `dependsOn map[string][]string` (resolved
  DAG edges); `MarkerSet{Completed, Failed, Dirtied map[string]bool}`.
- `DeriveStatus(id, MarkerSet) Status` — pure, precedence Dirtied >
  Failed > Completed > Ready (a reset/dirtied unit derives Ready over any
  stale terminal marker).
- `SelectDispatchable` — a unit is dispatchable iff its status is Ready
  AND **every** prerequisite derives Done (all-prereqs closure, never
  "any").
- `Stalled` — units Ready-but-held with nothing dispatchable and nothing
  in-flight (a `depends_on` cycle, or all non-terminal units blocked
  behind a failed prereq); the silent-idle backstop.

No domain types, no I/O. semspec's existing coordinator test suite (the
cases ARE the requirements) generalizes onto it.

**2. Executor component (framework).** A long-lived component that
manages each fan-out as an ADR-047 Lifecycle `Participant` instance:

- **Reads the unit set authoritatively** each evaluation via
  `graph.query.prefix` (full `EntityState`s) — never cache-first, never
  from in-memory tracking.
- **Re-evaluates statelessly**, driven by `pkg/lifecycle` `WatchAll` over
  the unit bucket (bootstrap re-delivery replays all current values on
  subscribe → restart reconciles from KV; NOT the `pkg/dispatch`
  completion-watcher, which suppresses replay), on every
  completion/failure/reset event *and* on a periodic backstop tick (so a
  missed watch event cannot hide a stall).
- **Claims-then-dispatches.** The brain returns `Ready` for an in-flight
  unit (it carries no terminal marker), so the executor records a durable
  `replace_owned` dispatch marker and skips units that already carry one.
  **Mutual exclusion comes from single-flight execution, not from the
  marker write** (`replace_owned` is last-write-wins, not CAS): the
  invariant is one re-eval pass at a time per instance, one instance per
  fan-out, and the marker MUST be committed *before* the work is
  dispatched (else a restart re-derives `Ready` and double-runs). The
  ADR-056 owner-incarnation lease is a *cross-incarnation* backstop only
  (catches a post-restart zombie), and it is **default-off** — it does
  not exclude same-incarnation concurrent claimants. See "Load-bearing
  invariants."
- **Dispatches under bounded concurrency** via `pkg/dispatch`
  BoundedDispatcher (the concurrency leg).
- **Surfaces `Stalled()`** as an alert (metric / `ops.diagnosis.*`),
  never as benign idle. `Stalled()` is net-new wiring — no current
  consumer calls it (semspec's live orchestrator is stall-blind).

**3. Framework/consumer boundary.**

- *Framework:* brain + executor + stall detection + the `$state` reset
  fix (below). Domain-agnostic — consumes a resolved `depends_on` edge
  set + completion/failure/reset markers on per-unit entities.
- *Consumer:* derives the edge set (semspec unions semantic prereqs +
  ADR-044 file-overlap serialization edges; semteams research is depth-1
  no-edges → stays on Phase 1 `for_each`), mints fresh-per-run unit
  identities, and layers any domain gate (semspec's M:N Story
  owner-gating: release a non-owner on its owner's terminal-OR-reset
  state). The brain has no `Story`/owner concept.

#### How the eight correctness requirements are met

1. **Derived, never mutated** — the executor re-reads the whole prereq
   set authoritatively and the brain re-derives status from membership
   every evaluation. No projected status field to drift (the wedge Path R
   could not escape).
2. **Wait for ALL prerequisites** — the brain's all-prereqs closure;
   stateless re-eval converges on any late-arriving prereq.
3. **Generic dependency source** — the brain consumes a resolved edge
   set; the consumer unions whatever sources it needs first.
4. **Reset / re-dispatch authoritative** — reset clears the unit's
   terminal + `replace_owned` claim; the next re-eval reads authoritative
   KV and `Dirtied` precedence re-derives Ready → re-dispatch. No reverse
   un-stamping (Path R's fragility); evicted-then-recreated entities are
   read fresh, never idempotent-skipped into idle.
5. **Fresh-per-run identity** — consumer mints run-nonce IDs; framework
   side is the `$state` fix. Orthogonal to the dispatch mechanism.
6. **In-flight dedup** — a durable `replace_owned` dispatch marker, read
   authoritatively, excludes already-dispatched units (the brain's
   `Ready` alone would re-select them). The marker is the *durable
   record*; the *mutual exclusion* is single-flight execution (the marker
   write is not a CAS lock — see invariants). Committed before dispatch
   so restart can't double-run.
7. **Failure releases dependents** — a failed node derives Blocked; its
   dependents stay gated (correct — you cannot run a dependent whose
   prerequisite genuinely failed) while **independent branches keep
   flowing**; recovery is reset-driven, and the stuck branch is surfaced
   by `Stalled()`, so it is never *silently* stranded. Policy knob:
   stop-on-first-failure vs continue-others (retry-with-backoff deferred —
   see Phase 2.1 §4). *The
   brain logic is verified correct (deep-chain mid-failure → dependents
   read Blocked-held, not idle); the `Stalled()` wiring that delivers the
   "not silently stranded" guarantee is net-new (below).*
8. **Stall / cycle surfaced** — `Stalled()` over the authoritative
   whole-set; the transition *into* a stall is always an event
   (boot / last completion / a failure), and the periodic backstop closes
   the missed-event hole. *Both `Stalled()` wiring and the backstop are
   net-new — no production code (incl. semspec's live orchestrator)
   implements them; they are the load-bearing build, not reuse.*

#### Load-bearing invariants (the implementer MUST hold these)

An earlier draft of this section asserted guarantees the substrate does
not provide; an adversarial code review corrected them. These are
non-negotiable, or the dispatcher double-runs / breaks recovery / hides
stalls:

1. **Single-flight per fan-out, one instance per fan-out.** `replace_owned`
   is last-write-wins, not CAS, and the owner-lease fence is default-off +
   cross-incarnation only — so neither provides concurrent mutual
   exclusion. The dedup holds *only* if the executor runs one re-eval pass
   at a time per instance and exactly one instance owns a given fan-out.
   If true cross-writer exclusion is ever needed, switch the dispatch
   claim to an `ExpectedRevision`-based CAS write.
2. **Claim before dispatch.** The `replace_owned` marker MUST be committed
   *before* the work (agent task) is published. A crash between dispatch
   and claim re-derives the unit as `Ready` on restart → double-run.
3. **Re-eval + recovery ride lifecycle `WatchAll`, not the dispatch
   completion-watcher.** The latter suppresses bootstrap replay; wiring
   re-eval/recovery to it silently breaks whole-set re-eval and restart
   reconciliation.
4. **Stall detection + periodic backstop are net-new.** No reference
   implementation exists; they are the core build of this phase, not
   composed substrate.

#### Resolved open questions (superseding the Phase-1-era deferrals)

- **Where the DAG lives:** per-unit graph entities carrying `depends_on`
  triples on `ENTITY_STATES`; the fan-out instance is an ADR-047
  Lifecycle `Participant` (named instance, restart recovery, operator
  gateway visibility). No parallel flow-definition store.
- **Completion source-of-truth:** multi-valued completion/failure markers
  on the unit entities, read authoritatively; status derived from
  membership — never a separate completion bucket that can lag the truth.
- **Failure semantics:** default = failed node leaves its dependents
  Blocked while independent branches flow; recovery is reset-driven and
  the stuck branch is surfaced by `Stalled()`. Knob = `continue_others`
  (default) | `stop_on_first_failure` (retry-with-backoff deferred,
  Phase 2.1 §4). FanOut-instance *failure* is consumer-driven, not
  auto-derived — Phase 2.1 §2.
- **Synthetic-decide completion:** a child that completes via
  synthetic-decide counts as complete for dependent dispatch **iff** it
  produced the required deliverable — defer the judgment to the
  deliverable validator; never treat terminal-tool-less output as
  unconditionally complete.

#### Phase 2.1 — consumer-contract completion (amended 2026-06-27, GH #363–#365)

semspec's pickup of `v1.0.0-beta.117` — the first consumer of #357 — surfaced
three contract gaps. beta.117 is functionally correct (their build + suites
green); these complete the *consumer contract*. Taken as one deliberate pass,
not scattered patches.

**1. Event-driven dispatch off unit completions (GH #363).** beta.117's only
event-driven re-eval trigger was the lifecycle `Watch` over the FanOut
*instance*; unit completion/failure/reset markers live on the *unit* entities
under `unit_entity_prefix`, which nothing watched — so a completed prerequisite
unlocked its dependents only on the next `backstop_interval` tick (up to
N×interval scheduling latency on a depth-N chain).

Decision: add a second, **raw KV watch over `unit_entity_prefix`**
(`natsclient.KVStore.Watch` on ENTITY_STATES — NOT a lifecycle `Watch`, since
unit entities do not carry the FanOut phase predicate) that nudges the existing
single-flight `trigger` channel on any unit write. The periodic backstop is
**demoted from the primary completion path to the correctness floor** (the
missed-watch-event / restart safety net). This decouples dispatch latency from
steady read-load: completions drive dispatch immediately, and operators can now
*raise* `backstop_interval` (a longer net) rather than *lower* it (more idle
whole-set reads). Single-flight, claim-before-dispatch, and authoritative
re-read each pass are unchanged — the watch only *nudges*; correctness still
flows from the whole-set read + brain (Load-bearing invariant #3 holds: the
lifecycle `Watch` bootstrap replay remains the restart-recovery driver; the new
KV watch is a low-latency nudge, not the recovery path).

*Implementation note — the in-flight hint (latent dedup bug the watch exposed).*
The durable claim commits in the bounded-dispatch worker, **after** `submit`
returns and `evalMu` releases. With the backstop's slow cadence the claim was
always readable before the next pass; the sub-millisecond watch nudges re-evaluate
*between* submit and claim-commit and would re-select the same unit → double
dispatch. Closed with an in-memory `inflight` set (touched only under `evalMu`):
a unit is deduped if it carries the durable claim **or** is in `inflight`; the
durable claim takes over once committed, and `inflight` entries clear when the
unit goes terminal, is reset (`dirtied`), vanishes, **or its claim fails** (the
worker clears the hint under `evalMu` so the unit is re-selected next eval — a
sustained claim outage increments `gated_dag_claim_errors_total`, never a silent
wedge). This is the complement single-flight
needs for *asynchronous* dispatch — invariant #1 (one re-eval pass at a time) is
necessary but not sufficient when the claim write is async.

**2. Framework-owned FanOut instance lifecycle (GH #364).** beta.117 declared
and *watched* the FanOut `Participant` (`dispatching → completed | failed`) but
never **created** an instance or **advanced** it to terminal — the
operator-visible instance sat at `dispatching` forever, and instance/entity/edge
creation was an unstated consumer responsibility.

Decision: an **optional `fan_out_instance_id`** config. When set, the executor
**creates** the FanOut Participant on Start (`dispatching`, idempotent — tolerate
already-exists) and **auto-transitions it to `completed`** once the whole set is
terminal-and-done (every unit `Done`, nothing dispatchable, not stalled) — the
framework-emitted "fan-out finished" event the consumer asked for. When unset,
beta.117 behavior is preserved (no instance lifecycle owned). Backward-compatible.

Failure stays **consumer-driven, deliberately.** The executor does NOT
auto-transition to `failed`: a stall is usually *recoverable* (reset a failed
prerequisite → `dirtied` → re-dispatch), and `Participant` terminal phases have
no out-edges — auto-failing a recoverable stall would strand the instance with
no path back to `dispatching`. The brain's `Stalled()` does not distinguish an
unrecoverable `depends_on` cycle from a recoverable blocked-behind-failure, so
the framework *surfaces* the stall (point 5) and leaves the `failed` verdict to
the consumer (`Manager.Fail`), who knows whether they will recover. A future
provable-cycle auto-fail would need cycle detection in the brain — deferred.

**3. Setup contract + free-form predicates (GH #364.2, #365.2).** `doc.go` gains
a **consumer setup checklist** — when using `fan_out_instance_id`, create the
FanOut instance carrying `gateddag.fanout.phase=dispatching`; seed each unit
entity; write the `depends_on` edges; plus the existing reset contract — and
states that marker/edge predicates are **free-form**: graph-ingest validates
only the indexing-profile predicate (ADR-054), so no vocabulary registration is
required for the gated-DAG markers.

**4. `failure_policy` enum (GH #365.1).** The shipped enum is `continue_others`
(default) + `stop_on_first_failure`. `retry_with_backoff` (floated in the
failure-semantics text above) is **deferred** — recovery is reset/`dirtied`-
driven, so per-node backoff is not needed by the current consumer; additive when
one is.

**5. Stall surfacing (GH #365.3).** `Stalled()` always surfaces as the
`gated_dag_stalled_units` gauge + a WARN log. Additionally, when an optional
`stall_subject` is configured, the executor publishes an **edge-triggered**
(0 → non-zero) registered `StallEvent` to it — the consumer-wireable event for
active wedge-detection, symmetric with the dispatch publish, and deliberately
lighter than the agent-oriented `ops.diagnosis` finding-entity (which would
re-mint every backstop tick). The gauge stays for scrape-based monitoring. The
event is **backstop-tick-driven** (not per-write): a transient stall while the
consumer is still seeding (a dependent written before its prerequisite) must not
alert, so the event fires on the 0→non-zero transition *as seen at a periodic
tick* (the settled state). The gauge + WARN log still update every pass.

## Trade-offs and alternatives considered

**Lift gated-DAG dispatch directly without Phase 1.** Rejected for
this tag — semspec's pattern is ~600 LOC of executor logic plus
completion-watcher integration plus bounded-concurrency knobs. Worth
its own session with focused design + e2e. Forcing it through today's
session risks the same "half-finished engine work" failure mode that
the [project_engine_gaps_not_app_state](orchestration-check skill)
warns about.

**Generalize `for_each` across all action types in Phase 1.** Rejected
as scope creep. `publish_agent` is the only action with the immediate
use case; broadening surface costs LOC + tests without unblocking a
known consumer. Additive when a need surfaces.

**Skip the `coordinator.decision.subtopics` triple emission; require
operators to add a separate triple-stamping rule.** Rejected — the
decide tool is already the canonical decomposition emission point; not
stamping the list as a triple was an artifact of the sequential
pattern not needing it. Adding the triple emission is forward-compat
(operators get the triple for free) and removes a rule-author
sharp edge.

## Open questions (Phase 1)

- **Map iteration?** Phase 1 ships `[]any` only. If a rule author
  needs to iterate a `map[string]string` (e.g. per-role config),
  that's a separate `for_each_map` follow-up. Not blocking.
- **Nested for_each?** Out of scope. Phase 1 supports a single
  iteration scope per action.
- **for_each on Subject / Subject template?** Phase 1 iterates the
  body (Properties, Prompt, related_loops values). Subject
  substitution iterates too — same `ec` overlay applies to all
  substituted strings on the action.

## Implementation plan

**Phase 1 (this PR, ~300 LOC):**
1. `rule.Action` fields + JSON schema regen
2. `ExecutionContext.SubstituteVariablesWithIterVar` overlay
3. List-typed-triple resolution path
4. `executePublishAgent` iteration loop
5. `decide` stamps `coordinator.decision.subtopics` triple when
   action=fan_out and subtopics non-empty
6. Tests: substitution unit + iteration integration cases

**Phase 2 (component, dedicated session — design amended 2026-06-27 above):**
1. Selection brain — dependency-free pure pkg (generalize semspec's
   `coordinator` to opaque unit IDs + `depends_on` + `MarkerSet`,
   Story/owner-free) + its generalized test suite.
2. Executor component — re-eval driven by `pkg/lifecycle` `WatchAll`
   (bootstrap re-delivery = restart recovery), bounded-concurrency
   dispatch via ADR-048 BoundedDispatcher, authoritative whole-set reads
   via `graph.query.prefix`; single-flight claim-then-dispatch via a
   durable `replace_owned` marker committed *before* dispatch (mutual
   exclusion is single-flight + single-instance, NOT the marker write —
   see Load-bearing invariants); periodic stall-detection backstop;
   manages instances as ADR-047 `Participant`s.
3. Stall/cycle detection surfaced as alert (the one capability nothing
   currently has).
4. Wire `StateTracker.DeleteAllForEntity` (`processor/rule/state_tracker.go`
   — defined but **zero callers** since beta.115) so per-`(rule,entity)`
   `$state` retry budgets reset on re-dispatch; complements the consumer's
   fresh-per-run identity. A standalone latent-bug fix, valid independent
   of this feature.
5. e2e validating: `depends_on` respected under concurrent completion
   arrival; reset/re-dispatch survives an evicted terminal row (no idle
   wedge); a failed node releases its dependents; a `depends_on` cycle
   surfaces as a stall, not silent idle.

## References

- GH #134 — original parallel-fan-out issue
- GH #357 — Phase 2 ask + the 8 correctness requirements; the 2026-06-27
  amendment resolves it to the component shape (C2)
- ADR-026 — coordinator agent, references this as milestone 2
- ADR-028 — orchestration architecture (rule skeleton + coordinator + ops);
  the "no separate workflow engine" identity that rules out a DAG-aware
  rule action
- ADR-044 — file-overlap → `depends_on` serialization edges (semspec's
  consumer-side edge derivation)
- ADR-047 — Lifecycle harness substrate; the fan-out instance is a
  `Participant`
- ADR-048 — BoundedDispatcher + completion-watcher; the executor's
  bounded-concurrency + re-eval substrate
- `semspec/processor/scenario-orchestrator/component.go` — gated-DAG
  prior art (coupled); `semspec/workflow/coordinator/coordinator.go` —
  the selection brain to generalize (Story/owner coupling stripped)
- `configs/rules/deep-research/03-fan-out-subtopics.json` — the
  existing sequential fan-out shape this replaces (the comment in
  that file points here)
