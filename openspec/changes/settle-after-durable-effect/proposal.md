# Change: settlement after durable effect

## Why

`#759` gave JetStream delivery a semantic vocabulary: a callback returns a `DeliveryDecision` and the binding
performs the terminal method. It did not change *when* the agentic components reach that decision. Ten
non-heartbeat durable callbacks across agentic-loop, agentic-dispatch and agentic-governance still returned
success from the point where their work was *started* rather than the point where its durable effect was
*committed*. A KV transition that never landed, a required publication that never received PubAck, a graph write
still in flight behind a cancelled context — each of those was an ACK, and the message was gone.

The same layer closes the other half of the same seam. A delivery owner that loses ownership control (unavailable
delivery metadata, a panicked handler, a semantic identity collision) reported nothing to the component's health
surface, so a process could keep answering `Healthy=true` while no lane could settle anything. And the shipped
long-running consumer defaults were internally inconsistent: heartbeat 60s against a BackOff whose shortest
interval is 30s means the lease expires mid-work and the message is redelivered while the first attempt is still
running, and `max_deliver: 1` silently truncated a two-entry BackOff to a single-delivery posture.

## What changes

- **Settlement follows the effect.** Every non-heartbeat durable callback in agentic-loop (cancel signal, approval
  response, approved verdict, rejected verdict), agentic-dispatch (`user.message`, `agent.created`,
  `agent.approval_pending`) and agentic-governance (task, request, response validation) joins its delivery-derived
  work and then passes a decision/cause tuple to the new `natsclient.SettleDelivery`. Decode, correlation, KV,
  Store, transition and required-publication failures can no longer become a positive acknowledgement.
- **One shared interpreter.** `natsclient.SettleDelivery` validates the closed decision/error tuple and attempts at
  most one terminal method. It owns no work, no context, no heartbeat and no consumer lifecycle.
- **A fatal delivery result latches health.** The first owner-fatal result in any lane synchronously sets
  `Healthy=false`, status `delivery ownership lost` and the exact cause in `LastError`, exactly once, before the
  exact consume handle drains. A private per-component admission latch stops new local work; JetStream keeps
  delivery authority.
- **The lease is validated before acquisition.** Loop task/response/tool-result consumers default to heartbeat 15s
  and refuse, before allocating a consumer, any heartbeat above half the shortest positive BackOff. agentic-model
  defaults to AckWait 120s with heartbeat 60s under the same rule. Shipped fixtures are held to it.
- **`max_deliver` has a floor.** The fixed two-entry BackOff requires at least two deliveries; omitted or zero
  defaults to 2 and an explicit value below 2 is refused with a typed error naming observed and required.

## Impact

- Affected capabilities: `agentic-loop`, `agentic-model`, `agentic-dispatch`, `agentic-governance`,
  `jetstream-consumer-policy`.
- Affected code: `natsclient/delivery_settlement.go`, `component/port_jetstream.go`, the four agentic processor
  components and their new private `delivery_owner.go` owners, `processor/agentic-loop/config.go`,
  `processor/agentic-model/config.go`, nine shipped `configs/**` fixtures, `schemas/agentic-loop.v1.json`.
- Operator-visible: the `agentic-loop` consumer schema changes its `heartbeat_interval` default from 60s to 15s
  and raises `max_deliver`'s minimum from 1 to 2. A deployment that pinned `max_deliver: 1` is refused at config
  validation with a typed error rather than silently running a single-delivery posture.

## Non-goals

- **No recovery logic.** Read-through reconstruction after process replacement, duplicate-proven-applied
  reconciliation, replay admission and waiter-less verdict recovery are a later layer and are deliberately absent.
- **No new persistent state.** No new bucket, subject, metric family, exported public state or communication path.
  The health latch reuses the component's existing health surface and error count.
- **No change to the heartbeat lanes' own settlement.** Task, response and tool-result keep the permanent typed
  heartbeat owner introduced by #759.
