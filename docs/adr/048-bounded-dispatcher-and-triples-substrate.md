# ADR-048: BoundedDispatcher + `.triples` Substrate Primitives

## Status

**Accepted** — 2026-05-28. Shipped alongside [ADR-047](047-lifecycle-harness-substrate.md)
in the 4-PR bundle tagged v1.0.0-beta.85. The companion lifecycle e2e
tier exercises the BoundedDispatcher contract indirectly via the
Manager — `.triples` substitution is exercised by the `phase3FireJoin`
integration test in `processor/rule`.

Originally proposed 2026-05-24. Co-locates two small substrate completions:
promoting the existing `pkg/worker.Pool` to a first-class framework
concurrency primitive with KV-twofer integration, and adding the
`.triples` enumeration substitution that closes the rule-engine
multi-valued primitive set.

These two primitives are bundled because both are unambiguous-win
substrate completions that the Lifecycle harness (ADR-047) and
consumers compose against. Together they retire the
fan-out-patch reactive pattern (beta.80-84) by naming what the
primitives ARE.

## Context

### BoundedDispatcher — promotion of existing pkg/worker

`semstreams/pkg/worker` already ships a generic, thread-safe
bounded worker pool with backpressure, observability, and
graceful shutdown (see `pkg/worker/doc.go`). One semstreams
consumer uses it (`processor/graph-index`).

`semspec/processor/scenario-orchestrator/` reinvents an equivalent
pattern in ~150 LOC of goroutine-pool + semaphore + WaitGroup +
error-channel code. The pattern is identical in shape; the
discoverability is missing.

The dispatcher pattern also recurs in the Lifecycle harness
(ADR-047) wherever a component does internal parallel work over a
known list — drone fleet weather-monitor walking all active
missions, scenario-orchestrator dispatching ready requirements
under DAG gating, manufacturing batch's per-widget station
processing.

### `.triples` enumeration — completion of the multi-valued primitive set

The five-tag pile (beta.80-84) added rule-engine primitives in
small reactive pushes:

- beta.80: `tool_choice` + synth-decide on terminal-tool-less completion
- beta.82: `for_each` declarative iteration + decide-emits-subtopics-triple
- beta.83: Subject override + array operators (`length_eq`, `length_gt`, `length_lt`, `array_contains`)
- beta.84: `.length` substitution + condition.Value resolution

GH #151 was filed for the next gap (enumerate sibling loops) with
two candidate paths: `read_loop_children` tool (~200 LOC) OR
`.triples` plural substitution (~30 LOC).

The honest framing per [[feedback_reactive_patches_vs_engine_completion]]:
these are all rule-engine primitives that compose for fan-out / ops
aggregation / chain walks / DAG dispatch / multi-entity reasoning —
NOT fan-out patches. `.triples` is the last primitive in the
multi-valued set that the previous tags incrementally built.

`.triples` mirrors `.length` semantically: where `.length` resolves
to the count of triple values for a multi-valued predicate,
`.triples` resolves to the values themselves as an iterable.
Composes with the existing `array_contains`, `length_eq`, and
`for_each` primitives for the patterns these would otherwise
require app-side enumeration of.

### Why bundle these two

Both are:

- Substrate-level (NOT rule-engine semantics changes; not workflow
  primitives)
- Small (combined ~200-300 LOC)
- Unambiguous wins (validated by multiple consumers)
- Required for the Lifecycle harness consumers to express common
  patterns without ad-hoc workarounds

Bundling them with ADR-047 in one tag completes the substrate
narrative: **substrate primitives (BoundedDispatcher, .triples) +
substrate convention (Lifecycle harness) = workflow-shaped framework
substrate**. One coherent push.

## Decision

### 1. BoundedDispatcher — promote `pkg/worker.Pool` to first-class

Create `pkg/dispatch` package wrapping `pkg/worker.Pool[T]` with
optional KV-twofer completion integration.

#### Public API

