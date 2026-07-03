# ADR-064: gated-DAG per-unit resource preparation (prepare-as-a-unit)

## Status

**Accepted** (2026-07-03, GH #427). Records a decision to solve per-unit
resource preparation with an **existing** gated-DAG primitive and to **reject** a
pre-dispatch prepare hook on the executor. **No framework code lands.** Extends
ADR-046 (gated-DAG dispatch component).

## Decision

For per-unit resource preparation that must happen **after** a unit's
prerequisites complete and **before** the unit's own work runs — dependency-
hydrated git worktrees, sandboxes, environment snapshots, data partitions,
container overlays — **model the preparation as a first-class gated-DAG unit**.
Do **not** add a pre-dispatch prepare/readiness hook to the executor's
claim/dispatch sequence.

Given a work unit `X` that needs a resource hydrated from its prerequisites
`{A, B}`:

1. Seed a **prepare unit** `P_X` with `P_X depends_on {A, B}`.
2. Retarget the dependent: `X depends_on {P_X}` (X transitively waits on the
   prerequisites *through* `P_X`).
3. The consumer's existing dispatch-subject handler routes `P_X` to hydration
   work — read A's and B's completed outputs, hydrate X's workspace — and writes
   `P_X`'s `Completed` marker on success or `Failed` on error.
4. When `P_X` is Done, the brain derives `X` dispatchable and gated-DAG
   dispatches `X` into the already-prepared workspace.

This needs **no new framework primitive**: the gated-DAG consumer contract
(`processor/gated-dag/doc.go`, "Consumer setup checklist") already lets a
consumer seed units, write `depends_on` edges, and wire its own handler on the
dispatch subject. A "prepare unit" is just a unit whose handler prepares a
resource instead of running an agent. `P_X` and `X` share a workspace coordinate
derived from `X`'s unit ID.

## Context

### The failure that motivated the ask (GH #427)

In a paid semspec run (2026-07-02) a CLI unit correctly declared
`depends_on: ["1"]` and gated-DAG held it until the calculator unit completed.
But semspec had **pre-created** the CLI worktree from the baseline fixture at
unit-materialization time — before the calculator branch existed. When the CLI
unit finally dispatched, its worktree lacked the completed calculator branch, so
the agent produced a shallow CLI that never called the calculator.

The workspace must be hydrated from completed-prerequisite outputs **at the
moment the DAG releases the unit** — not at materialization (prerequisite
branches do not exist yet) and not after dispatch (the task is already
published). GH #427 framed this as a missing framework hook. It is instead a
missing *unit*: the hydration is work with a dependency, and gated-DAG already
sequences work with dependencies.

### Why prepare-as-a-unit is the grain of ADR-046

ADR-046's whole thesis is *derived, never mutated*: every unit's status is a pure
function of markers + edges, re-derived on every change, with failure isolation
and stall detection riding the marker set. Modeling preparation as a unit inherits
all of it for free:

- **Exactly-once.** `P_X` rides the same durable claim marker as any unit; the
  executor's in-flight dedup makes it dispatch once.
- **Failure is VISIBLE.** A hydration failure writes `P_X`'s `Failed` marker →
  the brain derives `X` (and its subtree) `Blocked` → the wedge surfaces through
  `Stalled()` and the configured `StallEvent`. Nothing is silently stranded.
- **Reset / re-run is the existing contract.** To re-hydrate (e.g., a
  prerequisite was reset and re-ran to different content), reset `P_X` via the
  documented reset contract (clear terminal + claim markers, set dirtied); the
  brain re-derives it Ready and re-dispatches. The coordination is an explicit
  edge, not a hidden side effect.
- **No critical-path change.** No new marker, no new state, no new branch in the
  executor's claim/dispatch sequence.

## Rejected alternative: a pre-dispatch prepare hook

The intuitive design — and GH #427's literal ask — is an optional consumer
`Preparer` hook inside `executor.go:388 claimThenDispatch`, ordered
`prepare → claim → dispatch`, fail-closed by reusing the claim-failure retry
path. A draft of this ADR proposed exactly that. An adversarial review against
the source **rejected it**: the *ordering* axis is sound (it preserves invariant
#2 "claim before dispatch" and does not widen the "stranded-until-reset" window),
but the hook reintroduces the silent-stranding class ADR-046 exists to prevent.

- **Blocking — a failing prepare is an invisible wedge that blinds the whole
  fan-out's stall detector.** Fail-closed reuses the claim-failure path
  (`executor.go:397-405`), which has **no attempt cap and writes no `Failed`
  marker** — it is engineered for a claim, a pure idempotent KV write assumed
  transient. A `Preparer` is an arbitrary consumer op that can fail
  *deterministically* (bad prerequisite ref, disk full, `git worktree` index-lock
  contention). A deterministically-failing prepare leaves the unit with **no
  marker**, so the brain keeps deriving it `Dispatchable`; and `Stalled()` returns
  `nil` the instant *any* unit is dispatchable (`pkg/gateddag/gateddag.go:210`),
  so **one** spinning prepare masks the stall alert for the **entire** instance.
  The unit and its subtree never progress, the operator sees "dispatching," the
  stall gauge reads 0. That is the exact silent-stranding failure ADR-046 spent
  eight correctness wedges eliminating. "A prepare failure is indistinguishable
  from a claim failure" is the bug — a claim failure is *safe* to infinite-retry;
  a prepare failure is not.
- **Idempotency is understated to the point of re-arming the original bug.** The
  natural "reuse the resource if present" reading is skip-if-present, which is
  wrong here: a prerequisite reset→re-run to different content + a skip-if-present
  prepare no-ops → the dependent develops against **stale** state — GH #427
  reproduced by the contract meant to fix it. Correct hook idempotency is
  unconditional *reconcile-to-current-prerequisite-state*, not a cheap no-op.
- **Re-runs are not restart-bounded.** The reused claim-failure path clears the
  in-flight hint and re-selects the unit in-process, so a sustained claim outage
  (a known class with its own metric) re-executes `Prepare` on every retry — an
  expensive/side-effectful prepare amplifies against claim reliability.
- **Reset never refreshes a hook-prepared resource.** The reset contract clears
  markers but does not touch the out-of-graph resource, so the hook's fix would
  hold only on first dispatch, not on recovery.

Prepare-as-a-unit dissolves every one of these: failure becomes a `Failed`
marker (visible via `Stalled()`), exactly-once and reset ride the existing
markers, and "idempotency" becomes the consumer's ordinary unit-work reconcile —
guarded by a real `Completed` marker rather than an out-of-band skip check.

## Consequences

### Positive

- GH #427 is solved with **zero framework code**, on the primitive that already
  exists.
- Preparation failure is **operator-visible** (marker + `Stalled()` + stall
  event), not a silent wedge.
- No change to the executor's critical claim/dispatch path; ADR-046's invariants
  are untouched.

### Negative / cost

- One extra unit and one extra dispatch round-trip per prepared unit — graph
  volume and a small added latency (an eval cycle) before the dependent runs.
- The consumer derives the prepare edges (`P_X depends_on {prereqs}`,
  `X depends_on {P_X}`) and, on re-run, must reset `P_X` alongside `X`. This
  coordination is explicit (a visible edge) rather than free — but it is the same
  edge/marker machinery the consumer already drives.
- The hydration side effect still lands outside the graph (a git worktree is not
  a triple), but it is now guarded by `P_X`'s `Completed` marker, so it is
  genuinely once and its failure is visible.

### Risks

- Per-prepared-unit consumer boilerplate (seed `P_X`, wire edges, reset
  coordination) repeated across consumers. If that proves painful across **≥2**
  consumers, revisit a framework *convenience* — e.g., an "auto-derive a prepare
  unit for units carrying a `needs_prepare` marker" helper that emits the `P_X`
  entity and edges — **not** the rejected dispatch hook. Do not add the
  convenience preemptively (framework-vs-product boundary).

## Open questions

- **Workspace lifetime on reset.** When `X` is reset and re-run, should the
  consumer reconcile the existing workspace, tear it down and re-hydrate, or
  preserve a failed workspace for forensics? Consumer policy, but worth a
  documented default (lean: reconcile-to-current-prerequisite-state, which a
  reset-then-re-dispatched `P_X` performs naturally).
- **Shared-substrate contention.** A wide fan-out runs many `P_X` hydrations
  against one base repo; `git worktree add` takes repo-level locks. The
  consumer's hydration handler must tolerate/serialize this — a general concern
  for any per-unit resource sharing a substrate, not specific to this decision.

## Related decisions

- **ADR-046** — parallel fan-out + gated-DAG dispatch (the primitive reused here).
- **ADR-047 / ADR-048** — lifecycle harness + bounded dispatcher substrate the
  executor composes.

## References

- GH #427 — the filing and the semspec worktree-hydration failure.
- `processor/gated-dag/doc.go` — consumer setup checklist + reset contract (the
  contract that makes prepare-as-a-unit possible today).
- `processor/gated-dag/executor.go:388` — `claimThenDispatch` (the sequence the
  rejected hook would have modified).
- `pkg/gateddag/gateddag.go:206` — `Stalled()` (returns nil when any unit is
  dispatchable; the mechanism the rejected hook would have blinded).
