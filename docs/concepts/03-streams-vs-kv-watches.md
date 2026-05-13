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
│   "Is this a fact about the world                                   │
│    or a request to do something?"                                   │
│                                                                      │
│   Fact about the world  ──────────────────► KV Watch (twofer)      │
│   Request to do something ────────────────► JetStream Stream        │
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

This is correct behavior for a processor that maintains derived state. `graph-index` needs to
re-index everything it can see to ensure its output buckets are consistent with `ENTITY_STATES`.
Replaying is not a bug — it's recovery.

### JetStream Consumer on Restart

When a JetStream consumer restarts with `DeliverPolicy: "new"`, it picks up from where it left off
in the stream, processing only messages that arrived after it last acked. Messages it already
processed stay processed.

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

**The restart test:** If replaying every message since the beginning of time would be correct and
harmless, use KV watch. If it would be catastrophic, use a JetStream stream.

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
       ├──► graph-embedding (watches)    agentic-loop-instance-A
       └──► graph-clustering (watches)
```

KV watches are naturally fan-out: every watcher sees every change. This is correct for derived
state — multiple indexes should all update when an entity changes.

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

KV watches have no ack mechanism — they're fire-and-react. This is fine for fast, idempotent
processing. If the processor crashes mid-update, it will recover the correct current state on
restart from the next watch delivery or bootstrap.

JetStream consumers with explicit ack give you the full tuning surface from the
[JetStream Tuning Guide](../advanced/11-jetstream-tuning.md): `AckWait` for deadline enforcement,
`InProgress` heartbeats for long operations, `BackOff` for graduated retry, `MaxAckPending`
for backpressure. These tools exist precisely because work items have variable, potentially
long processing times and real consequences for failure.

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
│   AGENT_TRAJECTORIES "What did loop X do, step by step?"         │
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
restarted, you do not want it to re-execute every task it has ever received. You want it to
resume only the tasks that were in flight at the time of the crash.

`DeliverPolicy: "new"` on a durable consumer achieves exactly this: the consumer position
is persisted, and on restart it picks up from the last acked message.

If the system were to use a KV watch for task dispatch instead, every restart would re-trigger
every task ever queued. That would be catastrophic — re-running LLM tasks, re-executing tools
with side effects, producing duplicate results.

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
issued the tool call, and it should be delivered exactly once.

A KV approach would require the loop to poll or watch a known key for its result. Streams
deliver the result push-style to the consumer that is waiting for it, with at-least-once
guarantees and explicit ack. The stream also acts as a buffer — if the agentic-loop is
processing a previous result when a tool finishes, the result waits in the stream rather
than being dropped.

---

## The Workflow Processor

The same logic applies to the workflow processor, and it's worth being explicit because
workflows have a mix of triggers (streams) and state (KV) in close proximity.

```text
Workflow trigger:   workflow.trigger.{workflow_id}  ← JetStream stream
                         │
                         │  "Execute this workflow with these inputs"
                         │  (a request to do something)
                         ▼
                    workflow-processor

Execution state:    WORKFLOW_EXECUTIONS KV           ← KV twofer
                         │
                         │  "What step is execution X on?"
                         │  (a fact about the world)
                         │
                         ├── readable by anything
                         ├── watchable for status updates
                         └── recoverable after restart
```

The trigger is a stream message because "start this workflow" is a request with real cost —
you don't want it replayed on restart. The execution state is KV because "what step is this
execution on" is a fact that should be readable and recoverable.

Timer state follows the same pattern. `WORKFLOW_TIMERS` is KV because it records when timers
are scheduled to fire — a fact. The timer fire event itself (`workflow.timer.fire`) is a
stream message because "fire this timer now" is a request that should happen exactly once.

---

## Decision Guide

Apply these tests in order. The first test that gives a clear answer is usually sufficient.

```text
1. Restart test
   ─────────────
   If this processor restarted, should it re-process messages it already handled?

   Yes (re-process is correct recovery) ──────────────► KV Watch
   No (re-process would be wrong)  ───────────────────► JetStream Stream


2. Fan-out vs. queue test
   ───────────────────────
   Should multiple processors all react to this, or should only one handle it?

   All react (fan-out)  ───────────────────────────────► KV Watch
   Only one handles it (queue)  ───────────────────────► JetStream Stream


3. Processing time test
   ─────────────────────
   Is the processing fast and idempotent, or slow with real side effects?

   Fast and idempotent  ───────────────────────────────► KV Watch
   Slow or has side effects  ──────────────────────────► JetStream Stream


4. Nature test
   ────────────
   Is this a fact about the world, or a request to do something?

   Fact  ─────────────────────────────────────────────► KV Watch
   Request  ──────────────────────────────────────────► JetStream Stream
```

If any test gives conflicting answers, that is a signal the design may be muddled — revisit
whether the concept is actually a single thing or two things conflated.

### Common Cases

| Communication | Right primitive | Reason |
|--------------|-----------------|--------|
| Entity state changed | KV Watch | Fact; fan-out; fast; idempotent |
| New task to execute | JetStream Stream | Request; queue; expensive; side effects |
| Index update | KV Write → others watch | Fact; fan-out; fast |
| LLM call | JetStream Stream | Request; queue; slow; costly |
| Loop current state | KV | Fact; queryable; recoverable |
| Tool execution request | JetStream Stream | Request; queue; has side effects |
| Workflow trigger | JetStream Stream | Request; queue; expensive |
| Workflow execution state | KV | Fact; queryable; recoverable |
| Completion notification | JetStream Stream | Request (to downstream); once |
| Sensor telemetry | KV Write → ENTITY_STATES | Fact; latest-value semantics |

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
    loopsBucket       nats.KeyValue  // AGENT_LOOPS — current loop state
    trajectoriesBucket nats.KeyValue // AGENT_TRAJECTORIES — execution trace

    // Outbound work (JetStream publish)
    js jetstream.JetStream  // publish to agent.request.*, tool.execute.*
}
```

The separation is visible in the type signatures: `jetstream.ConsumeContext` for work item
inputs, `nats.KeyValue` for state. The distinction is structural, not just conceptual.

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
