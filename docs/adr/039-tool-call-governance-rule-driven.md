# ADR-039: Tool-Call Governance is Rule-Driven

## Status

**Audit mechanism amended by [ADR-055](055-graph-write-intent-taxonomy.md) §3a**
— 2026-06-11. The `approve`/`deny` "auditability via the graph" choice (audit
triples written to the rule-ID subject) relied on `triple.add` auto-vivify creating
a phantom non-6-part rule-ID entity. ADR-055 retires auto-vivify entity creation, so
the verdict audit moves to a registered append-only verdict event on the
`GOVERNANCE_VERDICT_AUDIT` stream. The explicit-verdict-audit goal and the
"verdict is structural; audit-write failure must not flip it" discipline are
preserved; only the storage mechanism changes. The rest of this ADR stands.

**Proposed — 2026-05-12.** Migrates tool-call governance from the
in-process filter pattern shipped in beta.67+68 onto the rules engine.
The forcing function: semspec hit unmarshal noise from the
parallel-observer subject path while wiring tool-call governance
through the in-process accessor pattern, exposing two structural
problems — (1) the governance component was tightly coupled to the
agentic-loop via a shared `*ToolCallFilter` pointer; (2) the rules
engine had matured into the natural home for declarative policy but
governance had been built as a parallel filter system before that
maturation. semspec is the only external consumer and is willing to
migrate, so this ADR commits to retiring the duplicated path rather
than entrenching it.

An earlier draft of this ADR proposed a "filter chain on subjects"
migration. Code investigation showed the rules engine already has the
primitives needed (subject subscription, message-field matching,
`publish`/`deny` actions, JetStream-routed Publisher), and only two
small extensions are required. The decision below reflects that
investigation.

The framing this ADR commits to: **the rules engine is an evaluation
engine over message inputs, not a policy engine.** Today it serves
orchestration (ADR-028) and time-driven scheduling (cron rules,
beta.27). This ADR adds policy enforcement as a third workload on
the same evaluator, sharing one DSL. The conflation risk lives in
action-set bloat (see "Architectural debt acknowledged" section),
not in the DSL itself.

Last shipped tag is `v1.0.0-beta.68` (PR #59 — tool-call governance
wiring accessors, the retirement target).

## Context

### How tool-call governance ended up parallel to the rules engine

The agentic-governance component was added before the rules engine
matured to its current state. At that time, the natural home for
content/policy enforcement was a chain of filters with hard-coded
types (PII, injection, content moderation, rate limiting). When
semspec's worktree-leak use case arrived (block bash commands that
write outside the agent's assigned worktree), the asks doc framed
the integration around extending that filter chain — a
`ToolCallFilter` type satisfying the loop's
`agentic.ToolCallFilter` interface, installed via
`MessageHandler.SetToolCallFilter`.

