# semspec/workflow Mapping — Concrete Harness-Slice Verification

**Status**: Research draft, 2026-05-24. Companion to
[`workflow-primitives-decision.md`](workflow-primitives-decision.md),
[`workflow-primitives-robotic-sketch.md`](workflow-primitives-robotic-sketch.md),
and [`workflow-primitives-semconnect-sketch.md`](workflow-primitives-semconnect-sketch.md).
Walks `semspec/workflow/` file-by-file to verify the ~500-LOC
harness-slice estimate against actual code.

## Method

For each file/subpackage in `semspec/workflow/`:

- **HARNESS**: code the framework primitive would directly replace
  (no domain references, generic pattern)
- **PATTERN**: code the framework primitive would obviate by
  providing the equivalent shape (semspec-specific names, but the
  shape is generic and gets reinvented per-consumer)
- **HYBRID**: file contains both
- **DOMAIN**: pure semspec-specific business logic; stays in app

LOC counts are non-test only.

## File-by-file walkthrough

### Pure HARNESS (~290 LOC) — should literally move to framework

| File | LOC | Why harness |
|---|---|---|
| `entity_id.go` | 28 | `HashInstanceID(parts...)` — produces compact dot-free instance segment for 6-part entity IDs. Generic; semstreams's 6-part-ID convention should provide this helper. |
| `entity_prefix.go` | ~30 (of 70) | Org/platform prefix conventions + slugification logic. Convention layer; defaults are semspec-specific (`DefaultOrg = "semspec"`) but the *mechanism* is harness. |
| `kv_helpers.go` | 80 | `WaitForKVBucket`, `WaitForStream`. Comment in the file explicitly states: *"Should move to natsclient as a framework primitive."* The lift is queued; nothing's blocking it. |
| `dispatchretry/retry.go` | ~150 | Generic retry primitive with `Config{MaxRetries, BackoffMs}`, jittered backoff, per-key Entry state. **semstreams already has `pkg/retry`** — this is duplication semspec built because either (a) `pkg/retry` came later or (b) the existing one didn't fit. Either way, lifting eliminates the duplication. |

**Subtotal: ~290 LOC of code that should literally relocate to
semstreams**.

### Harness PATTERN (~190 LOC) — framework primitive obviates by providing equivalent

| File | LOC | Why pattern |
|---|---|---|
| `execution.go` (pattern portion) | ~50 (of 201) | Key-shape funcs (`TaskExecutionKey`, `RequirementExecutionKey`), `IsTerminalTaskStage`, `IsTerminalReqStage`. The KV key convention + terminal-state detection are harness shape. The struct definitions themselves (`TaskExecution` with 40 fields, `RequirementExecution`) are domain. |
| `subjects.go` (pattern portion) | ~30 (of 224) | Per-event subject namespace + payload-base shape. The specific event types (`RequirementsGeneratedEvent`, etc.) are domain; the *pattern* of per-event subjects + envelopes is harness. |
| `cancellation/signal.go` | ~30 (of 61) | Cancellation signal envelope with `LoopID + Reason`. The pattern is harness (any workflow needs abort signaling); LoopID-vs-EntityID and the specific subject (`agent.signal.cancel.<loopID>`) are semspec-shaped. |
| `error_category.go` + `error_class.go` (pattern portion) | ~50 (of 213) | Error categorization mechanism. The specific categories (`ErrorClassAgent` vs `ErrorClassInfrastructure`) are semspec-shaped; the *idea* of a typed error class registry as a lifecycle annotation is harness. |
| `recoveryhint/` (pattern portion) | ~30 (of ~240) | "Emit a recovery hint when a tool fails" envelope. Specific tool-recovery semantics are domain (`graph_query` lookups, etc.); the *envelope* pattern is harness. |

**Subtotal: ~190 LOC of patterns the framework primitive replaces by
providing the shared shape**.

### HYBRID (~60 LOC harness portion)

| File | LOC harness | Why hybrid |
|---|---|---|
| `graph_marshal.go` | ~60 (of 177) | `writePlanTriples`/`writeRequirementTriples` are domain logic (each field of each domain struct → one triple). The pattern of "marshal a struct's fields to triples on an entity" is harness-shaped (a generic reflection-based marshaller would do it). `semstreams/graphutil` already provides `TripleWriter` for the low-level write; an `EntityMarshaler` harness layer is missing. |

### Pure DOMAIN (~7,300 LOC) — stays in semspec

This is the bulk of the package. Examples:

