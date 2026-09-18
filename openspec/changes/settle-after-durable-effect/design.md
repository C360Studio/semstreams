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

## D3 — the fatal latch is private, and JetStream keeps delivery authority

`deliveryLaneAdmission` (one per component, in each component's `delivery_owner.go`) is a mutex-guarded bool plus a
one-slot channel. On the first result whose `OwnerStopRequired()` is true it closes admission, records the cause on
the component's existing health surface exactly once, and wakes a per-binding observer that drains the exact
consume handle. It is not a circuit breaker and not a retry policy: it prevents *new local work* after ownership
control becomes unsafe. Redelivery, backoff and max-delivery remain JetStream's. Later fatal results neither
overwrite nor recount the first cause, so health names the cause that actually broke the lane.

## D4 — the lease is validated, never repaired

Heartbeat and delivery-count validation happens before consumer allocation and returns a typed error naming the
observed values and the ceiling. It does not truncate BackOff, lower the heartbeat, or raise `max_deliver` to make
a bad configuration work — a silently repaired lease is the failure mode this requirement exists to stop. The
ceiling is observed from the consumer's own configuration (half the shortest positive BackOff, or half the
effective AckWait when BackOff is empty), not predicted by the caller.

## Declared cost

- The nine shipped `configs/**` fixtures are edited in lockstep with the new floor, and a test holds them to it.
  A downstream deployment carrying its own flow JSON with `heartbeat_interval: 60s` or `max_deliver: 1` is refused
  at config validation. That refusal is the point; the migration is a two-key edit and the error names both values.
- `agentic-model`'s `agent.request` port gains explicit `AckWait`/`HeartbeatInterval` defaults where it previously
  inherited the component default. This is a behaviour change for any deployment that relied on the inherited
  values, and is covered by `TestPostFoundationBAgenticModelHeartbeatPolicyAmendmentIsExact`.
- Process-replacement *recovery* remains unimplemented after this layer. The `agentic` E2E tier's recovery stages
  exercise it; their state on this head is recorded in the landing PR rather than predicted here.
