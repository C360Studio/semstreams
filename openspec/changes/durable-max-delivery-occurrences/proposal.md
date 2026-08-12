# Change: Durably expose MaxDeliver exhaustion occurrences

## Why

A JetStream consumer that reaches finite `MaxDeliver` stops receiving that message. The server emits a
`$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>` advisory at the transition, but SemStreams previously had no durable
subscriber or operator metric for it. A Core NATS subscriber would lose exactly the incidents that occur while the
framework is down. Consumer counters and `AckFloor` cannot reconstruct the occurrence: an exhausted delivery can
leave the pending counters while a later acknowledgement advances the floor.

This closes GitHub #742 as occurrence visibility only. It does not infer a current parked set, redrive messages,
alter application consumers' retry policy, or turn readiness into a claim that every accepted message applied.

## What changes

- Provision one framework-owned `MAX_DELIVERY_EVENTS` JetStream stream before component consumers start. It captures
  exactly `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>`, with file storage, LimitsPolicy, DiscardOld, seven-day
  retention, a 64 MiB byte ceiling, and one replica.
- Bind one fixed durable observer identity across SemStreams replicas. It resumes unacknowledged occurrences after
  restart and uses unlimited delivery attempts so observer failure cannot generate recursive MaxDeliver exhaustion.
- Decode the NATS typed event strictly. A valid event increments a bounded-label Prometheus counter and emits a
  structured ERROR log before acknowledgement. Malformed, wrong-type, incomplete, or subject-mismatched poison
  events emit decoder-error telemetry and are acknowledged.
- Wire both production assemblies after capture-stream provisioning and before `Manager.StartAll`.
- Extend the disposable core E2E to exhaust the shipped ObjectStore write durable through test-side NATS
  administration and observe both retained evidence and the operator metric.

## Impact

- Affected current capability: `stream-provisioning`.
- New capability delta: `max-delivery-observability`.
- Runtime code: `internal/maxdelivery`, `cmd/semstreams`, `cmd/e2e-semstreams`.
- Test-only assembled administration temporarily updates the shipped durable to MaxDeliver=1 before fault injection.
- No component-author configuration, public framework package, readiness state, message redrive API, or
  consumer-policy change. Operators using restrictive NATS authorization must add the documented stream-management,
  durable-consumer, inbox, and ACK permissions before upgrading. Provisioning/binding denial fails boot; an ACK/NAK
  settlement error after binding emits a bounded counter and ERROR while the durable delivery remains pending.
