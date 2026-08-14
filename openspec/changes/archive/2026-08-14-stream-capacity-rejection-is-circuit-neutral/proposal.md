# Stream capacity rejection is circuit-neutral

## Why

A JetStream `DiscardNew` capacity refusal is an observed statement about one stream's configured storage ceiling,
not evidence that the NATS connection is unhealthy. Counting repeated capacity refusals against the shared client
circuit currently opens it and blocks unrelated healthy streams.

## What Changes

- Keep exactly the server's typed `10077` maximum-bytes, maximum-messages, and maximum-messages-per-subject PubAck
  refusals neutral to circuit accounting across synchronous, acknowledged, asynchronous, and batch publish paths.
- Preserve the existing returned errors, async futures, batch aggregation, metrics, logs, and successful-enqueue reset.
- Continue counting every other publish failure, including other or unknown `10077` descriptions.

## Non-goals

- No admission prediction, retry policy, stream provisioning, configuration, schema, metric, or exported API change.
- No general payload-size handling or changes to issue #857 or #738.
