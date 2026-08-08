# NATS JetStream Tuning Guide for Agentic Loops

**Context:** SemStreams agentic components — `agentic-loop`, `agentic-model`, `agentic-tools` — run JetStream
consumers that trigger async work with wildly variable latency: sub-second for rule evaluation, seconds for tool
calls, and potentially 5–30 minutes for multi-step LLM chains. This guide documents the knobs that matter and
how to set them correctly across these tiers.

---

## The Core Tension

JetStream's at-least-once guarantee is implemented by tracking outstanding acks. If a message isn't acked within
`AckWait`, the server redelivers it. This is great for crash recovery, bad for slow-by-design consumers like LLM
calls. The instinct to just increase `AckWait` globally is also wrong — a 30-minute AckWait means a truly lost
message stays lost for 30 minutes before retry.

The right answer is **differentiated consumer configs by latency class** + **in-progress heartbeats** for long
operations.

---

## Current State

All three agentic components share this consumer pattern:

```go
cfg := natsclient.StreamConsumerConfig{
    AckPolicy:  "explicit",
    MaxDeliver: 3,
    // AckWait: not set → defaults to 30s
    // MaxAckPending: not set → defaults to 1000
}

// Handler acks AFTER processing completes
err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
    handler(msgCtx, msg.Data())   // ← LLM call lives here, could be minutes
    msg.Ack()                     // ← this arrives after AckWait has expired
})
```

**Problems:**

1. **Ghost acks:** The 30s `AckWait` expires before the LLM step completes. NATS redelivers to the next available
   subscriber (or re-queues for the same one). The original call eventually acks a message that has already been
   redelivered — duplicate processing.

2. **No backpressure:** `MaxAckPending` of 1000 means the server can flood the consumer with 1000 concurrent LLM
   requests before applying backpressure. For an agentic loop, this is almost never what you want.

3. **Fixed MaxDeliver=3 across all tiers:** Tool calls might warrant 3 retries. An LLM reasoning step that requires
   human-quality output should probably get 1–2 attempts max, with proper failure signaling rather than silently
   retrying.

---

## Key Consumer Config Fields

### AckWait — When to Redeliver

The window the server waits for an ack before marking a message as failed and redelivering.

| Consumer Type         | Recommended `AckWait` |
|-----------------------|----------------------|
| Rule / structural     | `30s`                |
| BM25 / algorithm tier | `60s`                |
| Tool call             | `5m`                 |
| LLM step (model call) | `2m` + heartbeats    |
| Full agentic loop     | `30m` + heartbeats   |

Do not set a giant `AckWait` globally. Use heartbeats for long work instead (see below).

### MaxAckPending — In-Flight Limit and Backpressure

The maximum number of unacknowledged messages across all subscriptions bound to a consumer. When this limit is
reached the server **stops delivering new messages** — this is NATS giving you backpressure for free.

```text
Fast Publisher ──► Stream ──► Consumer (MaxAckPending=5)
                               │
                         Only 5 in flight at a time.
                         Server holds the rest until acks arrive.
```

For agentic work where each message triggers a goroutine with real resource cost:

| Consumer Type           | `MaxAckPending` |
|-------------------------|-----------------|
| Agentic loop (serial)   | `1`             |
| Agentic loop (parallel) | `worker_count`  |
| Tool calls              | `concurrency`   |
| LLM model calls         | `1`–`3`         |

Setting `MaxAckPending: 1` forces serial processing — NATS will not deliver the next message until the current
one is acked. This is the simplest and most correct approach for single-threaded loop orchestration.

### MaxDeliver — Retry Limit

Maximum delivery attempts before an advisory is emitted on
`$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.*`.

- Set this low (`2`–`3`) and handle the advisory rather than letting the server silently retry expensive work.
- At `MaxDeliver: 1`, a failure becomes a dead letter — you must handle recovery explicitly.

### BackOff — Retry Timing

Overrides `AckWait` per-attempt. Index 0 is the first retry wait, index 1 is the second, etc. Values at the
last index are used for all subsequent retries.

```go
BackOff: []time.Duration{
    10 * time.Second,   // retry 1: fast
    60 * time.Second,   // retry 2: slower
    5 * time.Minute,    // retry 3+: back off significantly
},
```

Use `BackOff` instead of a flat `AckWait` when retries should be graduated.

---

## The In-Progress Heartbeat Pattern

For long-running work, ack **InProgress** on a timer to reset the AckWait clock without completing the message.
This lets you keep `AckWait` short for failure detection while supporting arbitrarily long processing.

