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

`ConsumeDurable` and the temporary `NewDurableHandler` builder are removed without aliases. Validate the exact
consumer policy once, then compose the permanent typed callback with the canonical handle-return operation:

```go
policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
	ctx,
	cfg,
	heartbeat,
	natsclient.ImmediateDeliveryRetry(),
	func(
		workCtx context.Context,
		attempt natsclient.DeliveryAttempt,
		data []byte,
	) (natsclient.DeliveryDecision, error) {
		return doDurableWork(workCtx, attempt, data)
	},
)
if err != nil {
	return err
}

consumeHandle, err := client.ConsumeStreamWithConfig(
	ctx,
	owner,
	cfg,
	func(msgCtx context.Context, msg jetstream.Msg) {
		result := natsclient.ConsumeDeliveryWithHeartbeat(msgCtx, msg, policy)
		recordDeliveryResult(result)
	},
)
if err != nil {
	return err
}
```

`doDurableWork` and `recordDeliveryResult` are component-private placeholders, not framework APIs. A binding that
does not use attempt observation accepts and ignores `DeliveryAttempt` before delegating the unchanged bytes to its
transport-agnostic domain handler.

Pass the same `cfg` value to validation and acquisition. Validation rejects nil work, ended context, invalid retry
policy, nonpositive heartbeat, invalid acknowledgement timing, and heartbeat greater than half the effective
acknowledgement interval before acquisition. When `BackOff` is nonempty, its shortest positive interval is effective;
otherwise positive `AckWait` is effective, with a 30-second default for zero. Equality at half is valid.

The work callback defines its owner-specific durable consequence and returns ACK, Retry, Terminate, or Quarantine.
There is no universal nil-means-done contract. A component may ACK only after the durable consequence named by its
reviewed domain contract is committed; its replay path must consult that same authority before repeating effects.
`ConsumeDeliveryWithHeartbeat` owns payload extraction, `DeliveryAttempt` observation, InProgress, cancellation,
work join, and the one terminal settlement attempt. Inspect every `DeliveryResult`: preserve its semantic, heartbeat,
and settlement evidence in existing health/log surfaces. If `OwnerStopRequired` is true, close admission and stop the
exact retained consume handle outside the callback. A terminal-method error alone does not authorize owner shutdown.

The non-default #759 integration branch temporarily retains `ConsumeWithHeartbeat` only as a removal boundary while
#1146 migrates model and loop and #1249 migrates AgentRun. Its three-file zero-growth guard is not an API allowlist,
compatibility promise, current capability, or merge authority. No new integration may call it.

Final #759 conformance requires every production caller and the exported symbol to be absent without alias. The typed
surface and removal reach `main` together in one breaking pre-v1 cutover; no accepted default-branch interval exposes
both APIs. New and migrated integrations use only the typed API above.

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
`jetstream.ConsumeContext`, replace `ConsumeDurable` with the permanent typed policy/API composition above, define
their own durable done/replay matrix, and remove Client-wide shutdown calls. Gated-DAG's measured SemSpec and
SemDragon seams are recorded separately in
[Gated-DAG semantic-settlement migration](migration-gated-dag-semantic-settlement.md). A read-only 2026-08-29 scan of
active C360 sister checkouts found no `NewDurableHandler` adopters, so its removal requires no separate source
migration.

A fast consumer does not receive raw `jetstream.Msg` settlement authority or an exported no-heartbeat interpreter as
a migration shortcut. It uses an existing reviewed owner path or stops for a separate capability design.

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
