# Rule-Driven Artifacts

External agents and human consumers occasionally need flat, rendered
artifacts derived from graph state — markdown summaries, JSON exports,
HTTP webhook payloads. SemStreams treats these as *product concerns*
rather than framework primitives, but the framework already provides
the moving parts. This page documents the canonical pattern so a
product-side team building artifact emission doesn't reinvent the
wiring.

## When to use this pattern

Reach for it when something *outside* the running system needs to read
graph state in a non-graph shape — a markdown checklist for an external
agent platform, a CSV export for a downstream data team, a JSON
payload to POST at an SLA dashboard.

If the consumer is *inside* SemStreams (another component, a tool, a
rule action), prefer querying the graph directly via the gateway, MCP,
or KV reads. See [Query Access](11-query-access.md) for the in-system
choice.

## The pipeline

```text
┌───────────────────┐          ┌────────────────┐         ┌──────────────────────┐
│ Graph state       │  watch   │  Rule          │ publish │  Output component    │
│ (KV / triples)    ├─────────►│  (rule         ├────────►│  (output/file,       │
│                   │ change   │   processor)   │ subject │   output/httppost,   │
│                   │          │                │         │   etc.)              │
└───────────────────┘          └────────────────┘         └──────────┬───────────┘
                                                                     │
                                                                     ▼
                                                          ┌─────────────────────┐
                                                          │  Disk / HTTP / Obj  │
                                                          │  Storage / etc.     │
                                                          └─────────────────────┘
```

Three pieces, all framework primitives:

1. **A rule** that watches the relevant KV bucket or NATS subject and
   decides when to emit. Rules carry references, not content (see
   [ADR-028](../adr/028-orchestration-architecture.md)).
2. **A `publish` action** that sends a small message to a configurable
   subject. The message can carry the entity ID, a hint about what to
   render, or a pre-formatted body — the rule chooses.
3. **An output component** subscribed to that subject, configured to
   write the artifact to its destination.

Both publishers and subscribers are configured in the flow JSON; no Go
code is required for the basic shape.

## Worked example: emit a JSON snapshot per entity change

```json
{
  "name": "drone-snapshot",
  "type": "rule",
  "config": {
    "rules": [
      {
        "id": "emit-drone-snapshot",
        "watch": {
          "type": "kv",
          "bucket": "ENTITY_STATES",
          "key_pattern": "*.robotics.gcs.drone.*"
        },
        "actions": [
          {
            "type": "publish",
            "subject": "output.drone-snapshot.$entity.id"
          }
        ]
      }
    ]
  }
}
```

Pair it with an `output/file` component subscribed to that subject:

```json
{
  "name": "drone-snapshot-writer",
  "type": "output/file",
  "config": {
    "format": "json",
    "ports": {
      "inputs": [{
        "name": "snapshots",
        "config": {"kind":"jetstream","subjects":["output.drone-snapshot.>"]}
      }]
    },
    "destination": "/var/lib/semstreams/drone-snapshots/"
  }
}
```

Each entity change now produces a JSON file scoped under the entity
ID. Substitute `output/httppost` for outbound webhooks, or wire an
ObjectStore writer for blob-shaped artifacts.

## Worked example: render markdown via a renderer agent

The framework deliberately does **not** ship a template-rendering rule
action. Markdown / DSL formatting is a product-shape concern with too
many opinions to bake in. When you need rendering, dispatch a
short-lived agent that owns the template:

```json
{
  "type": "publish_agent",
  "role": "markdown-renderer",
  "model": "default",
  "prompt": "Render a markdown summary for the entity at $entity.id. Use the read_entity tool to fetch its current state.",
  "tools": ["read_entity"],
  "result_subject": "output.entity-md.$entity.id"
}
```

The `markdown-renderer` role has a system prompt that owns the
template; the rule just hands it the entity ID. The agent's terminal
tool emits the rendered text on `output.entity-md.<id>`, and an
`output/file` component subscribed to that subject persists it.

This puts the template in two places: a product-owned persona file
(see [Coordinator Pattern](../advanced/12-coordinator-pattern.md) for
how persona fragments are loaded) and zero in framework code. Updates
ship via config, not redeploys.

## Available rule actions

| Action | What it does | When to use for artifacts |
|---|---|---|
| `publish` | Sends a message to any NATS subject. | Default. Routes to any subscribed output component. |
| `publish_agent` | Spawns an agentic loop with a role + prompt + result subject. | Need rendering, transformation, or any LLM-mediated shape change. |
| `add_triple` / `update_triple` / `remove_triple` | Appends/updates facts on an **existing** entity (evidence-append, must-exist — `add_triple` no longer births an entity; ADR-055). | When the artifact *is* a graph fact (status flag, computed predicate) on an already-born entity. |
| `update_kv` | Writes to a KV bucket. | When the artifact is a structured snapshot keyed by entity ID. |
| `lifecycle_*` | Advances a lifecycle-managed entity through declared phases. | Operator-visible lifecycle steps such as render → validate → publish. |

