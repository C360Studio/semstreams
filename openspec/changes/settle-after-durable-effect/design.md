# Design: settlement after durable effect

## The design this layer implements is not carried here

The accepted design for the whole restart-safety programme lives on the Codex integration branch at
`origin/codex/gh1146-agentic-loop-restart:openspec/changes/agentic-loop-restart-safety/design.md`, read at
`79b0f29f`. Its inventory, reconciliation and review records sit beside it in that directory. That directory is
deliberately not carried onto `main` by this layer: it is the programme's working evidence, several times the size
of this slice, and most of its decisions govern recovery work that lands later. This file records only the
decisions a reader of *this diff* needs, and cites that document for the rest.

## D1 — the decision point moves, the vocabulary does not

#759 established that a callback returns a `DeliveryDecision` and the binding performs the terminal method. This
layer changes only *when* the callback is entitled to return Ack: after the lane's durable transition or defined
refusal has committed and every required PubAck has returned. The four decisions keep their #759 meanings
(`natsclient/delivery_settlement.go`). Nothing in the wire format, the payload registry or the subject grammar
moves.

## D2 — `SettleDelivery` is the settlement half of the typed owner, with no work half

`ConsumeDeliveryWithHeartbeat` owns work, lease renewal, join and settlement together, which is right for a lane
whose work can outlive an ack interval. The ten non-heartbeat lanes do not need a lease; giving them one would put
a heartbeat goroutine behind every cancel signal. `SettleDelivery(msg, decision, cause)` is therefore the same
interpreter with the work half removed: it validates the closed tuple and attempts at most one terminal method, and
it explicitly does not invoke work, read payload or metadata, create a context, derive a deadline, send a heartbeat
or own a consumer. The caller joins its own work first. The alternative — an exported work-owning no-heartbeat
adapter — was implemented, measured and reverted on the integration branch (`bef0b372`…`16c31f89`); it nets to
zero code and is not carried.

## D3 — the fatal latch is per LANE, and JetStream keeps delivery authority

`deliveryLaneAdmission` is **one per lane**, not one per component: each `setupConsumer` call constructs its own
(`agentic-loop/component.go:1066`, `:1088`; `agentic-dispatch/component.go:547`, `:588`, `:618`, `:659`,
`:702`; `agentic-model/component.go:408`; `agentic-governance/component.go:509`). It is
a mutex-guarded bool plus a one-slot channel. On the first result whose `OwnerStopRequired()` is true it closes that
lane's admission, records the cause on the component's existing health surface exactly once, and wakes a per-binding
observer that drains that exact consume handle. Health is component-wide and the drain is lane-exact, so a fatal on
one lane leaves `Healthy=false` while the other lanes keep admitting work. That is deliberate — the process is
degraded, not dead, and an operator reads the condition — but it is the fact to know before reasoning about the
latch.

It is not a circuit breaker and not a retry policy: it prevents *new local work* after ownership control becomes
unsafe. Redelivery, backoff and max-delivery remain JetStream's. Later fatal results neither overwrite nor recount
the first cause, so health names the cause that actually broke the lane.

## D4 — the lease is validated, never repaired

Heartbeat and delivery-count validation happens before consumer allocation and returns a typed error naming the
observed values and the ceiling. It does not truncate BackOff, lower the heartbeat, or raise `max_deliver` to make
a bad configuration work — a silently repaired lease is the failure mode this requirement exists to stop. The
ceiling is observed from the consumer's own configuration (half the shortest positive BackOff, or half the
effective AckWait when BackOff is empty), not predicted by the caller.

## D5 — "this process does not hold that loop" is one question with one answer

