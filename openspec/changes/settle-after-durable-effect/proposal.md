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
  exact consume handle drains. A private per-lane admission latch stops new local work; JetStream keeps
  delivery authority.
- **The lease is validated before acquisition.** Loop task/response/tool-result consumers default to heartbeat 15s
  and refuse, before allocating a consumer, any heartbeat above half the shortest positive BackOff. agentic-model
  defaults to AckWait 120s with heartbeat 60s under the same rule. Shipped fixtures are held to it.
- **`max_deliver` has a floor.** The fixed two-entry BackOff requires at least two deliveries; omitted or zero
  defaults to 2 and an explicit value below 2 is refused with a typed error naming observed and required.
- **An undecodable input is Terminate, not Ack.** A payload that cannot decode, or decodes to the wrong type, is a
  permanent defect: it is classified `PermanentDeliveryError` so the binding terminates it, instead of being
  acknowledged as if handled.
- **"This process does not hold that loop" is answered from the record, not from memory.** One classifier reads the
  loop record and reports stale (absent or terminal), live (non-terminal) or unknown (read failed); an input naming
  a stale loop is Acked and counted as an expected drop, and one naming a live loop is retried. It performs no
  recovery. The six call sites that previously Acked on a memory miss — uncorrelated model response, uncorrelated
  tool result, waiter-less governance verdict, and the cancel signal's not-found and already-terminal arms — use it.
- **A partially published result quarantines.** `persistHandlerResult`'s stamp phase is a whole-entity upsert and
  stays retryable; its publish phase is not, so a failure there is fatal-wrapped and settles as Quarantine rather
  than replaying publications whose PubAcks already returned.
- **Retry on the non-heartbeat lanes is bounded.** Those four lanes shipped with no consumer configuration, so
  `max_deliver: 0` meant unlimited and `SettleDelivery`'s Retry was an undelayed NAK. They now carry the same
  validated BackOff/`max_deliver` floor as the heartbeat lanes and settle Retry through
  `natsclient.SettleDeliveryWithRetry` with a 30s delay.

## Impact

- Affected capabilities: `agentic-loop`, `agentic-model`, `agentic-dispatch`, `agentic-governance`,
  `jetstream-consumer-policy`.
- Affected code: `natsclient/delivery_settlement.go`, `component/port_jetstream.go`, the four agentic processor
  components and their new private `delivery_owner.go` owners, `processor/agentic-loop/loop_presence.go`,
  `processor/agentic-loop/config.go`, `processor/agentic-model/config.go`, nine shipped `configs/**` fixtures,
  `schemas/agentic-loop.v1.json`.
- Exported surface: `natsclient.SettleDelivery` and `natsclient.SettleDeliveryWithRetry` are additions to a Tier 1
  package. `SettleDelivery`'s signature and immediate-retry behaviour are unchanged by the second entry point.
- New metric: `semstreams_agentic_loop_signals_dropped_total{reason}` counts cancel signals settled effect-free
  (`already_terminal`, `stale_loop_id`). The live-loop case is a Retry and is deliberately not counted as a drop.
- Operator-visible: the `agentic-loop` consumer schema changes its `heartbeat_interval` default from 60s to 15s
  and raises `max_deliver`'s minimum from 1 to 2. A deployment that pinned `max_deliver: 1` is refused at config
  validation with a typed error rather than silently running a single-delivery posture.

## Non-goals

- **No recovery logic.** Read-through reconstruction after process replacement, duplicate-proven-applied
  reconciliation, replay admission and waiter-less verdict recovery are a later layer and are deliberately absent.
- **No new persistent state or communication path.** No new bucket, subject, stream or public state. The health
  latch reuses the component's existing health surface and error count. The one new metric family and the one new
  `natsclient` function are named under Impact rather than hidden here.
- **No settlement change on agentic-model's request lane.** That callback still Acks before its response PubAck
  returns; it is the next layer's subject and is deliberately untouched here, so one component's settlement is not
  split across two changes.
- **No change to the heartbeat lanes' own settlement.** Task, response and tool-result keep the permanent typed
  heartbeat owner introduced by #759.
