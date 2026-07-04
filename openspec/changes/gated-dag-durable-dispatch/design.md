# Design — Gated-DAG durable dispatch

The decision + rationale live in ADR-070; this captures the concrete mechanics
and sequencing. **Framework side only** — the consumer migration (semspec,
semdragon) is a follow-up on a framework tag.

## Non-breaking by construction

A JetStream `PublishToStreamWithAck` publishes to the *subject*; the stream
captures a persisted copy AND core-NATS subscribers on that subject still receive
the live message. So switching the executor from `nc.Publish` to
`PublishToStreamWithAck` (with the dispatch stream provisioned) **does not break
the existing core-NATS consumers** — they keep receiving dispatches while the
stream begins persisting them. Consumers migrate to the durable consumer later,
atomically (a consumer must not run both a core sub and a durable consumer, or it
double-processes). This is why the framework side ships and tags independently.

## natsclient — `ConsumeDurable`

```go
// ConsumeDurable runs a durable at-least-once consumer: it composes
// ConsumeStreamWithConfig + ConsumeWithHeartbeat + ack/nak so the handler is a
// plain func(ctx, []byte) error — ack on nil, nak-with-delay on error, InProgress
// heartbeat holds a long-running unit past AckWait. The consumer never touches
// jetstream.Msg. Envelope decode stays above natsclient (payload registry
// layering); the []byte is the raw message data.
func (c *Client) ConsumeDurable(ctx context.Context, cfg StreamConsumerConfig, heartbeat time.Duration, handler func(context.Context, []byte) error) error
```

Implemented as `ConsumeStreamWithConfig(ctx, cfg, func(mctx, msg) { _ =
ConsumeWithHeartbeat(mctx, msg, heartbeat, func(wctx) error { return
handler(wctx, msg.Data()) }) })`. AckPolicy explicit; the wrapper owns ack/nak.

**Config cross-validation (B3):** `StreamConsumerConfig.Validate()` (or a
ConsumeDurable-time check) MUST reject `heartbeat >= AckWait` — a heartbeat that
first fires after AckWait already expired redelivers a live unit. Enforce
`heartbeat <= AckWait/2` (margin for the first tick). File the same gap against
`agentic-loop/config.go`'s `ConsumerConfig.Validate` as a sibling.

## gated-dag executor — durable publish + claim rollback

- **Provision the dispatch stream** at Start: `EnsureStream` on the dispatch
  subject, with a bounded retention (`MaxAge`/`MaxMsgs`) so an unconsumed backlog
  can't grow forever (a work stream, not the graph — ADR-068's no-TTL rule is
  about ENTITY_STATES, not request streams).
- **`natsPublisher.Dispatch`** switches `nc.Publish` → `PublishToStreamWithAck`,
  returning the error on a non-ack.
- **`claimThenDispatch` (executor.go:394-429)**: on a `Dispatch` error, **roll the
  claim back** — the same path the claim-error branch already takes
  (`executor.go:403-405`) — because a failed ack proves non-persistence, so the
  unit is safe to re-select next eval. Remove the "stranded until reset" comment;
  add a rollback + a `dispatch_publish_failures_total` metric tick.

## gated-dag executor — stall detector (residual observability)

`stallAfterInflight` (executor.go:435-448) currently returns nil the moment any
claimed unit is non-terminal (treats it as healthy in-flight), suppressing the
stall alert forever for a stranded unit. Change: a claimed, non-terminal,
non-dirtied unit whose claim timestamp (`claim.go:60`) is older than a configured
`stranded_after` threshold no longer suppresses the stall — it surfaces as a
stall/alert (edge-triggered publish as today). **Alert-only, never re-dispatch.**
Honest scope (ADR-070 B2): the threshold must be set above the max legitimate unit
runtime (a healthy long-running unit held by the consumer's heartbeat is
KV-indistinguishable from a stranded one); a false positive is a spurious alert,
not a double-run. `stranded_after: 0` disables it (back-compat).

## Shared decode helper (B5)

Add `gateddagexec.DecodeDispatch(data []byte) (*DispatchMessage, error)` in the
gated-dag payload package (BaseMessage → DispatchMessage). Both consumers import
it so the envelope unwrap is not reinvented per repo. Framework-side deliverable
even though the consumers live elsewhere.

## Config knobs (config.go)

`DispatchStream` (stream name), `DispatchDurable` (consumer name), `AckWait`,
`MaxAckPending`, `HeartbeatInterval`, `StrandedAfter`, stream retention. Sensible
defaults; `Validate()` enforces `HeartbeatInterval < AckWait` (B3) and
non-negative durations.

## Out of scope (this change)

Consumer wiring in semspec/semdragon; the CAS-UPGRADE claim exclusion; removing
the core-NATS publish path (kept until consumers migrate — non-breaking overlap).
