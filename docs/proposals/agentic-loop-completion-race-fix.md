# Agentic-loop completion-path race — design comparison (gh#159)

**Status:** Draft for design review. Picks among three options to make ADR-046 Phase 1's `example-fan-out` reference pattern work in real-LLM scenarios.

**Filed:** gh#159 (2026-05-29) by semteams from research-pack `#fan-out-validation-3` smoke against beta.86.

## Problem (one-liner)

`WriteLoopCompletion` and `WriteLoopFailure` stamp the completion-path triples (`outcome`, `parent`, `ended_at`, `iterations`, `tokens_in/out`, `model_used`, …) through **per-triple** `writeTriple` calls. Each triple goes through `AddTripleRequest` → `AddTriples([one])` → one CAS → one `EntityState UPDATED` event. Rules that fire on `outcome=success` and substitute `$entity.triple.agent.loop.parent` in the same action evaluate against a snapshot where `outcome` is present but `parent` isn't. beta.83 (#148) correctly refuses to write the garbled subject; the counter never accumulates; the join never fires.

The bundled `processor/rule/example_fan_out_integration_test.go` constructs a fully-populated entity directly and runs the rule against it — it tests the rule logic but not the stamping cadence. The first real-LLM consumer to wire this pattern reproduced the race three runs in a row.

## Code surface (current)

```go
// processor/agentic-loop/graph_writer.go:281
triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, cost, w.platform.Org, w.platform.Platform)
for _, t := range triples {
    if err := w.writeTriple(ctx, t); err != nil { /* warn-and-continue */ }
}
```

Each iteration:

- `writeTriple` → `AddTripleRequest{Triple: t}` over NATS
- graph-ingest's `handleTripleAdd` → `AddTriples([one])` → ONE `UpdateWithRetry` CAS → ONE KV `Put` → ONE `EntityState UPDATED` event the rule engine sees

