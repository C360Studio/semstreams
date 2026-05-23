# ADR-046: Parallel Fan-Out — `for_each` primitive + gated-DAG dispatch

## Status

**Proposed.** Phase 1 (`for_each` foundation) targets beta.82. Phase 2
(gated-DAG dispatch) defers behind operator validation of Phase 1 and a
dedicated implementation session. Closes ADR-026 milestone 2 (parallel
flow composition) which has been deferred since ADR-026 shipped Phase 1.
References GH #134.

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

Forward-compat: a future `fan_out_gated` action (Phase 2) is additive.
Rule packs that use Phase 1's `for_each` for no-deps flows don't need
to migrate; they coexist.

### Phase 2 — `fan_out_gated` (post-beta.82, separate ADR-046 amendment)

Lift semspec's `scenario-orchestrator` pattern to framework as a new
action type or workflow primitive:

- Accepts a list of work items with `depends_on` edges (an internal DAG).
- Completion-watcher integration (NATS KV watch on per-flow completion
  bucket).
- Stateless re-evaluation on each completion update: reconcile from
  KV, filter to currently-ready, dispatch under bounded concurrency.
- Implicit join (no-ready-left = done).
- Optional `max_concurrent` config knob, semaphore-based.

Implementation references `semspec/processor/scenario-orchestrator/component.go`
as prior-art canonical implementation. Open questions deferred to
Phase 2 design:

- Where the DAG lives: in the rule (declared inline), in a graph
  entity, in a flow definition.
- Completion source-of-truth bucket name and key shape.
- Failure semantics: stop-on-first-failure vs continue-others vs
  retry-with-backoff per node.
- Integration with the cheap-model substrate work (beta.80): if a
  child loop completes via synthetic-decide on terminal-tool-less
  output, does the gated dispatcher treat that as "complete" for
  dependent dispatch?

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

**Phase 2 (post-beta.82, separate PR with design amendment to this ADR):**
1. Design amendment: completion-bucket layout, DAG storage shape,
   failure semantics
2. `fan_out_gated` action implementation
3. Completion watcher + stateless re-eval scheduler
4. Bounded concurrency
5. e2e scenario validating depends_on respected under concurrent
   completion arrival

## References

- GH #134 — original parallel-fan-out issue
- ADR-026 — coordinator agent, references this as milestone 2
- ADR-028 — orchestration architecture (rule skeleton + coordinator + ops)
- `semspec/processor/scenario-orchestrator/component.go` — gated-DAG
  prior art
- `configs/rules/deep-research/03-fan-out-subtopics.json` — the
  existing sequential fan-out shape this replaces (the comment in
  that file points here)