```go
func consumeWithHeartbeat(ctx context.Context, msg jetstream.Msg, work func(context.Context) error) error {
    ackWait := 90 * time.Second           // Set AckWait to this on the consumer
    heartbeatInterval := 60 * time.Second // Beat before the window expires

    done := make(chan error, 1)
    go func() {
        done <- work(ctx)
    }()

    ticker := time.NewTicker(heartbeatInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if err := msg.InProgress(); err != nil {
                // Connection issue — work continues but we can't extend the window.
                // Log and let the timer expire naturally.
                return fmt.Errorf("failed to send InProgress: %w", err)
            }

        case err := <-done:
            if err != nil {
                // Signal NATS we failed but don't want immediate retry.
                _ = msg.NakWithDelay(30 * time.Second)
                return err
            }
            return msg.Ack()

        case <-ctx.Done():
            _ = msg.NakWithDelay(5 * time.Second)
            return ctx.Err()
        }
    }
}
```

**How InProgress works:** Sends `+WPI` reply to the server, which resets the AckWait timer for that specific
message. The message stays in the in-flight set — `MaxAckPending` is not decremented — but the redelivery
clock resets.

---

## Recommended Consumer Configs by Component

### agentic-loop — Orchestrator

The loop is stateful and serial by design. One message in flight at a time is correct. AckWait covers the
worst-case tool round-trip; heartbeats handle LLM steps.

```go
cfg := natsclient.StreamConsumerConfig{
    StreamName:    streamName,
    ConsumerName:  consumerName,
    FilterSubject: subject,
    DeliverPolicy: "new",
    AckPolicy:     "explicit",
    AckWait:       90 * time.Second,    // short enough to detect real failures
    MaxDeliver:    2,                   // one retry then dead-letter
    MaxAckPending: 1,                   // serial — NATS holds next until ack
    BackOff: []time.Duration{
        30 * time.Second,               // retry 1: wait 30s
        2 * time.Minute,                // retry 2+: back off
    },
    AutoCreate: false,
}
```

For the actual handler, wrap long work with the heartbeat pattern above.

### agentic-model — LLM Calls

Model calls are the long pole. Serial within the loop context, but you may want light parallelism for parallel
tool resolution. Heartbeats are essential.

```go
cfg := natsclient.StreamConsumerConfig{
    StreamName:    streamName,
    ConsumerName:  consumerName,
    FilterSubject: port.Subject,
    DeliverPolicy: "new",
    AckPolicy:     "explicit",
    AckWait:       2 * time.Minute,     // baseline window
    MaxDeliver:    2,
    MaxAckPending: 1,                   // don't queue multiple LLM calls
    AutoCreate:    false,
}
```

With `InProgress` heartbeats every 90 seconds, a 30-minute model call is fully supported.

### agentic-tools — Tool Calls

Tool calls are generally faster but highly variable (web fetch vs DB lookup vs external API). Slightly more
parallelism is acceptable since tool calls are idempotent or can be made idempotent.

```go
cfg := natsclient.StreamConsumerConfig{
    StreamName:    streamName,
    ConsumerName:  consumerName,
    FilterSubject: port.Subject,
    DeliverPolicy: "new",
    AckPolicy:     "explicit",
    AckWait:       5 * time.Minute,
    MaxDeliver:    3,
    MaxAckPending: 3,                   // allow some concurrency for parallel tools
    BackOff: []time.Duration{
        15 * time.Second,
        60 * time.Second,
    },
    AutoCreate: false,
}
```

---

## Producer Configuration

### Async Publish with MaxPending

For components that publish into the NATS stream, use async publish with a bound on in-flight publishes. This
gives you producer-side backpressure and prevents a fast producer from outrunning consumer capacity.

```go
js, err := nc.JetStream(nats.PublishAsyncMaxPending(64))

// Publish without blocking
ack, err := js.PublishAsync(subject, data)

// Periodically flush and check
select {
case <-js.PublishAsyncComplete():
    // all publishes confirmed
case <-time.After(5 * time.Second):
    // timeout — handle pending acks
}
```

`PublishAsyncMaxPending` is the producer analogue of `MaxAckPending`. When in-flight publish acks hit the
limit, the next `PublishAsync` call blocks. This closes the backpressure loop end-to-end:

```text
Producer (MaxPending=64)
    │
    ▼
Stream
    │
    ▼
Consumer (MaxAckPending=1 for loop, 3 for tools)
    │
    ▼
Handler + InProgress heartbeats
    │
    ▼
Ack → frees capacity up the chain
```

