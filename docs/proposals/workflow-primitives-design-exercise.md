# Workflow Primitives Design Exercise

**Status**: Proposed — 2026-05-24. Pre-ADR. Resolution gates resumption of ADR-046 Phase 2, GH #151, and any new rule-engine primitive work for fan-out / parallel / lifecycle patterns.

**Supersedes** the rules-engine-primitive-mapping framing in `project_rules_engine_design_review` memory (the original pause-point analysis). That memo correctly diagnosed the five-tag pile (beta.80–beta.84) and identified the design-review pause point. This proposal broadens the question: workflow primitives should be considered as a deliberate framework concern rather than ruled out by dogma.

## Summary

For the past week, the framework has been adding workflow-engine primitives to the rules engine (for_each, .length, array operators, condition.Value resolution, Subject override, tool_choice plumbing) while telling itself it isn't building a workflow engine. The reactive feel of those tags came from a stated discipline ("no separate workflow engine") that was a reasonable safety reaction to semspec's specific failure mode but generalized too far. With multiple consumer classes (agentic, robotic, API server, hybrid) all needing workflow semantics, the framework should decide deliberately whether to ship workflow primitives as a first-class layer on top of the rules engine.

This exercise is the deliberate design session that resolves the question.

## Background

### How we got here

Five tags in five days (beta.80–beta.84) added what was framed as "ADR-046 Phase 1 fan-out patches." In honest hindsight, every primitive shipped was a general workflow-engine capability:

- `for_each` — dynamic iteration over collections
- `array_contains`, `length_eq`, `length_gt`, `length_lt` — collection predicates
- `.length` substitution — collection introspection
- Subject override — cross-entity targeting
- `condition.Value` substitution — late-binding in conditions
- `tool_choice` on rule.Action + synth-decide on terminal-tool-less completion — per-spawn constraint + recovery primitive

The per-tag framing made each look reactive. The honest framing makes them coherent: **the rules engine has been growing workflow-runtime capabilities, just incrementally and without naming what it's doing.**

Two observations crystallize the question now:

1. **semspec's actual failure mode** was not "having a workflow engine." It was *"having a workflow engine that bypassed the rules engine, lived outside flow discovery, broke the flowgraph validator, and drifted from rule semantics over time."* A workflow engine that USES the rules engine as dispatch substrate, lives inside the component framework, and shares state semantics with rules would not have hit any of those failure modes. The dogma absorbed the wrong lesson.

2. **The framework's consumer base is broader than agentic.** semconnect is an OGC Connected Systems API server. Robotic automation is a near-term legitimate use case for the substrate (NATS + graph + rules naturally fit sensor/actuator coordination). Robotic patterns surface workflow semantics — long-running coordination, mission lifecycle, state persistence across restarts, real-time deadlines, multi-actuator fan-out, versioned process definitions, operator dashboards — with essentially zero LLM dependency. If we design workflow primitives only around agentic needs, we'll bake in assumptions (LLM-call as primary work unit, sub-second loops, soft deadlines) that miss robotic and other event-driven use cases entirely.

### The "rules sequence, components parallelize" reframe (Path X)

A parallel design conversation arrived at the discipline:

> **Rules sequence. Components parallelize. Components compose a framework-provided bounded-dispatch primitive for capacity-controlled parallel work.**

This is correct at the discipline level and resolves the dynamic-N-agent-fan-out question by reframing it: dynamic-N parallel work belongs in code components (ADR-045 PR 4 pattern), not as agent-loop fan-out at the rule layer. `for_each` over agent loops is scoped to static-N cases with per-loop isolation.

What that discipline doesn't address: **the higher-order workflow concerns (instance lifecycle, state tracking, versioning, introspection) that are shared across multiple consumer classes.** Those are what this exercise evaluates.

### The robotic angle

Robotic automation surfaces workflow needs that are difficult-to-impossible to express in rules + components alone:

| Pattern | Rules engine today | Workflow primitives could |
|---|---|---|
| Mission lifecycle (start, in-progress, abort, complete) | Implicit via state matching | Make first-class with operator-meaningful API |
| State persistence across reboots | App-side KV conventions | Framework-managed instance state |
| Long-running coordination (hours to days) | Possible but no introspection | Operator dashboards, per-instance metrics |
| Versioned process definitions | Implicit | Explicit version pinning per instance |
| Compensation / safe-abort | Per-rule conditions | Lifecycle-aware compensation hook |
| Multi-sensor fan-in synthesis | Per-rule aggregation (hard) | First-class join with completion semantics |
| Operator visualization | Rule-firing log | Workflow graph + live instance state |

