# Change: Simplify one-shot lifecycle ownership

## Why

SemStreams had duplicated native Go and NATS ownership with shared generation/result helpers, Client child catalogs,
name-routed lifecycle operations, and deletion configuration. The landed refactor removed that duplicate framework
state and left ownership with the code that acquires each resource.

## What Changes

- Migrate the inventoried production owners from `internal/lifecyclejoin` to private cancel/native handle/done or
  WaitGroup ownership.
- Delete `internal/lifecyclejoin`.
- Make canonical port-backed and internal consume operations return exact `jetstream.ConsumeContext` handles.
- Replace `ConsumeDurable` with stateless `NewDurableHandler`.
- Remove Client consumer/subscription child catalogs, name-routed Stop/delete APIs, `StopAllConsumers`,
  `OutstandingWork`, and Close-time child enumeration.
- Preserve the independent Client-local duplicate claim and consumer-policy/Prometheus observation.
- Remove five `DeleteConsumerOnStop` fields and published-schema properties without a replacement deletion mechanism.
- Preserve existing `Subscription.Drain` behavior.
- Preserve core health, Prometheus, and structured `slog` behavior; OTEL remains optional-adapter compatibility.

## Removed From Completion Scope

- Raw NATS root narrowing.
- Exact Client transport-close, reconnect, native-CLOSED, callback, and worker-join redesign.
- Controlled/dirty restart, settlement-wide, external-effect, and process/NATS kill proof.
- Graph-ingest semantic rewrites.
- Stronger sealed duplicate-owner admission and Subscription redesign.
- Registry/PR #990, Flow, Fusion, classifier, logging redesign, and new issue scope.

## Modified Capabilities

- `jetstream-consumer-policy`
- `service-shutdown`

## Impact

Port and durable adopters receive compiler-visible API breaks and retain native handles directly. Configuration authors
remove `delete_consumer_on_stop`. SemStreams records downstream impact but does not edit sister repositories.
