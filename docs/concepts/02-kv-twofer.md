# The KV Twofer

How SemStreams gets current state and change notification from one NATS KV write.

## The Core Insight

Most event-driven systems require two separate infrastructures: somewhere to store current state, and
somewhere to publish events about state changes. Keeping these in sync is a persistent source of bugs.

For the authority write itself, SemStreams avoids a separate state write and
change notification. Every NATS KV bucket is backed by a JetStream stream. A
single write changes current state and notifies declared watchers; it may also
retain explicitly bounded history. Derived convergence still requires each
owner's repair, redrive, readiness, and reset contract.

```text
┌─────────────────────────────────────────────────────────────┐
│                    Any SemStreams KV Bucket                  │
│               (e.g., ENTITY_STATES, PREDICATE_INDEX)        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Interface A: State                                         │
│  ─────────────────                                          │
│  kv.Get("acme.ops.robotics.gcs.drone.001")                  │
│  → Current entity state, right now                          │
│                                                             │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Interface B: Events (Watch)                                │
│  ────────────────────────────                               │
│  kv.Watch("acme.ops.robotics.gcs.drone.*")                  │
│  → Fires on every change to any drone entity                │
│                                                             │
│  Each entry carries: key, value, revision, operation        │
│  Processor reacts to state changes — no separate event bus  │
│                                                             │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Optional: bounded retained history                         │
│  ────────────────────                                       │
│  Only revisions retained by this bucket's History setting  │
│  ENTITY_STATES History: 1 (current value only)              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**The twofer:** writing entity state changes current state and produces a watch
notification. Configured KV history may retain recent per-key revisions, but it
is not automatically an audit or authority-recovery ledger.

## What This Replaces

In conventional stream processing, you maintain a separate event log alongside a state store:

```text
Conventional approach:

  Producer ──publish──► Kafka Topic    (event log)
      │
      └──write───────► Database        (state store)

  Consumer ─subscribe─► Kafka Topic
                            and
           ──query────► Database

  Problem: Two writes per update. Two systems to keep consistent.
           Missed events = stale state. Missed writes = silent data loss.
```

SemStreams collapses this to one operation:

```text
SemStreams:

  Producer ──Put──► ENTITY_STATES KV
                         │
                         ├─── Get → current state (Interface A)
                         ├─── Watch → event notification (Interface B)
                         └─── History → bounded retained revisions (if configured)

  One write. Two core interfaces, plus configured bounded history.
```

## How It Flows Through SemStreams

State-reactive graph processors use this pattern. Periodic owners may instead
read current state on their scheduled cycle.

```text
External data                           Internal processing
─────────────                           ──────────────────

  Sensor         registered Graphable event
  Adapter  ─────► configured entity stream ─────► graph-ingest
                  (default entity.>)
                                            │
                                            │ kv.Put()
                                            ▼
                                      ENTITY_STATES
                                            │
                               ┌────────────┼────────────┐
                               │            │            │
                           kv.Watch     kv.Watch     kv.Watch
                               │            │            │
                               ▼            ▼            ▼
                          graph-index  graph-rules  graph-embedding
                               │
                           kv.Put()
                               ▼
                          PREDICATE_INDEX
                          OUTGOING_INDEX
                          INCOMING_INDEX
```

`graph-ingest` writes to `ENTITY_STATES`. Graph-index, rule evaluation, and
embedding owners can react through KV watches. Graph-clustering periodically
reads current authority and topology instead. The write is still the change
notification for declared watchers; it does not force every owner into a watch.

## Owner-internal watch patterns

This section describes authority-owner mechanics and operator diagnostics. An
application or ordinary reactive component does not acquire `ENTITY_STATES`.
There is no canonical general reactive semantic subscription today. Controlled
framework internals may use a declared raw owner/dependency seam; raw KV is not an
adopter fallback. No general typed subscription is scheduled without measured
adopter evidence.

Because entity IDs are 6-part hierarchical keys, the same watch mechanism supports three levels of
subscription specificity without any routing configuration:

```text
6-part entity ID:  org . platform . domain . system . type . instance
                   acme . ops     . robotics . gcs  . drone . 001
```

| Watch Pattern | Subscribes To | Example Use |
|--------------|--------------|-------------|
| Full key | One specific entity | Track a particular drone's state |
| Type wildcard (`...type.*`) | All entities of a type | React to any drone change |
| Subtree wildcard (`...system.>`) | All entities in a subsystem | React to anything in GCS |

```go
// One drone
watcher, _ := entityStates.Watch("acme.ops.robotics.gcs.drone.001")

// All drones on this platform
watcher, _ := entityStates.Watch("acme.ops.robotics.gcs.drone.*")

