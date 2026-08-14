# Streams vs KV Watches

How to choose between JetStream streams and KV watches, and why agentic and workflow components use both.

## The Heuristic

SemStreams uses two NATS communication primitives for internal coordination. The choice between them
is not arbitrary — each maps to a fundamentally different kind of communication.

```text
┌─────────────────────────────────────────────────────────────────────┐
│                        The Decision                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   "Is this current state to rehydrate                              │
│    or queued work to resume?"                                      │
│                                                                      │
│   Current fact/state ───────────────► KV Watch (twofer)             │
│   Request/work item ────────────────► JetStream Stream              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

A drone's battery level is a fact. "Call this LLM with these messages" is a request. "This sensor
updated" is a fact. "Execute this tool" is a request. The distinction is usually obvious, and it
drives the right technical choice automatically.

## Why the Distinction Matters

The two primitives have fundamentally different restart semantics, and restart behavior reveals what
each one is actually for.

### KV Watch on Restart

When a KV-watching processor restarts, it receives all current values matching its watch pattern
before processing any new changes. This is the bootstrap phase from [the KV Twofer doc](02-kv-twofer.md).

```text
graph-index restarts:

  ENTITY_STATES delivers all current entities (bootstrap)
       │
       ├── entity A (revision 47)
       ├── entity B (revision 12)
       └── entity C (revision 103)
       │
       nil  ◄── bootstrap complete
       │
       ├── entity D (new, live)
       └── entity A updated (live)
```

This bootstrap hydrates the processor's current matching inputs. It does not prove
its output buckets are consistent and is not continuous retry. `graph-index`
still needs explicit repair/redrive and readiness evidence to complete recovery.

### JetStream Consumer on Restart

Durable consumer state tracks acknowledgments. On restart, unacknowledged work
may be redelivered; acknowledged work is not replayed. Consumers remain
at-least-once and must make effects idempotent.

```text
agentic-loop restarts (DeliverPolicy: "new", durable consumer):

  AGENT stream has messages at seq 1..50
  Consumer last acked seq 43

  On restart: delivers seq 44, 45, 46... (in-flight messages only)
  Does NOT replay: seq 1-43 (already handled)
```

This is correct behavior for a work queue. An LLM task that was already dispatched should not be
re-dispatched because the orchestrator restarted. Replaying would mean double-executing work with
real cost and side effects.

**The restart test:** if rehydrating all current matching values is correct, use
KV watch. If work must resume from acknowledgment without re-executing completed
requests, use a JetStream stream. KV history is bounded; this test never assumes
replay from the beginning of time.

---

## The Two Dimensions

The restart question is the sharpest test, but two other dimensions reinforce it:

### Dimension 1: Fan-out vs. Queue

```text
KV Watch — Fan-out:                    JetStream — Queue:

  ENTITY_STATES write                    AGENT stream message
       │                                       │
       ├──► graph-index (watches)              │ (one consumer gets it)
       ├──► graph-rules (watches)              ▼
       └──► graph-embedding (watches)    agentic-loop-instance-A
```

KV watches are naturally fan-out: every declared watcher sees every change.
Periodic derived owners may instead read current state on their own cycle.

JetStream consumers in a queue group are naturally competing: only one consumer instance handles
each message. This is correct for work items — only one loop orchestrator should execute a given
task.

### Dimension 2: Processing Time

```text
KV Watch:                              JetStream:

  Entity state change arrives            Task message arrives
       │                                       │
  Process in microseconds                Process over minutes
  (indexing, rule eval)                  (LLM calls, tool execution)
       │                                       │
  No ack needed                         AckWait, InProgress heartbeats,
  Idempotent on retry                   MaxDeliver, BackOff all apply
