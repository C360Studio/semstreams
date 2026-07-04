# Gated-DAG durable dispatch (gh#385)

## Why

A gated-DAG unit that is **claimed but never marked terminal** — because its
dispatch was lost (fire-and-forget core-NATS, consumer not yet subscribed /
restarting) — **wedges its entire downstream subtree forever, silently, with no
auto-recovery and no stall alert**. `claimThenDispatch` already warns *"unit may
be stranded until reset."*

The **root cause** is the transport: gated-DAG dispatch is
`processor/gated-dag/publisher.go` `nc.Publish` — **the only dispatch path in the
framework that is not durable**. By the KV-or-stream heuristic a dispatch is a
"request to do something" → a resumable JetStream stream; and the framework's own
`for_each` path (ADR-046 Phase 1) already dispatches into agentic-loop's **durable
JetStream consumer**. The gated-DAG Phase-2 executor shipped a core-NATS shortcut
that skipped durability. A claim lease / timer-based re-dispatch would be a
band-aid over the wrong transport (and re-running a unit to recover is wasteful +
needs idempotency) — we fix the transport instead.

The substrate already exists in `natsclient`: `PublishToStreamWithAck`,
`EnsureStream`, `ConsumeStreamWithConfig`, and `ConsumeWithHeartbeat` (the
`InProgress` heartbeat that holds a long-running unit past `AckWait`). No raw
`js.Publish`/`jetstream.*` leaks into the gated-DAG component.

## What Changes

- **BREAKING — durable dispatch.** The executor publishes each dispatch via
  `natsclient.PublishToStreamWithAck` (ack-confirmed persisted) to a
  JetStream stream provisioned with `EnsureStream`, replacing core-NATS
  `nc.Publish`. A dispatch is now durably queued and delivered whenever the
  consumer (re)subscribes — fixing lost-dispatch deterministically. Consumer crash
  mid-work is covered by `AckWait` redelivery + `ConsumeWithHeartbeat`'s
  `InProgress` heartbeat.
- **Roll the claim back on a failed publish-ack (B1).** A failed
  `PublishToStreamWithAck` proves the message was not persisted, so the executor
  clears the claim (mirroring the existing claim-failure rollback) → auto-retry
  next eval, instead of stranded-until-reset. The durable ack is the information
  core-NATS lacked.
- **Enforce `heartbeat_interval < ack_wait` at config-load (B3).** Not merely
  documented (the agentic-loop precedent documents but doesn't validate it, so a
  misconfig silently redelivers a live unit → duplicate paid work). File the same
  gap against agentic-loop's config as a sibling.
- **New natsclient primitive — a typed durable-consume wrapper.**
  `ConsumeDurable(ctx, cfg, heartbeat, handler func(ctx, []byte) error)` composes
  `ConsumeStreamWithConfig` + `ConsumeWithHeartbeat` + ack/nak so a consumer's
  handler is `func(ctx, payload) error` and never touches `jetstream.Msg`. The
  framework owns the at-least-once pattern once; both gated-DAG consumers use it.
- **BREAKING — consumer contract.** semspec and semdragon migrate from a
  core-NATS subject subscription to the durable consumer (`ConsumeDurable`),
  **acking after the terminal marker lands**, and on redelivery
  **short-circuiting** if the terminal marker is already present (ack without
  re-running — B4). `ConsumeDurable` hands a `func(ctx, []byte) error`; the
  `BaseMessage → DispatchMessage` unwrap stays above natsclient (layering), so a
  **shared decode helper** in the gated-dag payload package (imported by both
  consumers) owns it once rather than each repo reinventing it (B5). We control
  both consumers, so the break is coordinated and one-time.
- **Stall detector (the residual).** Fix the `stallAfterInflight` blind spot so a
  claimed-too-long non-terminal unit surfaces as a stall/alert — covering the one
  failure the stream cannot: a terminal-marker write dropped *after* the consumer
  acked (largely a consumer marker-write-reliability concern; the mutation API is
  already `RequestWithRetryClassified`), and the consumer-down case.
- **Retire the lease idea** entirely (not implemented). The **claim marker stays**
  but narrows to executor-side dedup (don't re-publish each backstop pass); no
  lease semantics.

## Capabilities

### New Capabilities
- `gated-dag-dispatch` — the durable dispatch contract for the gated-DAG
  executor: durable publish-with-ack, the durable-consumer + ack-after-marker
  consumer contract, the heartbeat for long-running units, claim-based dedup, and
  the stranded-unit stall signal.

### Modified Capabilities
- None (no existing spec covers gated-dag or natsclient streaming yet).

## Impact

- `natsclient`: new `ConsumeDurable` wrapper (composes existing primitives; small
  surface add). No new raw-JS surface — publish/ack/heartbeat already exist.
- `processor/gated-dag/`: `publisher.go` (`PublishToStreamWithAck` + stream
  provisioning), `component.go` (stream/consumer wiring, config), `executor.go`
  (`stallAfterInflight` fix), `config.go` (stream/consumer knobs — durable name,
  `AckWait`, `MaxAckPending`, heartbeat interval), `metrics.go`.
- **Consumers (semspec, semdragon — both ours):** migrate to
  `natsclient.ConsumeDurable` + ack-after-marker. Cross-repo, coordinated.
- **Decision recorded as an ADR amending ADR-046** (the dispatch-contract change).

## Non-goals

- **Claim lease / timer-based re-dispatch** — rejected as a band-aid over the
  transport; the stream is the root fix.
- **CAS-based cross-instance claim exclusion** (`claim.go:46` CAS-UPGRADE POINT) —
  a separate concern (ADR-046 invariant #1), untouched.
- **The reset-driven manual recovery path** stays as the operator escape hatch.
- No change to the KV-watch-driven *input* side of the executor (unit markers);
  only the *dispatch output* transport changes.

## Consumers

`processor/gated-dag` (framework); semspec + semdragon (the two dispatch
consumers, both ours). The `ConsumeDurable` wrapper is generic natsclient
substrate reusable by any durable at-least-once consumer.
