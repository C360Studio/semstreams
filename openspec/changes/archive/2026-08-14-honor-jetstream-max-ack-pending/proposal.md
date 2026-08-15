# Honor JetStream max_ack_pending contracts

## Why

`JetStreamPort.max_ack_pending` is accepted and extracted, but most port-backed consumers do not carry it to NATS.
Some omit it, three agentic components silently replace it, bypass consumers do not use the extractor, and output ports
accept a consumer-only field. The effective policy is also absent from SemStreams observability.

## What Changes

- Ordinary JetStream input consumers honor positive, zero, and `-1` declarations through one observed natsclient path.
- Agentic loop/model/tools retain component-owned policies and reject every nonzero declaration.
- JetStream outputs reject nonzero `max_ack_pending`; zero remains omission.
- Port-backed consumer APIs require bounded component/port context; non-port consumption is explicit.
- The ambiguous legacy `Client.ConsumeStream` API is removed.
- Requested, effective, and observation-availability gauges report configuration truth with bounded identity labels.
- Generated schemas consume canonical port-field minimum and direction metadata.
- Operational documentation distinguishes NATS delivery admission from component concurrency.

## Impact

This is a configuration tightening: previously accepted nonzero declarations on outputs and component-owned agentic
inputs now fail. It is also a Go source break for the three port-backed consumption signatures and removal of
`Client.ConsumeStream`. Adopters migrate by supplying `PortConsumerContext`; no duplicate stream, consumer, or policy
identity is required. Zero-config behavior and all fixed agentic values remain unchanged.

No generic nonzero default, queue/drop metric, or issue #309 flow-port metric is introduced.