```go
package dispatch

import (
    "context"
    "log/slog"
    "github.com/c360studio/semstreams/natsclient"
    "github.com/c360studio/semstreams/pkg/worker"
)

// BoundedDispatcher is the framework-provided primitive for
// bounded-concurrency parallel work. Components compose it for
// internal fan-out over known work lists.
//
// Use when:
//   - A component does internal parallel work over a list of items
//   - Each work item completes async and the dispatcher should fire
//     OnComplete when KV signals match (optional)
//   - Bounded concurrency is required (semaphore-backed)
//
// Do NOT use for:
//   - At-the-rule-layer fan-out (use rule engine's for_each instead)
//   - Sequential per-item processing (use a plain loop)
//   - Unbounded concurrency (use a bare goroutine pool)
type BoundedDispatcher[W any] struct {
    pool       *worker.Pool[W]
    completion *completionWatcher[W]  // optional
    logger     *slog.Logger
}

type Config[W any] struct {
    // Workers is the bounded concurrency target. The dispatcher
    // never runs more than Workers items at a time.
    Workers int

    // QueueSize bounds the submit queue. Submit returns
    // ErrQueueFull when full.
    QueueSize int

    // Process is called for each submitted work item, in one of
    // the worker goroutines.
    Process func(ctx context.Context, work W) error

    // CompletionKVBucket — optional. When set, the dispatcher
    // watches this bucket for completion signals and calls
    // OnComplete with the matched work item.
    CompletionKVBucket string

    // CompletionKeyForWorkItem — required if CompletionKVBucket set.
    // Returns the KV key the dispatcher watches for this work
    // item's completion.
    CompletionKeyForWorkItem func(W) string

    // OnComplete — called when CompletionKVBucket has a write at
    // the key returned by CompletionKeyForWorkItem.
    OnComplete func(ctx context.Context, work W) error
}

// New creates a new dispatcher. Returns ErrInvalidConfig for
// missing required fields.
func New[W any](ctx context.Context, cfg Config[W], deps Deps) (*BoundedDispatcher[W], error)

// Submit queues a work item. Returns ErrQueueFull if the queue
// is at capacity.
func (d *BoundedDispatcher[W]) Submit(work W) error

// Stop halts the dispatcher gracefully, waiting for in-flight
// work items to complete.
func (d *BoundedDispatcher[W]) Stop(ctx context.Context) error

// Stats returns current dispatcher statistics.
func (d *BoundedDispatcher[W]) Stats() worker.Stats

type Deps struct {
    NATSClient *natsclient.Client
    Logger     *slog.Logger
}

var (
    ErrInvalidConfig = errors.New("dispatch: invalid config")
    ErrQueueFull     = worker.ErrQueueFull  // re-export
    ErrStopped       = worker.ErrPoolStopped  // re-export
)
```

#### Properties

- **NOT a workflow engine** — no DAG semantics, no branching, no
  lifecycle.
- **NOT a rule-engine extension** — rules don't gain new fan-out
  primitives.
- **IS a substrate primitive** — components compose it into their
  internal fan-out logic.
- **IS KV-twofer-aware** — optional CompletionWatcher closes the
  read-completion-from-KV pattern (semspec's scenario-orchestrator
  spells this out manually today).
- **Generic over work type W** — work items can be any Go type.
- **Bounded queue with backpressure** — `ErrQueueFull` on overflow;
  caller chooses retry, drop, or backpressure-propagate.
- **Inherits pkg/worker observability** — statistics + optional
  Prometheus metrics.

#### Implementation

`pkg/dispatch` wraps `pkg/worker.Pool[T]` directly. The
`completionWatcher` is a small (~80 LOC) wrapper around
`natsclient.KVWatch` that:

1. Subscribes to `CompletionKVBucket` on dispatcher start
2. Tracks in-flight work items by `CompletionKeyForWorkItem(work)`
3. On KV-watch update, looks up the matching work item and calls
   `OnComplete`
4. Removes from tracking on completion

The dispatcher does NOT track completions when `CompletionKVBucket`
is unset; the pool behaves as a plain bounded worker pool.

#### Migration: pkg/worker → pkg/dispatch

`pkg/worker` stays. New uses prefer `pkg/dispatch.BoundedDispatcher`
(higher-level, KV-twofer-aware). Existing `pkg/worker.Pool` consumers
(`processor/graph-index`) keep using the pool directly — no
forced migration.

`pkg/dispatch` essentially is a `pkg/worker.Pool` constructor
preset for framework-typical patterns: bounded queue, slog-bound,
KV-twofer-completion-aware.

#### First downstream consumer

`semspec/processor/scenario-orchestrator/` becomes the first
downstream test case. The ~150 LOC dispatch goroutine pool +
semaphore + WaitGroup + error channel collapses to:

```go
d, err := dispatch.New[*workflow.Requirement](ctx, dispatch.Config[*workflow.Requirement]{
    Workers:                   c.config.MaxConcurrent,
    QueueSize:                 256,
    Process:                   c.processRequirement,
    CompletionKVBucket:        "EXECUTION_STATES",
    CompletionKeyForWorkItem:  func(r *workflow.Requirement) string {
        return "req." + r.Slug + "." + r.ID
    },
    OnComplete: c.onRequirementComplete,
}, dispatch.Deps{NATSClient: c.natsClient, Logger: c.logger})

// later in dispatchRequirements
for _, req := range filterReadyRequirements(...) {
    if err := d.Submit(req); err != nil {
        // handle ErrQueueFull
    }
}
```

That collapses ~150 LOC of orchestrator state-management to ~20
LOC of dispatcher use.