Each of these is buildable today by an app that's willing to invent conventions. But if multiple consumers build the same conventions, the framework should provide them.

## Central question

**Should semstreams grow workflow primitives as a first-class framework layer that rides on the rules engine, or should it limit itself to rules + components + a narrow substrate primitive (BoundedDispatcher) and require consumers to express workflow semantics ad-hoc?**

This question is open. The exercise's job is to answer it from evidence (pattern sketches across consumer classes), not from dogma.

## Reading order for the design session

Read in order before starting the pattern sketches:

1. This document (you're here)
2. `project_rules_engine_design_review` memory — the original primitive-mapping framing this re-scopes
3. `feedback_reactive_patches_vs_engine_completion` memory — the discipline that surfaced the question
4. `CLAUDE.md` — re-read with fresh eyes; the "no separate workflow engine" stance is what's being re-examined
5. `docs/concepts/14-orchestration-layers.md` — the existing pattern catalog (rules + components, no workflow layer)
6. `docs/concepts/25-phased-agentic-chains.md` — the recent pattern doc for sequential agentic chains
7. `docs/adr/028-orchestration-architecture.md` — the three-layer architecture currently in place
8. `docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md` — the ADR that surfaced the gaps; Phase 2 is gated on this exercise
9. The five recent PR descriptions (#136, #137, #138, #148, #150) — what we actually shipped framed as fan-out patches
10. `project_semteams_gatherer_sandbox_case_study` memory — production pattern reference
11. `semspec/processor/scenario-orchestrator/component.go` — the surviving 600 LOC; this is the prior-art for workflow-shaped patterns we're trying not to repeat ad-hoc

## Pattern sketches (the exercise)

Sketch each of these patterns three ways:

- **(a)** Rules + components only
- **(b)** Rules + components + `BoundedDispatcher` substrate primitive
- **(c)** Workflow primitives layered on top of rules + components

Evaluate each sketch for: clarity, completeness, operator audit, instance tracking, restart recovery, framework cohesion, and whether the sketch is honest framework code or hides app-side state plumbing.

### Pattern 1 — Agentic: ADR-045 graph-research chain (sequential phased)

Sequential phased agent chain. Five components + seven rules. Current Phase 1 target. The reference instance of the phased-agentic-chain pattern.

Expected outcome: cleanest in (a). Used to validate that workflow primitives don't make the simplest case harder.

### Pattern 2 — Agentic: semteams research-pack with dynamic-N investigators

Decompose topic into N subtopics; one investigator (LLM judgment + web search + bash) per subtopic; aggregate findings; synthesize. N varies per topic.

Per Path X, this is a code component (dynamic-N → component-internal fan-out via errgroup). Sketch must show: per-investigator audit story, per-investigator governance, per-investigator model-tier choice, partial-success handling. Does it cleanly use BoundedDispatcher? Does it want workflow-primitive instance tracking?

### Pattern 3 — Robotic: drone survey mission

Long-running (hours), waypoint navigation, weather/battery branch logic, emergency abort with safe-landing compensation, must resume after reboot, operator dashboard required, mission versioning across deployments.

Almost zero LLM dependency. Pure workflow semantics. The clearest test case for whether workflow primitives are needed — if (a) and (b) feel like reinvention here, (c) is the answer.

### Pattern 4 — Robotic: manufacturing batch run

Multi-station parallel work, per-widget tracking through stations, inspection failure triggers rework subflow, days-long lifecycle, versioned process definitions, real-time station status visibility.

Hybrid workflow + bounded-concurrency. Tests whether BoundedDispatcher + rules suffice for the parallel-station case, and whether workflow primitives add value for the per-widget instance lifecycle.

### Pattern 5 — Event-driven: semconnect API request lifecycle

Per-request state, validation → processing → response, error compensation, no LLM, no long-running, but operator observability matters. Short-lived (seconds to minutes per request).

Tests whether workflow primitives over-engineer the short-lived case. If (a) or (b) feel cleanest here, workflow primitives should be opt-in not default.

### Pattern 6 — Hybrid: semspec scenario-orchestrator (current production)

Bounded-concurrency dispatch over KV-tracked work items. The pattern the surviving 600 LOC in semspec implements. The benchmark we're trying not to repeat ad-hoc.

Sketch should show: does (b) BoundedDispatcher cleanly replace the existing 600 LOC? If yes, semspec can refactor to use the framework primitive, validating BoundedDispatcher's shape. Does (c) make the refactor even cleaner?

## Decision framework

For each pattern, classify the gap:

| Tier | Gap shape | Action |
|---|---|---|
| 0 | Expressible with existing rule primitives | None |
| 1 | Needs 1-2 small rule primitives | Add primitive, keep rules-as-orchestrator framing |
| 2 | Needs narrow substrate component (concurrency, dispatch helper) | Ship as substrate (e.g. BoundedDispatcher) |
| 3 | Needs higher-order workflow abstraction (named instance, lifecycle, state tracking, versioning, introspection) that rules + components can't naturally express | Workflow primitive — **the open question this exercise answers** |

If most patterns land in tiers 0-2, the existing framing + BoundedDispatcher is sufficient. If multiple patterns land in tier 3 — and especially if the same workflow primitives keep getting demanded across agentic / robotic / event-driven consumers — workflow primitives ship.

## Known substrate primitive (Tier 2 candidate)

**`BoundedDispatcher`** — a small framework-provided primitive for bounded-concurrency parallel work. Spec sketch:

```go
// pkg/dispatch/bounded.go (~100-200 LOC)
type BoundedDispatcher struct {
    MaxConcurrent int
    WorkSource    func(ctx context.Context) ([]Work, error)
    Dispatch      func(ctx context.Context, work Work) error
    CompletionKV  string  // KV pattern to watch for completion signals
    OnComplete    func(ctx context.Context, work Work) error
}

func (d *BoundedDispatcher) Run(ctx context.Context) error {
    // semaphore-bounded worker pool
    // each dispatch is async; OnComplete fires on KV-watch hit
    // when slot frees, pull next from WorkSource
}
```

Properties:

- NOT a workflow engine (no DAG, no state-machine semantics, no branching)
- NOT a rule-engine extension (rules don't gain new fan-out primitives)
- IS a Go-side substrate component that components compose into their fanning logic
- IS KV-twofer-aware (integrates cleanly with completion-watch patterns)

This is a clear win regardless of the workflow-primitives outcome. Both candidate paths (with or without workflow primitives on top) include this primitive. The exercise should confirm its shape against patterns 2, 4, 6.

## Possible outcomes

### Outcome A — Rules + BoundedDispatcher is sufficient

Most patterns express cleanly in (b). The five-tag pile + BoundedDispatcher closes the gap. CLAUDE.md keeps the "no separate workflow engine" stance but explicitly:

> *Rules engine + KV twofer + BoundedDispatcher provide workflow-shaped capabilities. No parallel workflow runtime is needed. Higher-order abstractions (lifecycle, versioning, introspection) are app-side conventions until proven otherwise.*

Action: ship BoundedDispatcher in one tag, close the design review, retire the workflow-primitives question as YAGNI for now.

### Outcome B — Workflow primitives ship as deliberate framework layer

Multiple patterns surface tier-3 needs that don't reduce. Framework grows a thin workflow layer:

- Workflow definition as named, versioned artifact (group of rules + components + state conventions)
- Workflow instance tracking (framework-managed KV bucket with lifecycle states)
- Workflow primitives: `start_workflow`, `workflow_state`, `workflow_complete`, `workflow_fail`
- Workflow visualization (auto-derived from rule pack graph)
- Per-workflow metrics + operator dashboards

All built on rules + components, no parallel dispatch. CLAUDE.md reframes:

> *semstreams is a stateful workflow runtime built on knowledge graphs + NATS JetStream + a rules engine, serving agentic, robotic, and event-driven coordination. The rules engine handles orchestration (sequence, branch, dispatch); components handle execution; the workflow layer provides instance lifecycle, state tracking, versioning, and introspection.*

This becomes ADR-047. Probably 800-1500 LOC of framework code + significant doc work. Tag-bundle-sized.

### Outcome C — Hybrid (recommended starting bias, but not foregone)

BoundedDispatcher ships now (clear win, narrow scope, ~200 LOC, one tag). Workflow primitives gated on a second design exercise once two consumer classes hit the same tier-3 patterns in production. Avoids overshooting; accepts that the question may take a second iteration to fully resolve.

This is the lowest-regret path if the pattern sketches don't decisively favor A or B.

## What's affected by this exercise

| Item | Impact |
|---|---|
| ADR-045 Phase 1 (PRs 3-6) | Unaffected — sequential agentic chain, all tier 0. Can proceed in parallel. |
| ADR-046 Phase 2 / `fan_out_gated` | Deferred until exercise resolves; framing changes per outcome |
| GH #151 (sibling-loop enumeration) | Deferred; may be tier 0 (composable from existing primitives) or tier 1 (`.triples` ships) per outcome |
| Five-tag pile (beta.80–beta.84) | Retroactively reframed from "Phase 1 patches" to "rule-engine primitives that compose into workflow patterns" — same code, honest framing |
| CLAUDE.md "no separate workflow engine" stance | Revised per outcome (either narrowed explicitly or replaced) |
| `docs/concepts/25-phased-agentic-chains.md` | Adds forward-pointer to this exercise; potentially recontextualized as one workflow pattern within a broader workflow-primitives picture (outcome B) |
| Sandbox-tier issues (#141–#146) | Unaffected; bash/sandbox/observability work proceeds independently |
| semspec scenario-orchestrator | Retroactively reframed as the prior art that motivates either BoundedDispatcher (outcome A) or workflow primitives (outcome B); refactor candidate in either case |

## Suggested working approach

The exercise wants concentrated independent work, not spreading across multiple sessions. Recommended:

- **One session, 2-3 hours.** Read the materials in the reading order, then sketch each of the 6 patterns against (a)/(b)/(c). Don't context-switch in or out of this; the framing decision compounds and parallel-sessioning risks the framing drifting.
- **Output: a decision document.** Pattern sketches + tier classifications + recommendation for outcome A/B/C + draft ADR-047 if outcome is B.
- **Resist preemptive coding.** No primitive ships until the framing decision lands. The temptation to "just ship .triples since it's small" is exactly the per-tag reactive pattern that got us here.

## What NOT to do in the next session

- Don't open code first. Open the pattern sketches.
- Don't accept #151's filed proposal at face value. The right answer might be `.triples` (Tier 1), might be "composable from existing primitives" (Tier 0), might be "moot once workflow primitives ship" (Tier 3).
- Don't ship more "patches." Either the next code tag is named what it IS (BoundedDispatcher, or workflow primitives), or there is no next tag until this exercise resolves.
- Don't pre-commit to outcome A or B. Run the sketches, let the evidence drive the call.

## Hand-off summary (TL;DR for the next session)

**Status**: paused after beta.84. ADR-046 Phase 2 and #151 are explicitly gated on this exercise. ADR-045 Phase 1 PRs 3-6 are unaffected and can proceed in parallel. SemTeams paused waiting on the framework decision.

**Blocking work**: this design exercise — not code. 2-3 hour concentrated session.

**Next step**: read the reading order, run the 6 pattern sketches against (a)/(b)/(c), classify gaps, recommend outcome A/B/C, draft ADR-047 if outcome is B.

**Output the session should produce**:

1. The 6 pattern sketches with tier classifications
2. A decision document recommending outcome A, B, or C with reasoning
3. If outcome B or C: spec for BoundedDispatcher (~200 LOC, one tag)
4. If outcome B: draft ADR-047 for workflow primitives layer
5. Updates to:
   - `project_rules_engine_design_review` memory (close out the original framing, point at the outcome)
   - `project_workflow_primitives_decision` memory (NEW — capture the outcome + reasoning)
   - CLAUDE.md (revised stance per outcome)
   - `docs/concepts/25-phased-agentic-chains.md` (recontextualize per outcome)
   - GH #151 (close, modify, or proceed per outcome)
6. Sister-project guidance for semteams: how to design research-pack against the resolved framing

## References

- `project_rules_engine_design_review` — original primitive-mapping framing this re-scopes
- `feedback_reactive_patches_vs_engine_completion` — the discipline that surfaced the question
- `feedback_integration_tests_must_drive_production_wire` — the testing discipline that caught the third-iteration wire bug
- `feedback_reference_configs_verify_triple_stamping` — the documentation-vs-reality discipline
- `project_semteams_gatherer_sandbox_case_study` — production pattern reference
- `docs/concepts/14-orchestration-layers.md` — current pattern catalog (no workflow layer)
- `docs/concepts/25-phased-agentic-chains.md` — sequential agentic chain pattern (today's work)
- `docs/adr/028-orchestration-architecture.md` — three-layer architecture currently in place
- `docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md` — Phase 2 gated on this exercise
- CLAUDE.md — "no separate workflow engine" stance under examination
- semspec `processor/scenario-orchestrator/component.go` — surviving 600 LOC, prior art
- semspec retired `workflow/reactive/` — the cautionary tale (the failure mode we won't repeat)