Six call sites asked it in different words and all answered it the same wrong way: an input naming a loop absent
from process memory was a settled-drop, acknowledged and gone. Memory cannot distinguish a loop that finished from
one this process lost, and those settle in opposite directions. `classifyMissingLoop` reads the loop record and
answers stale / live / unknown; a failed read is never stale. It performs no recovery — no reconstruction, no
routing re-registration, no reads of retained requests — because making the live case actually recoverable is L4's
(#1330) subject. The two recovery grammars are `<loopID>:req:<n>` and `<loopID>:tool:<n>`, and the extractor
requires a framework-minted token so a provider-authored call ID is never used as a bucket key.

## D6 — `runWithBudget` is synchronous on purpose

It was a goroutine-plus-select; it now calls `fn(bctx)` directly and reports `bctx.Err() != nil`. The old shape
returned while the work goroutine was still live, so the callback could settle before its own graph write finished —
the exact thing this layer exists to stop. The cost is that the budget binds only a callee that honours
cancellation. That is not an assumption: graph dependencies are lifecycle participants under ADR-049, which
*requires* cancellation to be honoured, and a component that ignores it fails lifecycle review rather than being
defended against here. Declared residual below.

## D7 — a partial publish is Quarantine, not Retry

`persistHandlerResult` stamps the loop entity and then publishes. The stamp is a whole-entity upsert
(`component.go:2224`, `:2146`, `:2173` write the full record, not a delta), so replaying it is harmless. The publish
phase is not: it emits N results in a loop, and a failure on result k leaves 1..k-1 already durable on the stream
with no record of how far it got. Redelivering that callback republishes them. So the two phases now classify
differently — a pre-publish failure wraps to Retry as before, and a publish-phase failure wraps
`errs.WrapFatal(..., "published results have unknown durability")`, which the heartbeat work func maps to
`DeliveryDecisionQuarantine` **before** the `PermanentDeliveryError` and Retry arms. Quarantine terminates that
delivery, latches the owner's health fatal and drains the lane: an operator sees a stopped lane naming the cause
rather than a silent duplicate storm. That is deliberately the blunt answer. L4 (#1330) relaxes it to identity-based
replay — once each published result carries a deterministic identity, republication is idempotent and the publish
phase can go back to Retry.

## Not in this layer

`agentic-model`'s request lane still returns Ack from its callback before the response PubAck returns
(`processor/agentic-model/component.go:396-397` — `handleRequest` is called for effect and
`DeliveryDecisionAck` is returned unconditionally). That is the same defect class this change exists to fix, and it
is L2's (`af829616`) subject, not a gap here — L1 touches agentic-model only for its heartbeat lease floor and its
delivery-owner health latch, and this change's `specs/agentic-model/` delta is scoped to exactly those two. Landing
the request-lane half here would split one component's settlement across two changes.

## Declared cost

- The nine shipped `configs/**` fixtures are edited in lockstep with the new floor, and a test holds them to it.
  A downstream deployment carrying its own flow JSON with `heartbeat_interval: 60s` or `max_deliver: 1` is refused
  at config validation. That refusal is the point; the migration is a two-key edit and the error names both values.
- `agentic-model`'s `agent.request` port gains explicit `AckWait`/`HeartbeatInterval` defaults where it previously
  inherited the component default. This is a behaviour change for any deployment that relied on the inherited
  values, and is covered by `TestPostFoundationBAgenticModelHeartbeatPolicyAmendmentIsExact`.
- Process-replacement *recovery* remains unimplemented after this layer. The `agentic` E2E tier's recovery stages
  exercise it; their state on this head is recorded in the landing PR rather than predicted here. Every
  `loopPresenceLive` classification added here is a Retry that stays a Retry until L4 (#1330) gives the delivery
  somewhere to go; the bounded MaxDeliver and 30s BackOff are what keep that from being a hot loop in the meantime.
- `runWithBudget` bounds a cooperative callee only. A graph writer that ignores cancellation blocks past the budget
  and, on the 30s-AckWait non-heartbeat lanes, is redelivered while the first attempt still runs. The guard is
  ADR-049 lifecycle review, not code here. Measured only for the cooperative case
  (`TestRunWithBudgetWaitsForCooperativeWorkToJoinAfterCancellation`).
- `graphWriter.WriteLoopCompletion`, `WriteLoopFailure` and `WriteLoopCancellation` return nothing, so their own
  write failures are swallowed inside the writer and the callers can only observe `ctx.Err()` or a budget timeout as
  a proxy. The cancel path's "cancellation graph write has unknown durability" therefore fires on cancellation, not
  on a graph-gateway rejection. Making those three return an error is a signature change across the writer and three
  call sites; it is deliberately not in this layer's scope and is recorded here rather than filed, per the File
  ritual, because it is a residual of this layer's own boundary.
- `agentic-model` has no panic wrapper equivalent to `runLoopDeliveryWork` / `runDispatchDeliveryWork` /
  `runGovernanceDeliveryWork`, so a panic inside `handleRequest` still escapes to the NATS callback. Model request
  settlement is L2's subject (`af829616`), and the wrapper belongs with it rather than half-landed here.
- The delivery-owner trio (`deliveryLaneAdmission`, `newStreamConsumerBinding`, `observeDeliveryLane`) is now
  spelled identically in four components' own `delivery_owner.go` — governance gained one in this round, replacing
  the inline locals that made it a fifth spelling. Hoisting the trio into a shared internal package would move
  ~400 lines across four components and is far past the bound this round set for the refactor, so the duplication
  is recorded, not removed.
