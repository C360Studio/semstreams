# Design: observed JetStream acknowledgement admission

## Context

The existing port declaration, facts projection, `ConsumerConfig`, and final nats.go carrier already represent
`MaxAckPending`. The defect is ownership and application after extraction. NATS remains authoritative for the effective
value and may inherit, default, cap, or reject a request.

## Decisions

1. Zero remains omission. Ordinary inputs forward positive and `-1` exactly; agentic component-owned inputs reject
   every nonzero value; outputs reject every nonzero value.
2. `PortFieldInfo` and the closed port binding table own minimum `-1` and input-only direction. Runtime validation,
   discovery, and checked-in schema generation consume that metadata.
3. Managed consumers require `PortConsumerContext{Component, Port, ComponentOwned}`. Requested policy comes from the
   exact final config; stream, consumer, and effective policy come from `ConsumerInfo`.
4. Initial observation succeeds before delivery. Nonzero mismatch is invalid; zero accepts the observed NATS value.
   NATS API errors 10121 and 10082 are invalid configuration; transport and availability failures remain transient.
5. Natsclient alone owns three policy gauges. Direct OTEL creation delegates observation to natsclient and receives an
   opaque cleanup closure.
6. Replacement, stop, deletion, client close, and OTEL stop delete old series. Refresh failure deletes effective truth,
   retains requested truth, sets availability to zero, and warns only on transition.
7. Durable policy updates use `CreateOrUpdateConsumer`; SemStreams does not delete durable state to change this field.

## Adopter seam

An external component author must choose only whether their input is ordinary or component-owned. Ordinary callers copy
the extracted value into the final config and provide component/port context. Component-owned callers reject nonzero
declarations and set `ComponentOwned`. If an adopter does nothing at the configuration surface, NATS or the component
retains its existing zero/default behavior. Generated schemas and startup errors tell operators where ownership lies.

## Risks and mitigations

- Exported Go signatures break at compile time rather than preserving an unobserved compatibility bypass.
- Metrics use only configured component, port, stream, consumer, and a three-value source label.
- No runtime prediction of server policy occurs; the framework acts, observes `ConsumerInfo`, and reports the result.
- Integration tests use server state/readiness, not arbitrary sleeps.

