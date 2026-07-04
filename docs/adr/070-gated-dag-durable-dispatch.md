# ADR-070: Gated-DAG dispatch is durable at-least-once (amends ADR-046)

## Status

**Proposed — 2026-07-04.** Amends ADR-046 (parallel fan-out & gated-DAG
dispatch). Scopes gh#385. Cross-repo contract decision: changes how the two
gated-DAG dispatch consumers (semspec, semdragon — both ours) receive dispatches.
Mechanics live in the `gated-dag-dispatch` spec
(`openspec/changes/gated-dag-durable-dispatch/`); this ADR records the decision.

## Context

The gated-DAG executor dispatches a unit by publishing a reference envelope via
core-NATS `nc.Publish` (`processor/gated-dag/publisher.go`) — **fire-and-forget,
the only dispatch path in the framework that is not durable**. A durable claim
marker is written *before* the publish (ADR-046 invariant #2), and the eval-loop
dedup then skips a claimed unit on every backstop pass unless it goes terminal or
is reset.

The failure (gh#385): a dispatch lost because the consumer was not subscribed
(restarting, not-yet-started) leaves the unit claimed-but-never-terminal — its
downstream subtree wedges **forever**, with no auto-recovery and (because
`stallAfterInflight` treats any claimed non-terminal unit as healthy in-flight)
no stall alert. The gh#373 "flake" was this in miniature.

Two observations make the fix clear:

1. **The transport is the root cause.** By the KV-or-stream heuristic a dispatch
   is a *request to do something* → a resumable JetStream stream. ADR-046's own
   `for_each` path (Phase 1) already dispatches into agentic-loop's **durable
   JetStream consumer** ("operators set the cap on the JetStream consumer"). The
   gated-DAG Phase-2 executor shipped a core-NATS shortcut that skipped
   durability. Core-NATS is for loss-tolerant fan-out; a work-unit dispatch whose
   loss wedges a subtree is not loss-tolerant.
2. **The substrate already exists in `natsclient`** — `PublishToStreamWithAck`,
   `EnsureStream`, `ConsumeStreamWithConfig`, and `ConsumeWithHeartbeat` (the
   `InProgress` heartbeat that holds a long-running unit past `AckWait`). The fix
   needs no raw `js.Publish`/`jetstream.*` in the component; the one gap is
   consume ergonomics (see Decision 2).

## Decision

1. **Gated-DAG dispatch is durable at-least-once over a JetStream stream.** The
   executor publishes each dispatch via `natsclient.PublishToStreamWithAck`
   (ack-confirmed persisted) to a stream provisioned with `EnsureStream`,
   replacing core-NATS `nc.Publish`. A dispatch is durably queued and delivered
   whenever the consumer (re)subscribes; consumer crash mid-work is covered by
   `AckWait` redelivery + the `InProgress` heartbeat. This is deterministic — no
   claim lease / re-dispatch timer to tune.

2. **The at-least-once consumer pattern is a framework primitive, not per-consumer
   glue.** natsclient gains a typed durable-consume wrapper —
   `ConsumeDurable(ctx, cfg, heartbeat, handler func(ctx, []byte) error)` —
   composing `ConsumeStreamWithConfig` + `ConsumeWithHeartbeat` + ack/nak so a
   consumer's handler is `func(ctx, payload) error` and never touches
   `jetstream.Msg`. The framework owns the ack/heartbeat/redelivery semantics
   once; both gated-DAG consumers use it.

3. **The consumer contract is: ack after the terminal marker lands.** A dispatch
   is complete only when the unit's terminal marker is durably written; the
   consumer acks then. Crash before ack → JetStream redelivers. Consumers MUST be
   idempotent for the (rare) redelivery of an in-flight unit — the heartbeat keeps
   this rare (a genuinely-running unit is not redelivered).

4. **A dropped terminal-marker write AFTER ack is a residual the stream cannot
   fix** — the consumer must write the marker reliably (the mutation API is
   already `RequestWithRetryClassified`), and a **stranded-unit stall detector**
   (fix the `stallAfterInflight` blind spot) surfaces the residual + the
   consumer-down case for operators. This is observability, not auto-re-dispatch.

5. **Rejected: a claim lease / timer-based re-dispatch.** It is a band-aid over
   the lossy transport; the stream makes it unnecessary, it re-runs work
   (wasteful, idempotency-demanding), and it has strictly worse long-running-unit
   semantics than the consumer-controlled `InProgress` heartbeat. Building it
   would be churn — retired before it shipped.

The claim marker stays (executor-side dedup — don't re-publish each backstop
pass); its role narrows, no lease semantics. The `CAS-UPGRADE POINT`
(`claim.go:46`, cross-instance exclusion) is untouched.

## Consequences

### Positive

- Lost-dispatch strand is eliminated at the transport, deterministically.
- gated-DAG dispatch becomes consistent with the framework's own durable-dispatch
  pattern (agentic-loop / `for_each`) rather than the lone core-NATS deviation.
- The `ConsumeDurable` wrapper is reusable substrate for any durable at-least-once
  consumer; both gated-DAG consumers drop their hand-rolled glue.
- No lease-TTL tuning; long-running units held by the consumer's heartbeat.

### Negative / cost

- **Breaking**: both consumers migrate from a core-NATS subject to the durable
  consumer + ack-after-marker. Coordinated one-time change (we own both repos).
- New stream + durable consumer to provision and observe (but the framework
  already runs JetStream streams; `cmd/semstreams` provisions them via
  `EnsureStreams`).
- Consumers must be idempotent for redelivery (documented contract).

### Risks

- **AckWait vs. long units** — mitigated by `ConsumeWithHeartbeat` (`InProgress`
  extends the window while work runs); set `AckWait` above the heartbeat interval.
- **Residual dropped-marker-after-ack** — surfaced by the stall detector, not
  silently wedged; consumers use the retrying mutation client for marker writes.

## Migration

Framework lands first (stream + `PublishToStreamWithAck` publisher +
`ConsumeDurable` + stall detector), tag; then semspec and semdragon bump and
switch their dispatch consumers to `ConsumeDurable` + ack-after-marker in a
coordinated pass. The old core-NATS subject publish is removed only after both
consumers migrate (or behind a brief transitional config if needed).

## References

- Amends ADR-046 (gated-DAG dispatch); gh#385 (the strand), gh#373 (the flake
  that hid it).
- `/kv-or-stream` heuristic (request → resumable stream);
  `docs/concepts/03-streams-vs-kv-watches.md`.
- natsclient: `PublishToStreamWithAck`, `EnsureStream`, `ConsumeStreamWithConfig`,
  `ConsumeWithHeartbeat` (the substrate); `processor/gated-dag/publisher.go`
  (the core-NATS deviation), `executor.go` `stallAfterInflight` (the blind spot).