### Deduplication Window

For agentic loops where the same task might be submitted more than once (retried HTTP request, reconnect
storm), the stream-level duplicate window prevents double processing. Set it to cover your expected submission
window:

```go
js.AddStream(&nats.StreamConfig{
    Name:       "AGENT",
    Subjects:   []string{"agent.>"},
    Duplicates: 5 * time.Minute,   // dedup window
})
```

Publish with a message ID header for dedup to work:

```go
js.PublishMsg(&nats.Msg{
    Subject: "agent.task",
    Data:    payload,
    Header: nats.Header{
        nats.MsgIdHdr: []string{taskID},   // "Nats-Msg-Id"
    },
})
```

---

## KV Bucket Tuning

KV buckets used for loop state and immutable trajectory observations don't need the same treatment as streams, but
their policies are intentionally different.

### History

Loop entities reuse one key, so deployments may retain more than one revision when they need state-transition history:

```go
js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
    Bucket:  "AGENT_LOOPS",
    History: 10,              // keep last 10 values per key
    TTL:     24 * time.Hour,  // auto-expire completed loops
})
```

`AGENT_TRAJECTORIES` is different: each observed attempt has its own immutable key. Keep history at `1`; increasing
per-key history adds no audit information. Foundation B also applies no fact TTL because evidence retention and
reference-aware garbage collection require a separate operator policy.

### Watchers vs Polling

KV watchers are push-based — the server notifies on change. They are preferable to polling for loop state
coordination. However, a watcher is backed by a JetStream consumer internally. For tight agentic loops,
ensure watcher consumers are accounted for in your overall consumer count.

---

## Monitoring and Dead Letters

### Observing Max Delivery Advisories

When a message hits `MaxDeliver`, NATS emits an advisory. Subscribe to these for alerting and recovery:

```go
nc.Subscribe("$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.AGENT.>", func(msg *nats.Msg) {
    // Parse io.nats.jetstream.advisory.v1.max_deliver
    // Contains stream_seq, consumer_seq, deliveries count
    // Log, alert, or route to dead-letter processing
})
```

### Consumer Info for Visibility

Pull consumer info to observe pending, in-flight, and redelivered counts:

```go
consumer, _ := js.Consumer(ctx, "AGENT", "agentic-loop-agent-task")
info, _ := consumer.Info(ctx)

// info.NumAckPending   — in flight right now
// info.NumRedelivered  — times a message has been retried (watch this)
// info.NumPending      — messages in stream not yet delivered
// info.NumWaiting      — waiting pull requests (pull consumers)
```

A rising `NumRedelivered` is the primary signal that `AckWait` is too short or `InProgress` heartbeats are
not firing.

---

## Summary Decision Table

| Scenario                        | AckWait          | MaxAckPending | MaxDeliver | InProgress? |
|---------------------------------|------------------|---------------|------------|-------------|
| Structural rule eval            | `30s`            | `10`          | `3`        | No          |
| BM25 / algorithm tier           | `60s`            | `5`           | `3`        | No          |
| Tool call (fast, idempotent)    | `5m`             | `3`           | `3`        | Optional    |
| Tool call (slow, external API)  | `2m`             | `1`–`2`       | `2`        | Yes         |
| LLM model call                  | `2m` + heartbeat | `1`           | `2`        | **Yes**     |
| Agentic loop orchestration      | `2m` + heartbeat | `1`           | `2`        | **Yes**     |

---

## Migration Plan for Current Code

The immediate changes with the most impact, in order of priority:

1. **Add `InProgress` heartbeats to LLM handlers (breaks redelivery bug).**
   Wrap the handler body in all three agentic components with the heartbeat pattern. This is a drop-in change
   that doesn't require consumer config changes.

2. **Set `MaxAckPending: 1` on loop and model consumers (free backpressure).**
   This is a consumer config change. Durable consumers must be deleted and recreated to change
   `MaxAckPending` — this is a NATS limitation. Plan for a brief restart cycle.

3. **Set explicit `AckWait` matching the latency class.**
   Replace the implicit 30s default with explicit values per component. Use `BackOff` slices instead of a
   flat `AckWait` for graduated retry behavior.

4. **Subscribe to max delivery advisories.**
   Wire up an advisory subscriber in the framework's health/telemetry layer so redelivery storms are visible.

5. **Add dedup headers on task submission.**
   Prevents double-processing during reconnect storms — critical for DDIL environments.
