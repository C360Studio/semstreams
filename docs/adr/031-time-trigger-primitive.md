# ADR-031: Time-Trigger Primitive for Reactive Rules

## Status

**Proposed (2026-04-29).** No tag is committed. This ADR captures the
design space for a future framework primitive so the discussion has a
target before implementation pressure forces a quick choice.

## Context

The reactive rule engine is event-driven by design. Every rule today
fires in response to a NATS subject match or a KV state change — the
`watch` side of every rule references either a stream pattern or a KV
bucket prefix. There is no first-class way to say "fire this rule on
Mondays at 9am" or "every 15 minutes" or "the first Tuesday of each
month."

This came up concretely in semteams issue #15: the operating-model
schema captures `Cadence` (e.g. `"weekly"`) and `Trigger` (e.g.
`"Monday 9am"`) per `Entry`, the onboarding interview populates them,
and the system-prompt preamble renders them ("Weekly planning:
Mondays 9-10am"). But nothing in the framework actually *acts* on
those fields. The system knows the user has a weekly planning block on
Monday mornings but cannot proactively surface relevant context, draft
a planning prompt, or initiate any task at that time.

Two related needs:

1. **Cadence-driven actions**. Fire a rule (and downstream agent
   tasks) on a schedule encoded as a cron expression or natural
   language ("every weekday at 9am").
2. **Trigger-text parsing**. Translate freeform user-supplied trigger
   text into a schedule the framework can evaluate. `"Mondays 9-10am"`
   needs to become something cron-equivalent before it can drive a
   timer.

Both needs are real. Neither is small. The cleanest moment to design
them is now, before semteams or another product invents a one-off
scheduler that the framework has to absorb later.

## Constraints and goals

- **Stay framework-neutral.** The primitive must not bake in semteams'
  operating-model semantics or any persona/cadence vocabulary. Other
  products (semspec, downstream agentic platforms) will want different
  scheduling shapes.
- **Honor the orchestration boundaries from
  [ADR-028](028-orchestration-architecture.md).** Time-driven firing
  is still rule-shaped: condition met (clock advances past trigger) →
  action fires. The action should publish a reference (entity ID, task
  description) and let downstream coordinators do the actual work.
- **No new payload-registry types if avoidable.** Reuse existing
  agent.task.\* / publish-action plumbing.
- **Recovery semantics.** A scheduled fire that's missed during a
  framework restart should be observable. "Did the Monday 9am block
  fire while we were down?" needs a clean answer.
- **No global cron daemon.** Per existing patterns, scheduling must
  ride on JetStream / KV substrate, not in-process timers that vanish
  on restart.

## Options considered

### Option A — `"type": "cron"` rule type

Add a new rule type alongside `kv-watch` and `stream-watch`:

```json
{
  "id": "weekly-planning-prompt",
  "type": "cron",
  "schedule": "0 9 * * MON",
  "actions": [
    {"type": "publish_agent", "role": "planning-coach", "prompt": "..."}
  ]
}
```

The rule processor adds a cron evaluator alongside its existing
match-and-fire logic. On startup it reads all `cron`-typed rules from
config and registers them with a single shared scheduler goroutine
that fires the rule's actions when each schedule's `Next()` time
elapses.

**Pros**:

- Reuses every existing rule action (`publish`, `publish_agent`,
  `update_kv`, etc.). No new action-side code.
- Tight integration: a cron rule and an event rule live in the same
  config and use the same observability surface.
- Smaller blast radius than a new component.

**Cons**:

- The rule processor takes on a stateful concern (next-fire times)
  that today it doesn't have. Restart recovery means persisting last-
  fired timestamps somewhere — probably a new KV bucket
  `RULE_SCHEDULES`.
- Rules-engine config gets more shapes. The implementation has to
  decide if cron rules can also have `watch` conditions (probably no
  — keep them disjoint), and how missed fires during downtime are
  reported.
- A cron expression is the lowest common denominator. Trigger-text
  parsing ("first Tuesday of the month") still has to happen
  somewhere — either the user/product writes cron directly, or the
  framework adds an LLM-assisted parser at config-load time.

### Option B — Dedicated scheduler component

A new component with its own JetStream subscription on a `schedule.*`
namespace and a dedicated KV bucket `SCHEDULES`:

```json
{
  "name": "scheduler",
  "type": "scheduler",
  "config": {
    "bucket": "SCHEDULES",
    "tick_interval": "30s"
  }
}
```

Other components (rules, agentic-dispatch, an /onboard handler that
translates `Cadence`/`Trigger` into schedule entries) write to
`SCHEDULES` keyed by `{owner}.{schedule_id}`. The scheduler ticks,
fires due jobs by publishing on `schedule.fired.{schedule_id}`, and a
companion rule subscribes and dispatches the actual work.

**Pros**:

- Cleanest separation. Scheduling is a real concern with its own KV
  shape, retry semantics, and operator surface (list/cancel/reschedule
  endpoints).
- Multiple subsystems can produce schedules without coupling them to
  the rule processor's config language. semteams can register
  per-user planning slots at /onboard time without dropping a rule
  config file.
- Restart recovery is the bucket itself: schedules persist, the
  scheduler picks up where it left off on the next tick.
- Schedule-status query is a graph-of-schedules read, not a config
  introspection.

**Cons**:

- Bigger surface: new component, new KV bucket, new JetStream subject
  conventions. More code to write, test, and document.
- Two ways to express "fire this on a schedule" once cron rules also
  exist — operators choose between them, and the wrong choice locks
  in pain.
- More pieces to fail. A scheduler bug is a separate failure domain
  from the rule processor.

### Option C — Defer to product layer

Products use their own external cron (host crontab, k8s
CronJob, GCP Cloud Scheduler) to POST to existing HTTP entry points
(`/loops/{id}/signal`, `/message`, etc.).

**Pros**:

- Zero framework code. Tomorrow.
- Battle-tested infrastructure. Every Linux box has cron.

**Cons**:

- The framework's promise of "the system acts on your behalf" fails
  to deliver on cadence-driven actions without bolt-on infrastructure
  every deployment has to set up. semteams' weekly-planning vision is
  worse than every other rule-driven flow because it requires a
  parallel mental model.
- No first-class observability. Missed fires, reschedules, and
  per-user audit trails live in three different places (host cron,
  HTTP logs, framework state).
- Trigger-text parsing has to happen somewhere — and "have the user
  also write valid cron" is a degraded UX.

## Decision

**Defer commitment.** Adopt no implementation today. The decision the
ADR records is: when scheduling pressure forces the build, **prefer
Option A first** unless concrete operator-friction reasons emerge that
make the dedicated scheduler component worth the surface cost.

### Rationale for the lean toward Option A

- The rule processor already owns "condition → action" — adding a
  time-source-of-truth condition stays inside its job description.
- Recovery cost is bounded: one new KV bucket, last-fired timestamps
  per cron rule, missed-fire detection on startup.
- If we later decide a dedicated scheduler is right, we can extract
  the timestamp store and the tick loop into a new component without
  rewriting any rule action.
- Option B is a one-way door: once products write schedules through a
  scheduler-component API, retracting that API is breaking surface.

### Trigger-text parsing

Whichever option ships, **the framework should not absorb freeform
trigger-text parsing as a pure-Go component**. The semantics of
`"Mondays 9-10am"` vs `"end of sprint"` vs `"first Tuesday of the
month"` are too open-ended for deterministic parsing to be reliable.

Two paths, both deferred until needed:

1. **LLM-assisted at write time.** When `/onboard` (or any product
   ingest) captures the trigger text, an LLM call translates it to a
   cron expression, presents the cron back to the user for
   confirmation, and persists both. The framework stores cron, not
   freeform text; the freeform stays as `Trigger` for human display.
2. **Validate-only at schedule-write time.** The framework exposes a
   helper that validates cron expressions and rejects unparseable
   values; products own the freeform-to-cron pipeline.

Option 1 is closer to "the system understands what you said." Option
2 is closer to "the framework only deals in unambiguous schedules."
Pick when implementation lands.

## What this ADR does NOT decide

- Cron expression flavor (POSIX vs quartz, second-precision support).
- Whether scheduled actions are bound to a single user/tenant or a
  bucket/subject (Option B implies the latter; Option A inherits the
  rule's existing scoping).
- Concurrent-fire semantics (one tick fires N rules — order
  guarantees? backpressure?).
- Exposure surface (config-only vs HTTP API to register/cancel
  schedules at runtime).

These are implementation-pass concerns; settling them now without code
risks bikeshedding.

## Implementation-pressure triggers

Open this ADR back up and pick when one of:

- A product (semteams, semspec) ships a feature that requires
  cadence-driven dispatch and is blocked without it.
- A new product is sized large enough that the dedicated scheduler
  component is justified by *its* needs (Option B's surface cost
  amortizes across multiple consumers).
- An external cron-based workaround in a deployed product shows
  observability gaps that are operator-felt (missed fires going
  undetected, manual reschedule pain).

Until one of those, defer. Cadence-driven dispatch is a real gap, but
shipping the wrong primitive locks the surface for every product
downstream of it.

## Related

- [ADR-028: Orchestration Architecture](028-orchestration-architecture.md)
  — the rule-skeleton + coordinator + ops-agent layering this primitive
  fits inside.
- [Concept: Orchestration Layers](../concepts/14-orchestration-layers.md)
  — the two-layer rule/component model.
- [Concept: Rule-Driven Artifacts](../concepts/18-rule-driven-artifacts.md)
  — the event-driven version of the pattern this ADR proposes a
  time-driven sibling for.
- GitHub: c360studio/semstreams#15 — the issue that motivated this
  ADR.