`writeBatch` already exists at `graph_writer.go:129` and is used by `WriteSyntheticDecide` for the exact reason (#133, ADR-046 Phase 1 precedent): "all three triples must land atomically — a partial write would corrupt the rule-matching contract."

graph-ingest's `AddTriples` (component.go:1333-1411) groups input triples by Subject and issues exactly one CAS per Subject. A batch of N triples on the same loop entity produces ONE KV write → ONE `EntityState UPDATED` event with all N triples in the snapshot. Per-subject atomicity is the load-bearing guarantee.

## The three design options

### Option A — Atomic batch in WriteLoopCompletion / WriteLoopFailure

**Diff shape.** Replace the per-triple loop with a single `writeBatch` call. ~6 LOC change × 2 sites + extending `writeBatch`'s godoc to cover the completion-path use case.

```go
// processor/agentic-loop/graph_writer.go:281 — proposed
triples := buildLoopCompletionTriples(...)
if err := w.writeBatch(ctx, triples); err != nil {
    w.logger.Warn("graph_writer: failed to write loop completion batch",
        "loop_id", event.LoopID, "predicate_count", len(triples), "error", err)
}
```

Same for `WriteLoopFailure` at line 309 and (worth checking) `WriteLoopCancellation` for consistency.

**What it fixes.** All completion-path triples land on the loop entity in one CAS → one `EntityState UPDATED` event → the rule engine's snapshot at action-execution time contains every completion triple. The ADR-046 Phase 1 reference pattern works as documented.

**What it doesn't fix.** Late-arriving triples that aren't part of the completion batch but the rule's action depends on (e.g., a downstream component that stamps something after persistHandlerResult returns). Those remain races. In practice, the completion-path triples are the only ones rule authors reach for in fan-out joins today — Option A covers the documented surface.

**Cost.** Loses per-triple failure granularity in the warn log: today a failure on `LoopTokensIn` doesn't take down `LoopOutcome`; under batch, a single CAS failure rolls back the whole completion stamp. Mitigation: the budgeted-write path (`stampLoopCompletionWithBudget`) already has Prom timeout instrumentation, so an operator looking at gauge drift sees the failure the same way. Loss of per-predicate error attribution is small and acceptable; the win is structural correctness.

**Atomicity sharpness.** Per-subject CAS is exactly the unit completion-path triples need — they all share the loop entity Subject. Cross-entity atomicity (which `AddTriples` does NOT provide) isn't needed here.

**Failure surface check.** If the batch CAS retries exhaust, today's per-triple path would also fail at the same writer-timeout boundary; the budget treatment is unchanged. The visible difference is "0 triples land" vs "some triples land" — and "some triples land" is exactly the bug class we're closing (partial snapshot = phantom subject).

### Option B — Stamp `agent.loop.parent` at spawn time

**Diff shape.** Three pieces:

1. At the spawn site (`component.go:855-859`, where `WriteLineageTriples` already runs), add an `agent.loop.parent` triple stamp pulled from `task.ParentLoopID`. Either extend `WriteLineageTriples` to also emit the framework-canonical `LoopParent` triple, or add a sibling `WriteParentLineage` call.
2. Drop the `agent.loop.parent` stamp from `buildLoopCompletionTriples` (`graph_writer.go:543-546`) and `buildLoopFailureTriples` (`graph_writer.go:603-611`). The completion-path is no longer the source of truth for parent.
3. Update `graph_writer_test.go:540` and `graph_writer_integration_test.go:192` expectations — the test that asserts "expected agent.loop.parent triple on failure path" stays valid (the triple still lands on the failure-path entity), but the source comment + write site move.

**What it fixes.** `agent.loop.parent` is visible from the moment the loop spawns through the rest of its lifetime. Any consumer reading it — mid-loop tool, downstream rule, trajectory step inspection, ancestry walk on a still-running child — gets a stable answer. The race window literally cannot exist for this triple because there's nothing to race with.

**What it doesn't fix.** Only addresses `agent.loop.parent` specifically. The other completion-path triples (`outcome`, `iterations`, `tokens_in/out`, `model_used`, `cost`) remain per-triple writes; any future rule that joins on `outcome=success AND $entity.triple.<other-completion-triple>` would reproduce the race shape. Doesn't generalize the way Option A does.

**Cost.**

- One extra graph write per spawn (cheap — same NATS round-trip the lineage triples already make).
- Subtle semantic shift: today `agent.loop.parent` means "this loop completed and its parent was X." Under (b), it means "this loop is a child of X" — present even mid-execution. **This is more useful, not less** — it enables patterns currently blocked on completion (e.g., "watch children's mid-loop scratchpad").
- Two-source-of-truth risk during the migration cycle: until both write sites are migrated and any operator-side rule pack updated, careful sequencing matters.

**Generalization question.** If we go (b), are there other completion-path triples whose value is known at spawn time and should also be hoisted? Candidates:

- `agent.loop.role` — definitely known at spawn (it's the rule's `Role` field). Same hoist applies.
- `agent.loop.task` — TaskID is known at spawn.
- `agent.loop.workflow` / `workflow_step` / `user` — all known at spawn.

Hoisting all of these gets us most of Option A's coverage for free, AND makes them visible mid-loop. The remaining completion-path triples that are genuinely only-known-at-completion are `outcome`, `iterations`, `tokens_in/out`, `model_used`, `cost`, `ended_at`. Those still race against each other under (b) alone; (b)+(a) together close every shape.

### Option C — Per-entity-state "completion-flushed" event guard

**Diff shape.** Frame the race semantically. Extend the rule-engine event model so consumers can opt into "fire on `EntityState UPDATED` only after the completion-event batch is fully applied" via a rule-side condition (`"on": "completion_flushed"`) or a new event variant (`EntityStateCompleted` distinct from `EntityStateUpdated`). Requires:

1. Graph-ingest emits a marker — either a new event subject or a header on the `EntityState UPDATED` event — when the completion-path batch finishes
2. Rule engine subscribes to the marker; rules with the opt-in fire on the marker, not on individual UPDATED events
3. Config schema for the opt-in
4. Docs/examples
5. Backward-compat: existing rules without the opt-in keep firing on UPDATED

**What it fixes.** Generalizes beyond completion to ANY "batch of triples logically belong together" scenario. Lets rule authors declare causal-readiness independent of stamping mechanics. Richest expressiveness of the three.

**What it doesn't fix on its own.** Requires the producer side (agentic-loop) to actually emit the marker — which means option (a)'s atomic-batch work happens anyway, just with extra event-shape complexity layered on top. Without (a), there's no "batch boundary" to mark.

**Cost.**

- Multi-package change spanning graph-ingest event model + natsclient subscription + rule engine condition matcher + config schema.
- New concept rule authors have to know about; new way to misconfigure rules.
- Backward-compat is non-trivial: existing rules SHOULDN'T silently start firing later; new rules SHOULD if they opt in. The two-mode behavior is a maintenance surface.

**When it would be worth it.** If we accumulate two or three more "stamping-cadence race" shapes in other parts of the system (graph-clustering staging triples? statistical-tier index updates?), the framework cost amortizes. Today there's exactly one place this race lives. Heavy hammer for one nail.

## Comparison

| Dimension | (a) atomic batch | (b) spawn-time stamp | (c) completion-flushed event |
|---|---|---|---|
| Closes #159 reference pattern | yes | yes (for parent specifically) | yes (requires a too) |
| Generalizes to other completion-path joins | yes | partial — hoists subset of triples | yes |
| Enables mid-loop reads of `parent` | no | **yes** | no |
| LOC change | ~12 LOC, 1 file | ~30 LOC, 2 files | ~200+ LOC, 4 packages |
| Migration surface for existing consumers | none | rule packs reading parent unchanged; tests update | rule pack opt-in to get new semantics |
| Risk of regression | low — pattern already used by `WriteSyntheticDecide` | low — pattern already used by `WriteLineageTriples` | medium — new event-model concept |
| Test coverage delta | extend existing batch tests; add real-LLM-cadence integration test | move parent-triple assertions; add spawn-time integration test | new event-model contract + per-rule opt-in tests |
| Risk of silently failing on next consumer | partly addressed — completion-batch joins safe | partly — depends which triple they join on | structurally addressed if they opt in |

## Recommendation

**Ship (a) + a constrained version of (b).**

- **(a) closes the documented surface** with minimal change and a precedent that's already shipping (`WriteSyntheticDecide`). It's the structural cure for the completion-path race class, not a one-off patch for parent specifically. ADR-046 Phase 1's `example-fan-out` reference pattern starts working in real-LLM scenarios.

- **(b) limited to spawn-known triples** (`agent.loop.parent`, `agent.loop.role`, `agent.loop.task`, `agent.loop.workflow*`, `agent.loop.user`) gives us a clean architectural win independent of the race fix: these triples become visible for the entire loop lifetime, enabling patterns currently blocked on completion. The triple semantics shift from "completed-with-parent" to "is-child-of" — more useful, and aligns with how `WriteLineageTriples` already treats lineage.

- **(c) is deferred.** Track it as a watch-item: if a second "stamping-cadence race" shape surfaces in a different subsystem within two tag cycles, revisit. Today the framework cost doesn't earn back.

This is consistent with `feedback_reactive_patches_vs_engine_completion`: instead of three reactive patches for #158/#159/#160, we ship one deliberate completion-semantics pass that names the primitive (atomic completion stamp + spawn-time identity stamp) and locks the contract.

## Test plan

The integration-test gap that let beta.84 ship the reference pattern with this race is the discipline cure surfaced in `feedback_integration_tests_must_drive_production_wire`. Whichever option lands, the new test must:

1. **Drive through the production wire.** Use real graph-ingest + agentic-loop's actual completion path (testcontainers), NOT a mock entity-state constructor that bypasses the cadence.
2. **Reproduce the staggered-stamp cadence** that real-LLM produces — multiple UPDATED events arriving microseconds apart.
3. **Assert the rule fires with the full completion snapshot** — substitution resolves, action writes, counter accumulates.
4. **Cover both the success and failure paths** since `WriteLoopFailure` has the same surface.

A failing test against current main (before the fix) is the gate that proves the test catches the bug class.

## Open questions

1. **`WriteLoopCancellation`.** Should the cancellation path get the same atomic-batch treatment for consistency? It's currently a smaller stamp (~3 triples) but the same per-triple loop. Suggest yes, for symmetry — no incremental cost.
2. **Which triples does (b) hoist?** Proposed set: `parent`, `role`, `task`, `workflow`, `workflow_step`, `user`. Anything spawn-known I'm missing? `description` (prompt) is spawn-known; debatable whether it belongs at spawn or completion.
3. **Tests asserting "parent present on failure path".** `graph_writer_test.go:540` and `graph_writer_integration_test.go:192` keep asserting parent presence. Under (b), the source comment changes ("stamped at spawn, observed at failure") but the assertion stays valid. Confirm test locality is OK.
4. **semteams workaround sunset.** The `agent.lineage.researcher-plan-entity` workaround they shipped after this race is precisely the spawn-time stamp pattern (b) generalizes. Once (a)+(b) land, semteams can retire that workaround in their rule pack; we should confirm migration cost is small.

## References

- gh#158 — text-only LLM completions strand work (same bundle)
- gh#159 — THIS issue
- gh#160 — rule substitution prefix-collision footgun (same bundle)
- ADR-046 — Phase 1 `for_each` + coordinator-as-counter; `example-fan-out` reference pack
- `processor/agentic-loop/graph_writer.go:129` — `writeBatch` (the proven pattern)
- `processor/agentic-loop/graph_writer.go:387` — `WriteLineageTriples` (option-B precedent)
- `processor/graph-ingest/component.go:1383` — `AddTriples` per-subject CAS (the load-bearing guarantee)
- memory: `feedback_reactive_patches_vs_engine_completion`, `feedback_integration_tests_must_drive_production_wire`