```

KV watches have no processing acknowledgment or redelivery. A derived owner must
apply desired state idempotently, redrive or repair failed work explicitly, and
publish readiness/degradation. Bootstrap rehydrates current inputs after restart;
it does not continuously retry failed output. If an owner cannot meet those
obligations, it must redesign the seam rather than use a watch as a work queue.

JetStream consumers with explicit ack give you the full tuning surface from the
[JetStream Tuning Guide](../advanced/11-jetstream-tuning.md): `AckWait` for deadline enforcement,
`InProgress` heartbeats for long operations, `BackOff` for graduated retry, `MaxAckPending`
for delivery admission. SemStreams ordinary port-backed consumers honor that port field and report its requested and
effective values; components that own a fixed policy reject nonzero overrides. This NATS limit is distinct from local
queue capacity and execution concurrency.

---

## How Agentic Components Use Both

The agentic components use both primitives correctly — streams for work items, KV for state — and
seeing them side-by-side makes the distinction concrete.

```text
┌───────────────────────────────────────────────────────────────────┐
│                    Agentic Component Architecture                  │
├───────────────────────────────────────────────────────────────────┤
│                                                                    │
│  Work items (JetStream streams):                                  │
│                                                                    │
│   agent.task.*               ──► agentic-loop    "Execute this task"        │
│   agent.request.*            ──► agentic-model   "Call LLM with these msgs" │
│   tool.execute.*             ──► agentic-tools   "Run this tool"            │
│   tool.result.*              ──► agentic-loop    "Here's the tool output"   │
│   agent.response.*           ──► agentic-loop    "Here's the LLM output"    │
│   agent.toolcall.proposed.*  ──► rule processor  "Should this call run?"    │
│   agent.toolcall.approved.>  ──► agentic-loop    "Verdict: approved"        │
│   agent.toolcall.rejected.>  ──► agentic-loop    "Verdict: rejected"        │
│                                                                    │
│  State (KV buckets — twofer):                                     │
│                                                                    │
│   AGENT_LOOPS        "What state is loop X in?"                   │
│                                                                    │
│  Derived graph state (KV buckets — twofer):                       │
│                                                                    │
│   ENTITY_STATES      "What do we know about agent X's loop?"     │
│   PREDICATE_INDEX    "Which loops are in 'executing' state?"      │
│                                                                    │
└───────────────────────────────────────────────────────────────────┘
```

### Why task dispatch uses streams

`agent.task.*` carries an instruction to do expensive work. If the agentic-loop component
restarted, you do not want current KV keys to be interpreted as fresh work. You want it to
resume only the tasks that were in flight at the time of the crash.

`DeliverPolicy: "new"` on a durable consumer achieves exactly this: the consumer position
is persisted, and on restart it picks up from the last acked message.

If the system were to use a KV watch for task dispatch instead, every restart would re-trigger
every current matching task key. That could re-run LLM tasks, re-execute tools with side
effects, and produce duplicate results.

### Why loop state uses KV

`AGENT_LOOPS` stores the current state of each loop entity: which phase it's in, how many
iterations it has run, which tool calls are pending. This is a fact about the world, not a
request to do anything.

Any component that needs to know a loop's current state can call `kv.Get()`. Any component
that wants to react when a loop enters a specific phase can call `kv.Watch()`. When the
agentic-loop component restarts, it can recover the full current state of all active loops
from the bucket — no replay of task messages needed.

If loop state were published to a JetStream stream instead, it would not be queryable by
other components without consuming and replaying the stream. The stream would grow without
bound. And the latest-value semantics of KV (which is all you usually need — "what state is
this loop in right now?") would require extra work to reconstruct.

### Why tool results use streams

`tool.result.*` carries the output of a specific tool call back to the orchestrating loop.
This is a work item response — it is only meaningful to the specific loop instance that
issued the tool call. Delivery is at least once, so handlers remain idempotent.

A KV approach would require the loop to poll or watch a known key for its result. Streams
deliver the result push-style to the consumer that is waiting for it, with at-least-once
guarantees and explicit ack. The stream also acts as a buffer — if the agentic-loop is
processing a previous result when a tool finishes, the result waits in the stream rather
than being dropped.

---

## Decision Guide

Use the canonical four tests in
[`kv-or-stream`](../../.agents/skills/kv-or-stream/SKILL.md). The sharpest is the
restart test:

```text
Rehydrate all current matching values  -> KV Watch
Resume unacknowledged queued work       -> JetStream Stream
```

The remaining tests ask fan-out versus queue, processing behavior, and current
fact versus request. Conflicting answers mean the concept may need to split.

### Common Cases

| Communication | Right primitive | Reason |
|--------------|-----------------|--------|
| Entity state changed | KV Watch | Fact; fan-out; fast; idempotent |
| New task to execute | JetStream Stream | Request; queue; expensive; side effects |
| Index update | Owner KV write | Only the declared projection owner writes |
| LLM call | JetStream Stream | Request; queue; slow; costly |
| Loop current state | KV | Fact; queryable; recoverable |
| Tool execution request | JetStream Stream | Request; queue; has side effects |
| Completion notification | JetStream Stream | At-least-once downstream work |
| External telemetry | Graphable or typed ingest | graph-ingest owns authority |

---

## What This Looks Like in Code

A component using both primitives in the same process is completely normal. The agentic-loop
does exactly this — it has JetStream consumers for work item inputs and KV bucket handles for
state:

```go
type AgenticLoop struct {
    // Work item channels (JetStream)
    taskConsumer     jetstream.ConsumeContext  // agent.task.* — inbound work
    responseConsumer jetstream.ConsumeContext  // agent.response.* — LLM results
    resultConsumer   jetstream.ConsumeContext  // tool.result.* — tool results

    // State (KV twofer)
    loopsBucket nats.KeyValue // AGENT_LOOPS — current loop state

    // Outbound work (JetStream publish)
    js jetstream.JetStream  // publish to agent.request.*, tool.execute.*
}
```

The separation is visible in the type signatures: `jetstream.ConsumeContext` for
work items and `nats.KeyValue` for loop state. Trajectories are not a current KV
bucket: active/finalized reads use memory and a TTL cache, while durable content
uses ObjectStore and graph entities.

---

## Related

**This document builds on:**
- [The KV Twofer](02-kv-twofer.md) — KV watch mechanics, bootstrap phase, predicate channels

**Context:**
- [JetStream Tuning Guide](../advanced/11-jetstream-tuning.md) — AckWait, InProgress, MaxAckPending
  for the JetStream side of this pattern
- [Agentic Components Reference](../advanced/08-agentic-components.md) — full port and
  configuration details for agentic-loop, agentic-model, agentic-tools
- [Event-Driven Basics](01-event-driven-basics.md) — JetStream and KV fundamentals
