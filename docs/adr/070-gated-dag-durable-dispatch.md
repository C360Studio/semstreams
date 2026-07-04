# ADR-070: Gated-DAG dispatch is durable at-least-once (amends ADR-046)

## Status

**Accepted — 2026-07-04.** Amends ADR-046 (parallel fan-out & gated-DAG
dispatch). Framework side implemented first (durable publish + `ConsumeDurable` +
stall detector); the coordinated consumer migration (semspec, semdragon) follows
on a framework tag. Scopes gh#385. Cross-repo contract decision: changes how the two
gated-DAG dispatch consumers (semspec, semdragon — both ours) receive dispatches.
Mechanics live in the `gated-dag-dispatch` spec
(`openspec/changes/gated-dag-durable-dispatch/`); this ADR records the decision.

A pre-Accept adversarial review confirmed the core decision (transport is the
root cause; the natsclient substrate exists; the lease rejection is defensible)
and sharpened four points folded in below: (B1) the durable ack lets us roll the
claim back on a *failed* publish — an auto-recovery core-NATS could not offer;
(B2) the retained stall detector is a soft alert-only threshold, not the
zero-tuning it was first framed as, and the stream — not the detector — now
covers consumer-down; (B3) `heartbeat_interval < ack_wait` must be *enforced*;
(B4) idempotency = terminal-marker short-circuit.

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
   `AckWait` redelivery + the `InProgress` heartbeat.

   **Roll the claim back on a failed publish-ack (B1).** The claim is committed
   before the publish (ADR-046 invariant #2). Under core-NATS, publish could not
   fail-visibly, so the claim was left in place on error ("unit stranded until
   reset", `executor.go:420`) — rolling back risked a double-run because you
   could not know whether a consumer had already received it. With
   `PublishToStreamWithAck`, a **failed** ack is proof the message was **not
   persisted** and will **not** be delivered — so the executor MUST clear the
   claim on publish error (mirroring the existing claim-failure rollback,
   `executor.go:403-405`). This converts "publish failed → stranded until manual
   reset" into "publish failed → auto-retried next eval." The durable ack is
   exactly the information the fire-and-forget path lacked; using it is a
   first-class part of this decision, not an afterthought.

2. **The at-least-once consumer pattern is a framework primitive, not per-consumer
   glue.** natsclient gains a typed durable-consume wrapper —
   `ConsumeDurable(ctx, cfg, heartbeat, handler func(ctx, []byte) error)` —
   composing `ConsumeStreamWithConfig` + `ConsumeWithHeartbeat` + ack/nak so a
   consumer's handler is `func(ctx, payload) error` and never touches
   `jetstream.Msg`. The framework owns the ack/heartbeat/redelivery semantics
   once; both gated-DAG consumers use it.

3. **The consumer contract is: ack after the terminal marker lands, and
   short-circuit on redelivery (B4).** A dispatch is complete only when the unit's
   terminal marker is durably written; the consumer acks then. Crash after
   marker-write but before ack → JetStream redelivers an *already-terminal* unit.
   Idempotency here is a **terminal-marker short-circuit**: on any delivery the
   consumer FIRST checks whether the unit's terminal marker is already present and,
   if so, **acks without re-running** — it does NOT re-run and rely on
   replace-by-predicate (re-running an expensive agent unit for nothing is the cost
   we are avoiding). The marker write uses the retrying mutation client
   (`RequestWithRetryClassified`) so a dropped marker is rare.

4. **After durable dispatch, the only residual strand is a marker dropped AFTER
   ack** — consumer-down is now handled by the stream (the message waits until the
   consumer returns), and publish-failure by Decision 1's rollback. A
   **stranded-unit detector** (fix the `stallAfterInflight` blind spot, which today
   reads any claimed non-terminal unit as healthy in-flight) surfaces that residual
   plus any unknown wedge — as an **alert only**, never auto-re-dispatch. Honest
   caveat (B2/B6): from KV state alone a *stranded* unit (claimed, no marker) is
   indistinguishable from a *healthy long-running* one (claimed, no marker yet,
   held alive by the consumer's `InProgress` heartbeat), so a wall-clock "claimed
   too long" threshold is a **soft tuning knob** (set above max unit runtime) — not
   the zero-tuning the lease-rejection first implied. The difference from the lease
   is that a false positive is a spurious alert, not a double-run. A precise,
   tuning-free alternative — cross-checking the JetStream consumer's per-unit
   in-flight/pending state instead of a wall clock — is left to the spec/design
   phase to evaluate.

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
- Publish-failure and consumer-down become auto-recovering (Decision 1 rollback +
  the stream holding the message), not reset-driven.
- The `ConsumeDurable` wrapper owns the **ack/heartbeat/redelivery** pattern once
  for any durable at-least-once consumer. It does NOT own **envelope decode**
  (B5): natsclient deliberately does not import the payload registry (a layering
  inversion), so the handler is `func(ctx, []byte) error` and the
  `BaseMessage → DispatchMessage` unwrap stays above natsclient. To avoid each
  consumer reinventing that unwrap, provide a small shared decode helper in the
  gated-dag payload package that both consumers import.
- No auto-re-dispatch timer (the lease). The stall detector's alert threshold is a
  softer, alert-only knob (B2), not zero tuning; long-running units are held by
  the consumer's `InProgress` heartbeat, not a guessed window.

### Negative / cost

- **Breaking**: both consumers migrate from a core-NATS subject to the durable
  consumer + ack-after-marker. Coordinated one-time change (we own both repos).
- New stream + durable consumer to provision and observe (but the framework
  already runs JetStream streams; `cmd/semstreams` provisions them via
  `EnsureStreams`).
- Consumers must be idempotent for redelivery (documented contract).

### Risks

- **Misconfigured heartbeat vs. AckWait → double-dispatch of a live unit (B3).**
  If `heartbeat_interval ≥ ack_wait`, the first `InProgress` lands *after* AckWait
  has already expired, so JetStream redelivers a genuinely-running unit → duplicate
  (expensive) work. The agentic-loop precedent DOCUMENTS this invariant
  (`agentic-loop/config.go:169`) but its `Validate()` does NOT enforce it (checks
  the two bounds independently). The gated-dag config MUST **cross-validate
  `heartbeat_interval < ack_wait`** (with margin) at load time, not merely document
  it. (File the same enforcement gap against agentic-loop's config as a sibling.)
- **Residual dropped-marker-after-ack** — surfaced by the stranded-unit detector
  (alert), not silently wedged; consumers use the retrying mutation client for
  marker writes and the terminal-marker short-circuit on redelivery.
- **Stall detector false-positive on a healthy long-running unit** — see Decision
  4: alert-only, so the cost is a spurious alert; threshold set above max unit
  runtime, or replaced by stream-state introspection in the design phase.

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