- `task.go`, `research.go`, `question.go`, `project.go` — domain entity types
- `plan.go`, `plan_artifacts.go`, `plan_decision.go`, `plan_requirement.go`, `plan_review_result.go`, `plan_scenario.go` — Plan domain logic
- `execution.go` (struct portion) — TaskExecution + RequirementExecution structs with ~40 fields each
- `detection.go` (1199 LOC) — incident detection logic
- `aggregation/review.go` (375 LOC) — review aggregation logic
- `cascade/cascade.go` — PlanDecision dirty-cascade business logic
- `phases/constants.go` (236 LOC) — semspec-specific phase names ("decomposing", "executing", "approved", "escalated", etc.)
- `payloads/` (~1000 LOC) — semspec-specific message payloads
- `validation/` (~559 LOC) — semspec-specific validation
- `lesson.go`, `lessons/` — semspec's lesson learning system
- `parseincident/` — incident parsing
- `jsonutil/`, `graphutil/`, `indexing_gate.go` — partial-helper packages mostly semspec-shaped
- `subjects.go` (event struct portion ~190 LOC), `error_category.go` (registry content ~100 LOC) — specific events/categories
- All `*_test.go`, `types*.go` — testing infrastructure

**Subtotal: ~7,300 LOC stays in semspec**.

## Totals

| Category | LOC |
|---|---|
| Pure harness (literal lift) | ~290 |
| Harness pattern (framework primitive obviates) | ~190 |
| Hybrid (harness portion) | ~60 |
| **Total harness slice** | **~540 LOC** |
| Pure domain (stays in app) | ~7,300 |
| **Total semspec/workflow top-level** | **~7,840 LOC** |

The 500-LOC estimate from the drone sketch was approximately correct
(~540 LOC actual). The harness slice is ~7% of semspec/workflow's
top-level code.

## What the LOC number doesn't capture

The harness primitive's value is **not primarily LOC reduction**.
It's standardization across consumers. The 540 LOC of harness slice
in semspec is reinvented:

- Slightly differently by each consumer (semspec, semconnect when
  it grows to lifecycle, future products)
- With slightly different naming conventions (Phase vs Stage vs
  Status, completed vs done vs finished)
- With slightly different KV key shapes (semspec uses `task.<slug>.<taskID>`,
  another product might use `<workflow>.<id>`, another `instance:<uuid>`)
- With slightly different terminal-detection conventions
- With slightly different subject namespaces
- With slightly different error categorizations

The standardization wins:

1. **One operator API across products.** `GET /workflows` works the
   same against semspec, semconnect, drone-survey-co.
2. **One audit trail convention.** Phase-transition history is
   readable by any operator tooling, not just semspec-aware tooling.
3. **One restart-recovery story.** The harness's restart semantics
   apply uniformly; consumers don't roll their own state-replay.
4. **One migration story when conventions evolve.** semspec retired
   `workflow/reactive/` (7,264 LOC) — that retirement was per-
   consumer because the conventions were per-consumer. A future
   framework-conventioned harness change migrates by a single
   semstreams release, not per-product.
5. **Cross-product tooling becomes possible.** A unified workflow
   dashboard, a unified per-instance trace viewer, a unified
   workflow-instance count-by-phase metric — all are framework-
   native, not per-product re-implementations.

## What the analysis confirms

- The harness shape is **real, ~540 LOC of code**, not a phantom
  cluster
- It's **broadly applicable** — appears in semspec's hand-rolled
  reality, fits the drone-survey sketch, fits the semconnect
  per-resource sketch
- The lift is **bounded** — no one file is a runtime; no one piece
  needs DSL or state-machine logic
- The standardization value **exceeds** the LOC-replacement value
- Two of the harness slice items (kv_helpers.go's WaitForKV*,
  dispatchretry's retry primitive) are **explicitly flagged in
  comments** as "should move to framework" — confirming the demand
  is already documented in code

## Outcome B' commitment-readiness

Three consumer-class sketches (drone-survey, semconnect, semspec
prior art) all map to the same harness shape. The harness lift is
~540 LOC of code-replacement + standardization across N consumers.
The bundle estimate (~1800-2650 LOC) accounts for harness substrate
+ BoundedDispatcher + .triples + rule integration + operator API
+ tests.

**The evidence supports committing to outcome B'.**

The remaining design choices (multi-tenancy, $now substitution,
operator-writability mechanism, dashboard ship-or-not) are
ADR-047-drafting concerns, not exercise-resolution concerns.

## Recommended next step

Amend the decision document to reflect outcome B' and draft ADR-047
skeleton. The mapping evidence + two consumer-class sketches +
semspec prior art make a tight case. Sketching P4 (manufacturing
batch) for parent/child workflow shape is optional — the user's
Q2 answer already specified parent/child folds into ADR-047 as an
optional Participant method, so P4 evidence doesn't change the
design call.

## What's still TBD for ADR-047

These design choices need resolution in the ADR draft, not before:

1. `Manager.List` filter signature (multi-tenancy)
2. `$now` vs cron-only for temporal conditions
3. Per-field operator-writability mechanism (struct tags vs method)
4. Cross-product dashboard ship-or-not
5. Atomic multi-field transitions in rule actions (sketch C from
   drone gap 1)
6. Arithmetic in substitution OR alternative composition (drone
   gap 2)
7. KV indexing for `Manager.List` performance (drone gap 6)
8. Phase-transition validation: declared transitions table vs
   freeform (drone gap 5)

Each is bounded; the ADR draft can decide each in isolation.