### 2. `.triples` enumeration substitution

Add `.triples` substitution suffix to `processor/rule/`'s
substitution layer.

#### Semantic

For a multi-valued triple predicate `P` on entity `E`:

- `$entity.triple.P` resolves to the FIRST value (existing
  back-compat behavior for single-valued semantics)
- `$entity.triple.P.length` resolves to the COUNT of values
  (shipped beta.84)
- `$entity.triple.P.triples` resolves to the LIST of all values
  (this ADR)

Symmetric path for related entities: `$related.triple.P.triples`.

#### Compose patterns

The `.triples` substitution composes with existing primitives:

- **Fan-out** — `for_each` over `$entity.triple.subtopics.triples`
  iterates the children
- **Aggregation** — `array_contains` against
  `$entity.triple.completed.triples` checks set membership
- **Cardinality** — `length_eq` against `.length` of one predicate
  vs the count of another's `.triples`

#### GH #151 supersession

GH #151 filed for sibling-loop enumeration. The `.triples`
substitution satisfies #151's join-pattern need:

```json
{
  "name": "synthesize_when_all_children_complete",
  "when": {
    "conditions": [
      {"field": "$entity.triple.children.length", "op": "eq",
       "value": "$entity.triple.completed_children.length"}
    ]
  },
  "actions": [
    {"type": "publish_agent", "role": "synthesizer",
     "prompt": "Children completed: $entity.triple.completed_children.triples"}
  ]
}
```

The earlier `read_loop_children` tool proposal (~200 LOC) is
superseded; `.triples` substitution does it in ~50-100 LOC with
broader applicability.

#### Implementation

`.triples` extends the existing substitution-evaluation path
that powers `.length`:

- Detect `.triples` suffix in `$entity.triple.X.triples` and
  `$related.triple.X.triples` substitution paths