// Everything in the robotics subsystem
watcher, _ := entityStates.Watch("acme.ops.robotics.>")
```

No topic registry. No routing tables. The hierarchy falls out of the EntityID structure.

## Handling the Initial Values

When a watcher starts, it first delivers all current values matching the pattern, then transitions to
delivering live updates. The NATS client signals this transition with a `nil` entry.

This matters for processors: they must distinguish bootstrap from live to avoid treating every existing
entity as a "new" event on restart.

```go
watcher, _ := entityStates.Watch("acme.ops.robotics.gcs.drone.*", nats.Context(ctx))

bootstrapping := true

for entry := range watcher.Updates() {
    if entry == nil {
        // Transition point: all current values delivered, live updates follow
        bootstrapping = false
        p.logger.Info("bootstrap complete, processing live updates")
        continue
    }

    if bootstrapping {
        // Hydrate cache from current state — don't treat as new events
        p.cache[entry.Key()] = decode(entry.Value())
    } else {
        // Live update — diff against cache to detect what changed
        p.handleChange(entry)
    }
}
```

Bootstrap supplies a processor's current matching inputs before live updates. It
does not by itself recover failed writes, stale removals, dependency changes, or
reset state. Recovery is complete only under that owner's declared lifecycle,
repair, readiness, and redrive behavior.

## History Is Explicit and Bounded

KV buckets default to `History: 1` (latest value only). `ENTITY_STATES` is
deliberately fixed at that depth, so it cannot reconstruct past authority. A
different, non-authoritative store may retain a bounded recent per-key history
when its own contract requires it:

```go
js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
    Bucket:  "RECENT_STATUS_EXAMPLE",
    History: 64,
    TTL:     7 * 24 * time.Hour,
})
```

With `History: 64`, that example bucket stores at most the last 64 values per key.
It remains a bounded storage policy, not a complete event log. If complete audit
or authority recovery is required, design that capability explicitly.

---

## Reactive observation boundary

An adopter does not derive a raw bucket key or watch `PREDICATE_INDEX`. No
canonical general typed reactive seam exists or is scheduled. An owner-specific
typed observation operation requires a named-adopter census, a before/after
public-surface and adopter-decision table, and owner acceptance proving it reduces
knowledge and total surface.

Raw bucket watches belong only inside the authority or declared projection owner,
or in explicit operator diagnostics. Bucket names, composite key layout, marker
values, bootstrap mechanics, poison, watcher loss, repair, and reset remain the
owner's responsibility rather than an application recipe.

The four access strata are intentionally distinct:

1. applications query through an admitted HTTP operation or named typed adapter;
2. reactive components have no general public subscription; measured consumers
   may propose a named typed operation;
3. projection owners use only their declared buckets and dependencies; and
4. operators may inspect raw KV explicitly for diagnostics.

Only controlled framework internals use a declared raw owner/dependency seam. If
no typed operation exists, raw KV is not the adopter fallback; the capability is
not admitted.

---

## In Context: The Full Event Model

To be precise about where the twofer applies and where it doesn't:

```text
                          ┌──────────────────────────────┐
                          │  Asynchronous fact seam      │
                          │                              │
  External systems  ─────►│  registered Graphable event │
  Input processors        │  on configured entity stream│
  Standards adapters      │  (default entity.>)          │
                          └──────────────┬───────────────┘
                                         │
                                    graph-ingest
                                         │
                                         ▼
                          ┌──────────────────────────────┐
                          │  Internal graph              │
                          │  (KV twofer)                 │
                          │                              │
                          │  ENTITY_STATES authority     │
                          │       │                      │
                          │       ▼ declared owners      │
                          │  named materialized views    │
                          │                              │
                          └──────────────────────────────┘
```

The fact seam uses a configured JetStream input and declares its own acceptance,
redelivery, idempotency, and observability behavior. Graph-ingest alone writes
authority. Declared view owners may watch or periodically read their dependencies,
and each owns repair, redrive, readiness, and reset. Applications use admitted
typed seams rather than the buckets shown internally; no general reactive
application seam is implied.

---

## Related

**Concepts**
- [Streams vs KV Watches](03-streams-vs-kv-watches.md) — when to use the twofer vs JetStream
  streams, and why agentic components use both
- [Event-Driven Basics](01-event-driven-basics.md) — pub/sub, JetStream, and KV fundamentals
- [Knowledge Graphs](04-knowledge-graphs.md) — how triples create the predicates being watched
- [Real-Time Inference](00-real-time-inference.md) — how the twofer enables each inference tier

**Reference**
- [Index and Bucket Reference](../advanced/05-index-reference.md) — complete bucket inventory,
  key formats, and ownership table
- [Vocabulary](../basics/04-vocabulary.md) — governed predicate naming
- [Graph Components](../advanced/07-graph-components.md) — declared owner dependencies