Pick the smallest tool that fits — a `publish` to an existing output
component is structurally cheaper than spawning a renderer agent.

## What the framework does NOT provide

- **A `render_template` rule action.** There is no built-in way for a
  rule to fill a template against entity state and emit the result. If
  demand emerges, this is a clean future extension; in the meantime use
  `publish_agent` with a renderer role.
- **Per-entity ObjectStore writes from rules.** Rules can `update_kv`
  but not write directly to an ObjectStore bucket. If the artifact is
  large (>1 MB) wire an output component that consumes the rule's
  publish and writes to ObjectStore on its side.
- **Built-in templates for common formats.** The framework is
  format-agnostic. OB1's USER.md / SOUL.md / HEARTBEAT.md shapes, for
  example, are entirely product-side concerns; the framework ships
  the substrate, not the schema. Issue
  [#13](https://github.com/c360studio/semstreams/issues/13) closed
  out-of-scope on this principle.

## Time-driven artifacts (cron rules)

The patterns above all watch graph or NATS state. Beta.27 added the
[time-driven sibling](../adr/031-time-trigger-primitive.md): a
`"type": "cron"` rule fires on a wallclock schedule and uses the same
`publish` / `publish_agent` actions to emit an artifact. The shape:

```text
┌──────────────┐  schedule    ┌────────────────┐ publish ┌──────────────────┐
│ Wallclock    ├─────────────►│  Cron rule     ├────────►│ Output component │
│ (cron tick)  │  matches     │  (rule         │ subject │ (file / httppost │
│              │  expression  │   processor)   │         │  / etc.)         │
└──────────────┘              └────────────────┘         └──────────┬───────┘
                                                                    │
                                                                    ▼
                                                         ┌────────────────────┐
                                                         │ Disk / HTTP / Obj  │
                                                         │ Storage            │
                                                         └────────────────────┘
```

Use a cron rule when the artifact emission is **cadence-driven** rather
than state-driven — periodic snapshots, scheduled exports, daily roll-
ups. State-driven emission (snapshot-on-profile-change) stays on the
expression rule path described above.

### Worked example: hourly status snapshot

```json
{
  "id": "hourly-status-snapshot",
  "type": "cron",
  "name": "Hourly status snapshot",
  "enabled": true,
  "schedule": "@hourly",
  "actions": [
    {
      "type": "publish",
      "subject": "output.status-snapshot.hourly",
      "payload": {
        "rule_id": "$schedule.id",
        "fired_at": "$now",
        "previous_fire": "$schedule.last_fired_at"
      }
    }
  ]
}
```

An `output/file` component subscribes to `output.status-snapshot.hourly`
and writes each message to a timestamped JSON file. The `$schedule.*`
substitution tokens (`$schedule.id`, `$schedule.spec`,
`$schedule.last_fired_at`) and the always-available `$now` give the
output component everything it needs to name files and detect missed
intervals.

### When to combine cron + ops-role coordinator

For artifacts that require *iteration over graph state* — a
kill-switch sweep that disables flagged entities, a chain-hash audit
that walks a chain looking for breaks — use the
**cron → `publish_agent` → ops-role coordinator** pattern from
[ADR-028](../adr/028-orchestration-architecture.md). The cron rule is
just the clock; the spawned coordinator does the iteration / chain
walk and emits structured `ops.diagnosis.*` triples that downstream
expression rules can match on. See
`configs/rules/cron/governance-example.json` for a worked starting
point.

## Operational notes

- **Subject naming.** Use the standard `output.<purpose>.<routing>`
  pattern. Routing keys (`$entity.id`, user ID, etc.) make per-tenant
  filtering at the subscriber side trivial via NATS subject wildcards.
- **At-least-once semantics.** Rule emission rides on NATS JetStream's
  at-least-once delivery. Consumers should be idempotent — keying
  output filenames by entity ID + revision is a clean way to dedup.
- **Backpressure.** A rule that fires on every triple change can
  saturate slow output components. Use `fire_every_n_events` or a
  cadence-aware condition if the consumer can't keep up.
- **Atomicity.** A rule fires after the underlying KV write commits.
  If the write fails, no rule fires; if the rule fires, the write
  already happened. The output component sees committed state, not
  in-flight transactions.

## Related

- [Orchestration Layers](14-orchestration-layers.md) — the rule /
  workflow / component boundary this pattern lives inside.
- [Query Access](11-query-access.md) — for *in-system* consumers.
- [Streams vs KV Watches](03-streams-vs-kv-watches.md) — picking the
  right substrate when designing the watch side of the rule.
- [ADR-028](../adr/028-orchestration-architecture.md) — why rules
  carry references, not content.
- [ADR-031](../adr/031-time-trigger-primitive.md) — the time-driven
  primitive that powers the cron-rule section above. Shipped in
  beta.27.
- [Migration: beta.26 → beta.27](../operations/migration-beta26-to-beta27.md)
  — upgrade path for the cron rule kind.