- Resolve to the slice of values (already typed `[]any` after
  `for_each`'s multi-valued resolution shipped in beta.82)
- For string-context substitution (e.g., inside a prompt or a
  property value), serialize as a **JSON array** —
  `["a","b","c"]` — never CSV. JSON wins because it matches the
  existing graph encoding for list-typed predicates (e.g.
  `coordinator.decision.subtopics`, see
  `vocabulary/agentic/predicates.go:677`) and because CSV
  ambiguity on commas-in-values is a latent production-bug class
  the framework refuses to introduce. There is no operator-config
  flag for serialization — one canonical format, period.
- For typed-context substitution (e.g., inside a condition
  operator like `array_contains` or `length_eq`), pass through
  as `[]any` — no string serialization happens.

#### Persona-prose convention for `.triples` recipients

When a rule threads a `.triples`-substituted value into a
downstream agent's prompt or property, the agent's persona MUST
include the canonical parse instruction (one templated sentence
per consuming role):

> *"The property `<field_name>` contains a JSON array of
> `<item_type>` (e.g. `["loop_a","loop_b","loop_c"]`). Parse it
> as JSON and iterate each item."*

The framework guarantees JSON-array format; consumer prose
guarantees the parse expectation. This converts "cognitive load on
every consumer" into one copy-pasteable sentence, per the
discipline in [[feedback_persona_prose_needs_decision_criteria]].

**Known limitation — cheap-model substrate:** The JSON-parse step
assumes a reliable JSON-parsing model. Frontier models (the
`general` model class in this codebase) handle this trivially.
Cheap-model substrate (the `decide`-class roles from the beta.80
cheap-model bundle) may not — silent malformed-parse is a real
failure surface. When a `.triples`-threaded value flows into a
cheap-model context, prefer static-N + AND-composition patterns
(or upgrade the recipient role to `general`). Operator guidance:
if a cheap-model role consumes `.triples`, instrument the
trajectory for "did the model successfully iterate every item?"
before relying on the pattern in production.

Test coverage extends `test/reference_configs_test.go` (the
beta.84 lint) — any reference config using `.triples` must
reference a predicate that's framework-stamped or allowlisted as
rule-stamped.

## Bundle plan

Both primitives ship in PR 4 of the ADR-047 bundle (see ADR-047
"Bundle plan" section):

### PR 4 — BoundedDispatcher + `.triples` (~200-300 LOC + tests)

- `pkg/dispatch/` (BoundedDispatcher + completionWatcher + Config)
- `pkg/dispatch/dispatch_test.go` (unit tests)
- `pkg/dispatch/integration_test.go` (with KV-twofer completion)
- `.triples` substitution in `processor/rule/execution_context.go`
- `processor/rule/substitution_triples_test.go`
- Reference config example using `.triples` for fan-in
- Update `test/reference_configs_test.go` to lint `.triples`
  references
- Concept doc 14 update — add BoundedDispatcher to substrate
  primitive section

The full bundle (ADR-047 PR 1+2+3 + ADR-048 PR 4) ships as one
tag: **"Lifecycle harness substrate + BoundedDispatcher +
`.triples` — workflow-shaped framework primitives (ADR-047 +
ADR-048)"**

## Consequences

### Positive

- **Substrate completion narrative**: the five-tag-pile pattern
  (beta.80-84) is closed with one deliberate completion tag rather
  than another reactive patch.
- **GH #151 closes cleanly**: `.triples` satisfies the filed need
  and supersedes the `read_loop_children` proposal.
- **semspec scenario-orchestrator refactor target**: ~150 LOC
  collapses to ~20 LOC of dispatcher use.
- **pkg/worker stays**: existing consumers untouched; pkg/dispatch
  is the higher-level recommended path forward.
- **Discipline restored**: per
  [[feedback_reactive_patches_vs_engine_completion]], the bundle
  is titled what it IS (substrate completion) rather than what it
  FIXES (#151).

### Negative

- **Two new substrate APIs to maintain**: `pkg/dispatch` and
  the `.triples` substitution. Each is small, but the surface
  grows.
- **Documentation work**: concept doc 14 update + the new
  reference configs.
- **Generic Go in framework code**: `pkg/dispatch.BoundedDispatcher[W]`
  uses generics. semstreams already uses generics in pkg/worker;
  not a new pattern but worth noting.

### Risks

- **Naming overlap**: "dispatcher" overlaps with NATS subjects
  ("dispatch" stream), with the "BoundedDispatcher" sketched in
  the workflow-primitives proposal, and potentially with rule
  engine internal "dispatch" terms. Mitigation: package docs
  explicit about `pkg/dispatch.BoundedDispatcher` as the framework
  primitive; rule-engine internal terminology stays separate.
- **`.triples` overload with the existing `Triples()` method on
  `Graphable`**: `Graphable.Triples()` returns the triples of an
  entity; `.triples` suffix is a substitution path resolving to
  values of a specific predicate. Different conceptual layers,
  but the word reuse can confuse. Mitigation: concept doc spells
  out the distinction; reference configs make the substitution
  use clear.
- **Completion-watcher race conditions**: subscribing to KV after
  Submit could miss completion signals that landed between Submit
  and Watch-subscribe. Mitigation: dispatcher subscribes BEFORE
  any Submit; tests cover the race window.

## Open questions

1. **`pkg/dispatch` package location** — alongside `pkg/worker` in
   `pkg/`, or under `natsclient/` since it's natsclient-aware?
   Lean `pkg/dispatch` — substrate primitive deserves its own
   package; natsclient is a dep not a parent.
2. **Completion watcher lifecycle** — should it survive
   `Stop()` and re-subscribe on restart, or stop with the
   dispatcher? Lean stop-with-dispatcher; restart is the consumer's
   concern.
3. **Should `.triples` substitution support `.triples.length`?**
   It would be `.length` of the resolved list. Lean YES for
   symmetry; semantically equivalent to `.length` directly.
4. ~~**JSON-vs-CSV serialization of `.triples` in string context**~~
   **RESOLVED** — JSON array, no operator-config flag, canonical
   persona-prose template + cheap-model limitation documented in
   the Implementation section above. CSV rejected because
   commas-in-values is a latent production-bug class the framework
   refuses to introduce.

Open questions 1-3 resolve during PR drafting.

## Migration path

No migration. Both primitives are additive:

- `pkg/worker.Pool` keeps its existing API and consumers
- `pkg/dispatch.BoundedDispatcher` is new and unused until
  consumers adopt
- `.triples` substitution path is new; no existing rules use it

semspec scenario-orchestrator refactor to use BoundedDispatcher
is a follow-up PR in semspec, not in this bundle.

## Related decisions

- ADR-028 — Orchestration architecture (foundation)
- ADR-046 — Parallel fan-out + gated DAG dispatch (Phase 2
  superseded; BoundedDispatcher addresses the dispatch concern;
  `.triples` addresses the enumeration concern)
- ADR-047 — Lifecycle Harness Substrate (companion ADR; the
  bundle's larger work)
- GH #151 — sibling enumeration filing (superseded; `.triples`
  ships)

## References

- [Workflow Primitives Design Resolutions](../proposals/workflow-primitives-design-resolutions.md) — the 8 settled choices including the `.triples` over `read_loop_children` decision
- `pkg/worker/` — existing bounded worker pool that BoundedDispatcher wraps
- `semspec/processor/scenario-orchestrator/component.go` — prior art that becomes first BoundedDispatcher consumer
- `processor/rule/execution_context.go` — substitution layer that `.triples` extends
- `test/reference_configs_test.go` — lint test that extends to `.triples` coverage
