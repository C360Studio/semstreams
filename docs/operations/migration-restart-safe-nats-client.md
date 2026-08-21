# Migrate to direct one-shot lifecycle ownership

## Status and scope

The landed migration removes SemStreams lifecycle state that duplicated native Go and NATS ownership. It deletes
`internal/lifecyclejoin`, makes consumer acquisition return exact native handles, removes Client child catalogs and
name-routed lifecycle operations, and removes five inert consumer-deletion settings.

This guide does not claim a redesigned Client Connect/Close protocol, exact native `CLOSED` observation, async publish
settlement during Close, raw NATS-root retirement, or controlled/dirty process-restart proof. Those were considered
during the change but were not implemented and are not part of this migration.

## Native consumer ownership

The canonical port-backed operations return the exact native `jetstream.ConsumeContext`:

```go
func (c *Client) ConsumeStreamWithConfig(
    ctx context.Context,
    owner PortConsumerContext,
    cfg StreamConsumerConfig,
    handler func(context.Context, jetstream.Msg),
) (jetstream.ConsumeContext, error)

func (c *Client) ConsumeStreamWithConfigContexts(
    setupCtx context.Context,
    handlerCtx context.Context,
    owner PortConsumerContext,
    cfg StreamConsumerConfig,
    handler func(context.Context, jetstream.Msg),
) (jetstream.ConsumeContext, error)
```

All fallible stream, consumer, policy, and observation setup completes before `Consumer.Consume`. The caller retains
the returned handle and owns its shutdown. There is no SemStreams managed-consumer wrapper and no temporary
`*Handle` alias.

`ConsumeInternalStreamWithConfig` is limited to the named non-port framework consumers and also returns its exact
native handle.

## Durable consumer migration

`ConsumeDurable` is removed. Compose the stateless handler with the canonical handle-return operation:

```go
handler, err := natsclient.NewDurableHandler(cfg, heartbeat, work)
if err != nil {
    return err
}

consumeHandle, err := client.ConsumeStreamWithConfig(ctx, owner, cfg, handler)
if err != nil {
    return err
}
```

`NewDurableHandler` rejects nil work and nonpositive heartbeat. When `BackOff` is nonempty, every interval must be
positive and the minimum interval is the effective acknowledgement wait. Otherwise positive `AckWait` is effective,
with a 30-second default for a nonpositive value. Heartbeat equal to half that effective wait is valid; a larger value
is rejected.

The returned handler delegates InProgress and terminal settlement to `ConsumeWithHeartbeat`. Every nonnil handler
result remains operator-visible as a WARN with message `ConsumeDurable handler error` and fields `stream`, `consumer`,
and `error`.

## Removed Client lifecycle authority

The Client no longer owns consumer or subscription child catalogs and no longer exposes:

- `StopConsumer`;
- `StopAndDeleteConsumer`;
- `StopAllConsumers`; or
- `OutstandingWork`.

Client Close does not enumerate or stop component-owned children. Existing consumer-policy Prometheus metrics, graph
readiness, agent-loop inflight observation, and optional OTEL adapter observation remain separate from lifecycle
ownership.

`Subscription.Drain(context.Context)` behavior is unchanged by this migration.

## Duplicate durable identity

The existing Client-local `(stream, durable)` claim remains reject-only and handle-free. A second live local
acquisition fails without stopping, draining, deleting, or replacing the incumbent. Precommit failure and exact native
handle closure release the opaque claim.

The claim does not provide sealed pre-Start validation or owner labels and does not assert complete ADR-095
conformance.

## Configuration removal

The following components no longer expose `DeleteConsumerOnStop` or the generated
`delete_consumer_on_stop` schema property:

- OTEL exporter;
- agentic dispatch;
- agentic loop;
- agentic model; and
- agentic tools.

No production deletion mechanism replaces these inert settings. OTEL retains its existing strict unknown-field
behavior; the four agentic components retain their existing lenient JSON decoding.

Normal owner Stop and Client Close do not delete durable consumers. A test fixture that must delete topology owns the
exact identities it created and performs fixture-local teardown.

## Go lifecycle ownership

The inventoried production owners no longer depend on `internal/lifecyclejoin`. Each owner uses only the private
cancellation, native handles, and completion signals its resources require. Completed repeated Stop is a no-op. No
shared generation, retained-result, rejoin, or lifecycle state-machine API remains.

This migration does not impose one universal drain/cancel/join implementation on every owner. Each concrete owner is
responsible for canceling and joining the work it starts and for preserving its resource-specific settlement contract.

## Observability

Core health, Prometheus consumer-policy metrics, and structured `slog` behavior remain. Observation state has no Stop,
Drain, deletion, or Client-child authority. Unknown backlog remains an error rather than a fabricated zero.

OTEL remains an explicitly selected optional framework adapter. It consumes the existing observation seam but does
not define core lifecycle behavior.

## Downstream migration

SemStreams records migration surfaces but does not edit sister repositories.

Known old-signature port consumers exist in SemSpec, SemDev, and SemDragon. Known `ConsumeDurable` consumers exist in
SemMachina, SemSpec, and SemDragon. Those owners must compile their current checkout, retain the returned
`jetstream.ConsumeContext`, replace `ConsumeDurable` with `NewDurableHandler`, and remove Client-wide shutdown calls.

Generated schema copies containing `delete_consumer_on_stop` exist in SemStreams UI, SemSpec, SemTeams, and SemDragon.
Their owners regenerate or remove those copies and validate their products.

## Landed evidence

- `8da1b83a` deleted `internal/lifecyclejoin` with one insertion and 749 deletions.
- `c4fec3d3` landed exact handle ownership, the stateless durable handler, and Client child/catalog removal.
- `2e879304` removed the five configuration fields and schema properties.
- Focused race, natsclient real-NATS, graph-ingest, agent-loop, lint, build, schema, contract, core-E2E, and agentic-E2E
  evidence is recorded in the archived change ledger.

This evidence proves the migration described above. It does not prove the explicitly excluded Client redesign,
raw-root cleanup, or process-crash guarantees.