That shipped in beta.67 (PR #58) and the consumer-side wiring
accessors shipped in beta.68 (PR #59). It works mechanically, but
the implementation choice was driven by the existing in-handler
filter slot in `MessageHandler`, not by the framework's current
communication patterns. Today's framework communicates
inter-component via JetStream subjects and KV watches per CLAUDE.md's
"facts vs requests" model. Beta.67+68's pattern is the only place in
the framework where two processor components share a Go pointer at
runtime.

### What the rules engine actually does

A code audit (2026-05-12) of `processor/rule/` shows the rules
engine is already a message-driven, subject-subscribing,
payload-inspecting engine:

| Primitive | Where | What it does |
|---|---|---|
| `Subscribe() []string` | `interfaces.go:33` | Rule declares NATS subjects it listens on |
| `Evaluate(messages []message.Message) bool` | `interfaces.go:36` | Rule receives full message payloads |
| Field-path conditions | `expression_factory.go:175` | `{"field": "command", "operator": "contains", "value": "..."}` matches dotted paths into payload data |
| Operators | `expression_factory.go:222-239` | `eq`, `ne`, `lt`, `lte`, `gt`, `gte`, `contains`, `starts_with`, `ends_with` |
| `publish` action | `actions.go:474-528` | Emits to NATS subject; `Publisher` interface routes core NATS or JetStream by port config |
| `deny` action | `actions.go:1003-1041` | Returns `*DenyVerdict` error + writes audit triple. Short-circuits remaining actions. Beta.32. |
| Variable substitution | `execution_context.go:77-180` | `$entity.*`, `$related.*`, `$state.*`, `$schedule.*`, `$caller.*` namespaces |

The earlier draft of this ADR misclassified the engine as
"reactive to graph state only." Caller correction during ADR review
flagged the mistake and prompted the code audit summarized above.

### The two small gaps

For tool-call governance via rules, two extensions are required:

1. **`$message.*` substitution namespace.** The existing namespaces
   read from entity context; there is no path to template a subject
   with values from the inbound message. We need this for
   per-loop/per-call verdict subjects (e.g.
   `agent.toolcall.rejected.$message.loop_id.$message.call_id`).
2. **`approve` action.** Symmetric to `deny` — publishes to a
   configurable approved subject and writes an audit triple. Closes
   the asymmetry where `deny` produces an in-process verdict but
   approval has no first-class action type. See "Why explicit
   approve" below.

Both are small, well-shaped engine extensions. Neither requires
changing the engine's core model.

### Why explicit `approve`, not optimistic "absence of deny"

An earlier design sketch had the loop dispatch optimistically: publish
proposed, wait a short window for `agent.toolcall.rejected.*`,
dispatch if none arrives. This is simpler but loses on three
dimensions:

- **Audit completeness.** The existing `deny` action is structurally
  designed to write an audit triple even when downstream propagation
  fails ("verdict is structural; audit-write failure must not flip
  it to allow"). Optimistic-dispatch has no corresponding positive
  audit — operators can't ask "show me every tool call we explicitly
  approved" against the triple store, only "show me every tool call
  we explicitly denied."
- **Ambiguity under failure.** Loop timeout reached without verdict
  → was it rules-engine slow? rules-engine down? subject mis-wired?
  network blip? Optimistic semantics conflate "no rule said no" with
  "no rule responded." Explicit verdict eliminates the ambiguity.
- **Symmetry with deny.** The deny action signals an explicit
  decision. Approve as its mirror is the framework-honest pattern.
  Operators reading the rule DSL see two action types with parallel
  semantics, not one with a special-case "absence means yes."

Cost: one extra action type in the engine surface (~80 LoC).
Affordable.

### What changes for operators

semspec's current beta.68 wiring:

```go
gov := governanceComponent.(*agenticgovernance.Component)
loop := loopComponent.(*agenticloop.Component)
if f := gov.ToolCallFilter(); f != nil {
    loop.SetToolCallFilter(f)
}
```

with JSON config:

```json
{
  "filter_chain": {
    "filters": [
      {
        "name": "tool_call_governance",
        "tool_call_config": {
          "blocked_command_patterns": ["cd /workspace "]
        }
      }
    ]
  }
}
```

becomes, post-ADR-039:

```go
// No coupling. Each component is constructed from its own config.
// The loop's tool_call_governance.mode flag turns on the subject path.
```

with JSON rule definitions instead of filter config:

```json
{
  "name": "block-bash-workspace-leak",
  "subscribe": ["agent.toolcall.proposed.>"],
  "conditions": [
    {"field": "tool_name", "operator": "eq", "value": "bash"},
    {"field": "command", "operator": "contains", "value": "cd /workspace"}
  ],
  "logic": "all",
  "actions": [
    {
      "type": "publish",
      "subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",
      "properties": {
        "call_id": "$message.call_id",
        "reason": "writes outside worktree blocked"
      }
    },
    {"type": "deny", "reason": "writes outside worktree blocked"}
  ]
}
```

Operators write policy in the same DSL they use for orchestration.
Custom governance — different rules per role, time-of-day deny
windows, rate limits composed with command patterns, role-based
allowlists — becomes a rule-writing exercise, not a code change.

## Decision

**Tool-call governance is rule-driven, subject-based.** Concrete
commitments:

1. **agentic-loop publishes proposed tool calls to
   `agent.toolcall.proposed.<loop_id>.<call_id>`** before dispatch.
   Per-call fan-out from the batch; each call evaluates independently
   in rules. Per-call payload carries `{loop_id, parent_loop_id,
   call_id, tool_name, command, url, ...}` flattened so rule
   conditions match cleanly on field paths. **`parent_loop_id` is
   nullable but included from day one** so rules can match across
   loop hierarchies (sub-agent governance inheritance).
2. **agentic-loop subscribes to
   `agent.toolcall.approved.<loop_id>.<call_id>` and
   `agent.toolcall.rejected.<loop_id>.<call_id>`** with a bounded
   wait window. **Subscription is bound BEFORE the corresponding
   `proposed.*` publish** to close the proposed-faster-than-subscribe
   race (see implementation section). Approved → dispatch. Rejected
   → fail the call with the verdict reason. Timeout (no verdict) →
   fail the call with a "governance verdict timeout" reason.
3. **Rules engine gains `$message.*` substitution namespace.** Mirrors
   `$entity.*` but reads from the inbound message data, including
   deep paths (`$message.tool_args.command`) per the `$entity.triple.X`
   precedent. Required for templating verdict subjects with
   loop_id / call_id. Unresolved tokens trip the existing silent-pass
   warning at `execution_context.go:27`.
4. **Rules engine gains `approve` action.** Symmetric to `deny` —
   publishes to a configurable approved subject and writes a
   positive-verdict audit triple. Both actions follow the
   "verdict is structural; audit-write failure must not flip the
   verdict" discipline. `approve` does NOT short-circuit subsequent
   actions (asymmetric to `deny`, which is terminal); operators may
   want to fire observability/audit actions alongside approval.
5. **agentic-loop gains config:**
   ```json
   "tool_call_governance": {
     "mode": "disabled" | "enforce" | "audit",
     "timeout": "1s"
   }
   ```
   - **`disabled`** (default) — current behavior, no publish.
     Preserves zero-impact for deployments without governance.
   - **`enforce`** — publish, wait for verdict within `timeout`,
     fail-closed on timeout. Production posture.
   - **`audit`** — publish, dispatch immediately without waiting,
     log verdicts asynchronously for observability. Shadow mode
     for rule development against real traffic before enforcement.

   `timeout` ships at a deliberately generous **1s default** in
   beta.69 paired with a new Prometheus histogram
   `tool_call_governance_verdict_duration_seconds`. Default
   tightens in beta.70 based on observed p99 from semspec's
   deployment. **Shipping a measured default rather than a guess.**
6. **`agentic-governance.ToolCallFilter` retires** as a filter type.
   The `*ToolCallFilter` struct, `NewToolCallFilter`,
   `NewToolCallFilterWithConfig`, `ToolCallFilterConfig`,
   `createFilter` "tool_call_governance" case, and the
   `EnableToolGovernance` flag all remove.
7. **`Component.ToolCallFilter()` accessor and
   `Component.SetToolCallFilter()` setter retire** from
   agentic-governance and agentic-loop respectively.
   `MessageHandler.SetToolCallFilter`,
   `MessageHandler.toolCallFilter` field, and the dispatch-time
   filter invocation at `handlers.go:919-943` all remove.
8. **Operators migrate** from the
   `filter_chain.filters[tool_call_governance].tool_call_config`
   JSON shape to rule definitions. Semspec is the only consumer; the
   migration is a coordinated rewrite of their rule config file.

### Out of scope (separate ADRs / work items)

- **Content-inspection governance** (PII, injection, content
  moderation): these are scanners, not policy matchers. They produce
  scores/classifications that rules can then consume. Migration plan
  for those is a separate ADR; not blocking this one. The
  agentic-governance component retains them.
- **Path A's parallel-observer problem** (downstream consumers read
  raw `agent.task.*` instead of `.validated.*`): orthogonal to
  tool-call governance. Separate decision.
- **Batch evaluation in the rules engine** (`Evaluate(messages []
  message.Message)` already supports it; `pkg/buffer/` infrastructure
  exists; processor evaluates one message at a time today per
  `message_handler.go:113`): planned as a parallel improvement.
  Useful for aggregation rules, sliding windows, "if any call in
  this batch is X" semantics. Not required for tool-call governance
  because per-call fan-out works. Tracked as a separate enhancement
  PR.

## Options Considered

### Option A: Filter-based subject migration (original ADR draft)

Move the existing `*ToolCallFilter` onto subject middleware. Keep
the filter chain pattern but route through JetStream. Governance
component subscribes to `agent.toolcall.proposed.*`, runs the
filter chain, publishes verdicts.

**Rejected.** Doubles down on the parallel filter system when the
rules engine already provides the same capability declaratively.
Operators end up with two policy systems (rules for orchestration,
filters for governance) that overlap heavily. Filter chain's
hard-coded type system limits expressiveness vs rules engine's
condition language. The "different governance rules per operator"
requirement is exactly what the rules engine was built for.

### Option B: Rules-engine-based (this ADR)

Tool-call governance is rule-driven. Two small engine extensions
(`$message.*` substitution, `approve` action). Filter chain's
tool-call type retires entirely.

**Chosen.** Single uniform mechanism for orchestration AND policy.
Operators write declarative JSON, hot-reloadable. Compose tool-call
policy with state predicates (role gating, rate limits, agent state)
trivially. Engine extensions are small and have value beyond
governance.

### Option C: All-in-process governance

Move all governance hooks (including content inspection) to
in-process filters installed on every agentic component via setters.

**Rejected.** Contradicts CLAUDE.md's facts-vs-requests model.
Forces every agentic component to define hook points for every
filter type. Loses auditability of JetStream history. Couples every
component to the governance package's filter interfaces. Was
implicitly the direction beta.67+68 pointed; this ADR reverses
that direction.

### Option D: Keep current in-process (status quo)

Leave `Component.ToolCallFilter()` and
`Component.SetToolCallFilter()` in place. Document the standalone
constructor as the recommended alternative. Don't unify with rules
engine.

**Rejected.** Coupling persists. Operators face two policy systems
indefinitely. semspec's downstream confusion would be patched, not
resolved. Misses the opportunity to set the pattern correctly while
the surface is small and one-consumer.

## Consequences

### Positive

- **Single uniform policy system.** Rules engine handles
  orchestration AND tool-call governance. Operators learn one DSL.
- **Declarative policy.** Operators write JSON rules. Different
  policy per role, per time-of-day, per agent state, per anything
  the rules engine can match. Hot-reloadable.
- **Zero Go coupling.** agentic-governance and agentic-loop
  communicate only via JetStream subjects. No shared pointers, no
  pre-Start ordering invariant.
- **Auditability for free.** Both `approve` and `deny` write audit
  triples. Operators query the graph for verdict history.
- **Composability with other rule primitives.** Tool-call policy
  can use `FireEveryNEvents` (per-rule global rate limit),
  `Cooldown` (per-rule time-window), entity-state predicates
  (role/state-based gating), cross-rule references via entity
  triples. **Per-entity / per-tenant rate limiting is not present
  today** — `FireEveryNEvents` fires per-rule globally, not scoped
  per-agent — and is a separate enhancement noted in "Architectural
  debt acknowledged" below.
- **agentic-governance component shrinks.** Filter chain keeps PII /
  injection / content moderation / rate limiting (those are scanners
  that produce findings; rules consume them). Tool-call governance
  surface goes away entirely.

### Negative

- **JetStream round-trip latency per tool call.** ~1-5ms in
  in-process JetStream deployments. Tool calls already cross
  JetStream to agentic-tools, so this is one extra hop on an
  already-async path. Bounded by the loop's configurable timeout.
- **Implementation work.** Engine extensions (`$message.*`,
  `approve` action) + loop changes (mode flag, publish/subscribe,
  fan-out, race-fix) + retirement of beta.67+68 surface across two
  tags (beta.69 BREAKING with single in-tree consumer; beta.70
  retirement after soak).
- **Breaking change for semspec.** Acceptable per user direction;
  semspec is the only consumer and is willing to migrate. The migration
  is mechanical (config-file rewrite) with no in-flight state to
  preserve.
- **Wedge mode if rules engine isn't deployed.** Loop publishes to
  `proposed.*`, waits for verdict that never comes. Mitigated by
  explicit `mode: "disabled"` default and bounded timeout with
  fail-loud behavior. Not magic auto-detection.

### Neutral

- **agentic-governance keeps existing filter types** (PII,
  injection, content moderation, rate limiting). Those are scanners
  whose output rules can consume. Their subject topology is a
  separate ADR / migration; this ADR doesn't touch them.

## Implementation

### Phase 1 — This ADR

Written commitment. No code changes. Establishes the rule that
subsequent phases honor.

### Phase 2 — Single-tag delivery (engine extensions + loop subject mode + docs)

**One tag (beta.69), one BREAKING change, one in-tree consumer for
every new surface.** Engine extensions ship together with the loop
changes that consume them — no dead-code-in-main hazard per the
"PR scope is complete system" discipline. semspec rewires their
config at this tag; e2e:agentic must run green with a rule-driven
tool-call governance path before tag per the CLAUDE.md
breaking-change e2e rule.

**Engine extension A: `$message.*` substitution namespace.**
- New helper `messageSubstitute(template, msg)` in a new file
  `processor/rule/message_substitution.go`
- Mirrors `entity_substitution.go` structure
- Supported tokens: `$message.<field_path>` for any dotted path into
  the inbound message data (e.g. `$message.loop_id`,
  `$message.call_id`, `$message.tool_args.command`). Deep-path
  access per the `$entity.triple.X` precedent.
- Wire into `ExecutionContext.SubstituteVariables` flow
- Update regex at `execution_context.go:27` to include `message`
- Tests mirroring `entity_substitution_test.go` shape, **including
  a test case for an unresolved `$message.<missing_field>` token
  that asserts the silent-pass warning fires and the literal stays
  in the output**. Catches a class of bugs where new event shapes
  forget to populate a field.

**Engine extension B: `approve` action.**
- New action type constant `ActionTypeApprove = "approve"` in
  `actions.go`
- `executeApprove` mirror of `executeDeny`: writes audit triple via
  `tripleMutator` (with new `PredicateRuleApprove` constant), then
  publishes to the configured approved subject via
  `publisher.Publish`. Returns nil on success.
- Reuses existing `Subject` field on the action struct.
- **Asymmetric short-circuit**: `deny` short-circuits remaining
  actions via `*DenyVerdict`; `approve` does NOT short-circuit
  (later actions can fire). This asymmetry IS the feature —
  approval doesn't preclude observability/audit actions on the
  same rule firing.
- Tests: config → execute → audit-triple + publish round-trip.

**Loop changes:**
- New config schema:
  ```json
  "tool_call_governance": {
    "mode": "disabled" | "enforce" | "audit",
    "timeout": "1s"
  }
  ```
  Default `disabled`. 1s timeout is **deliberately generous** for
  beta.69; tightens in beta.70 after p99 measurement (see
  Observability below).
- New output port `agent.toolcall.proposed.*` (JetStream).
- New input ports `agent.toolcall.approved.*` and
  `agent.toolcall.rejected.*` (JetStream, scoped per-call).
- Per-call publish payload shape (flattened so rule conditions
  match cleanly):
  ```json
  {
    "loop_id": "...",
    "parent_loop_id": "...",
    "call_id": "...",
    "tool_name": "bash",
    "command": "...",
    "url": "...",
    "arguments": { ... }
  }
  ```
  **`parent_loop_id` ships from day one (nullable)** so rules can
  match across loop hierarchies for sub-agent governance
  inheritance. Cheap to include now; expensive to retrofit.
- In `handleToolCallResponse` (handlers.go:919-943), replace the
  in-process filter invocation with the mode-driven dispatcher:

  | mode | behavior |
  |---|---|
  | `disabled` | Current behavior. No publish. Direct dispatch. |
  | `enforce` | Subscribe-then-publish (see race fix below), wait `timeout`, dispatch on approve / fail on reject / fail-closed on timeout. |
  | `audit` | Publish proposed, dispatch immediately without waiting, log verdicts to a counter when they arrive. Shadow mode for rule development. |

**Race-condition fix: subscribe before publish.** JetStream
consumer binding is async with publish; if a rule processor fires
faster than the loop's verdict subscriber binds, the verdict can be
published to a subject with no subscribers, the loop times out, the
call fails. Same class as the natsclient handler-error payload
convention bug (`feedback_natsclient_error_payload_convention`) —
in-process tests pass, production races. Fix: the loop binds
verdict-subject subscriptions BEFORE the proposed publish completes.
Implementation candidates:

1. Pre-create the JetStream consumer with `DeliverAll`, wait for
   the consumer to be active, then publish proposed.
2. Use core NATS with `Sub.Sync()` precondition before publishing.
3. Use a per-loop pre-bound `>`-wildcard subscription that lives
   for the loop's lifetime; verdicts demux by `<call_id>` path
   segment.

(3) is the cleanest — one bind per loop, all per-call verdicts
demuxed off the wildcard. Implementation phase will pick one;
documenting the race here so the choice is deliberate, not
accidental.

**Observability:**
- New histogram `tool_call_governance_verdict_duration_seconds`
  with labels `{loop_id_hash, decision}`. Drives the timeout-tuning
  decision in beta.70.
- New counter `tool_call_governance_verdict_total` with labels
  `{decision, mode}` where `decision ∈ {approved, rejected,
  timeout}`.
- New counter `tool_call_governance_subscribe_before_publish_failures_total`
  to catch the race-fix regressing.

**Retirement of beta.67+68 surface — deferred to beta.70 (Phase 3).**
Retirement at beta.69 alongside the breaking change is too
aggressive; semspec needs an escape hatch (their existing
in-process wiring) until they confirm subject-mode works end-to-end.

**Documentation in the same PR:**
- `docs/operations/16-tool-call-governance.md` (new): subject
  topology, rule examples (block-bash-pattern, rate-limit-tool-calls,
  role-based-tool-allowlist), troubleshooting verdict-timeout symptoms.
- `docs/concepts/03-streams-vs-kv-watches.md` updated with the new
  tool-call governance subjects.
- semspec response doc amended with the rule-DSL migration snippet.

### Phase 3 — Retire beta.67+68 surface

**One tag (beta.70), no behavior change for operators who already
migrated.** Soak period between beta.69 and beta.70 is for semspec
to confirm subject-mode works end-to-end on their stack before the
escape hatch goes away.

- Remove `agentic-governance.ToolCallFilter` type, `NewToolCallFilter`,
  `NewToolCallFilterWithConfig`, `ToolCallFilterConfig`, the
  `createFilter` "tool_call_governance" branch, and the
  `EnableToolGovernance` config flag.
- Remove `agentic-governance.Component.ToolCallFilter()` accessor.
- Remove `agentic-loop.Component.SetToolCallFilter()` setter,
  `MessageHandler.SetToolCallFilter`,
  `MessageHandler.toolCallFilter` field, and the dispatch-time
  filter invocation at `handlers.go:919-943`.
- Update `schemas/agentic-governance.v1.json` (autogenerated).
- Update CLAUDE.md project context to remove the in-process filter
  reference.
- **Tighten default `tool_call_governance.timeout`** based on p99
  observed from semspec's beta.69 deployment. Honest measurement
  closes the loop on the "ship with a guess" anti-pattern.

### Migration timeline (proposed)

| Phase | Deliverable | Earliest tag |
|---|---|---|
| 1 | This ADR merged | next session |
| 2 | Engine extensions + loop subject mode + docs | beta.69 (BREAKING — semspec rewires; e2e:agentic green required) |
| 3 | beta.67+68 surface removal + timeout tuning | beta.70 (after semspec confirms migration) |

Two tags, one breaking change. Soak period between tags is
substantive (not bureaucratic) — semspec needs the escape hatch
during migration and the team needs measured timeout data before
defaulting to anything tighter than 1s.

## References

- `processor/rule/interfaces.go:28-49` — `Rule` interface, `Subscribe()`,
  `Evaluate()`, `EntityStateEvaluator`
- `processor/rule/expression_factory.go:75-243` — message-driven
  condition evaluation, supported operators
- `processor/rule/actions.go:51-150` — `Action` struct shape;
  publish + deny action plumbing
- `processor/rule/actions.go:474-528` — `executePublish` (target
  pattern for `executeApprove`)
- `processor/rule/actions.go:1003-1041` — `executeDeny` (symmetry
  target for `executeApprove`)
- `processor/rule/execution_context.go:27-180` — variable substitution
  namespaces (extension target for `$message.*`)
- `processor/rule/message_handler.go:113-114` — single-message
  evaluation TODO (`pkg/buffer/` future-work hook)
- `pkg/buffer/` — existing buffer infrastructure for future batch
  evaluation restoration
- `processor/agentic-governance/tool_filter.go` — `*ToolCallFilter`
  (phase 4 removal target)
- `processor/agentic-governance/component.go:601-624` — accessor
  (phase 4 removal target)
- `processor/agentic-loop/component.go:289-321` — setter shim
  (phase 4 removal target)
- `processor/agentic-loop/handlers.go:919-943` — current in-process
  filter invocation (phase 3 replacement target)
- semspec asks doc:
  `/Users/coby/Code/c360/semspec/.semspec/semstreams-governance-asks-2026-05-12.md`
- semspec response doc:
  `/Users/coby/Code/c360/semspec/.semspec/semstreams-governance-asks-2026-05-12-response.md`
  (amended after phase 3 ships)
- Project memory:
  `project_governance_enforcement_topology.md` (architectural picture
  recorded during this ADR's evolution)
- Discipline memory:
  `feedback_verify_main_go_wire_for_sister_asks.md` (the discipline
  rule that this ADR is the corrective for)
- CLAUDE.md "Architectural Identity" section (facts vs requests,
  KV twofer)
- PR #58 (beta.67) — filter mechanics; partial removal target
- PR #59 (beta.68) — wiring accessors; full removal target
- ADR-028 — orchestration architecture (rule skeleton + coordinator +
  ops agent); this ADR extends "rules trigger" to include tool-call
  governance
- ADR-032 — `$caller.*` substitution + `deny` action (the precedent
  this ADR builds on)

## Architectural debt acknowledged

This ADR ships with three architectural debts that should be visible
to future contributors rather than buried in implementation:

1. **`approve` as peer of `deny` signals a sub-typing refactor when
   a third policy-shaped action lands.** The Action struct already
   carries fields used by only a subset of action types (`Role`,
   `Model`, `Prompt` are publish_agent-specific; `Subject` is
   publish/approve-specific; etc.). Adding `approve` extends that
   pattern. **Two verdict types is acceptable; a third (e.g.
   `quarantine`, `escalate_to_human`) is the smell threshold where
   the action set wants a sub-typing pass** — either a typed
   `Verdict { Decision string }` union, or an interface-per-action
   refactor. ADR-032's `deny` was the first step on this gradient;
   this ADR's `approve` is the second. **Commit to revisiting at
   the third.**
2. **Per-entity / per-tenant rate limiting is not present.**
   `FireEveryNEvents` is per-rule globally. Tool-call governance
   patterns like "rate limit bash calls to N per minute per agent"
   are expressible only as global rules today. Filed as a
   follow-up enhancement; not blocking this ADR. The composability
   claim in "Positive consequences" is honest about this limit.
3. **Hot-reload semantics during a live tool-call evaluation are
   undefined.** Rule definitions are hot-reloadable (beta.9 sync
   fix). What happens when a rule that matched a proposed call gets
   replaced before its verdict publishes? Today's in-process model
   sidesteps this because evaluation is synchronous; subject-driven
   evaluation has a window where it can happen. **Document in
   operator docs (Phase 2 deliverable) and revisit if it ever
   surfaces as a real problem.**

## Open questions for review

These remained open after the architect review. The "must address"
items from review are now resolved in the Decision and Implementation
sections above; what remains here is design detail that benefits from
implementation-phase feedback rather than ADR-merge-time resolution.

1. **Approved subject shape.** Single subject
   `agent.toolcall.approved.<loop_id>.<call_id>` (fully-scoped) vs
   broader `agent.toolcall.approved.<loop_id>` carrying call_id in
   payload. Lean fully-scoped — simpler on the loop side, one
   subject = one verdict, no payload demuxing. Worth confirming
   during Phase 2 implementation given the subscribe-before-publish
   race-fix candidates (option 3 in that section uses a wildcard
   subscription that demuxes — that path argues for broader).
2. **Wedge behavior under mass-rejection rules.** What happens when
   a rule rejects every tool call (operator mis-config)? Loop fails
   every call, emits "all tools failed" trajectory, eventually hits
   MaxIterations. Acceptable; the rule engine is honoring the
   operator's stated policy. **Add to operator docs (Phase 2):
   troubleshooting section on "every call denied — check the rule
   set."**
3. **Resolved: 200ms default timeout.** Per architect review, this
   was a guess and ships replaced with a 1s deliberately-generous
   default + observability histogram. Default tightens in beta.70
   based on measured p99 from semspec's deployment.
4. **Resolved: ternary mode.** Per architect review, binary
   `subject` | `disabled` was too coarse for the dev/staging
   shadow-mode use case. Final shape: `disabled` (default) /
   `enforce` (production) / `audit` (shadow mode for rule
   development).
5. **Resolved: subscribe-before-publish race.** Per architect
   review, the race needed explicit acknowledgment and a chosen
   fix. Three candidates documented in Implementation section;
   Phase 2 implementation picks one deliberately.
6. **Resolved: `$message.*` deep field access.** Confirmed yes per
   `$entity.triple.X` precedent.
7. **Resolved: `approve` short-circuit semantics.** Confirmed
   non-short-circuit per architect review — the asymmetry with
   `deny` IS the feature, not a flaw.
