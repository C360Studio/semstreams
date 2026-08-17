# Migrating to owner-local restart-safe NATS lifecycle

## Status and scope

This guide records the approved pre-v1 target contract. The contract reset does not claim the runtime migration or
restart proofs are complete. Do not treat the new signatures or shutdown result as available until their ordered PR
lands and its checks pass.

The migration replaces Client-wide child discovery with ordinary Go ownership: the component or service that starts a
subscription or managed consumer retains its exact returned handle and drains it during `Stop(ctx)`. Composition owns
one synchronous Connect, aggregates all owner Stop results, invokes one terminal transport-only Close, and exits the
process. Clean versus failed changes exit status and observability only; supervision always creates the next process
and Client.

Boot-only service/component composition, the dedicated rule-definition hot-reload exception, and dirty-power recovery
remain unchanged. Power loss runs no cleanup and recovers from durable JetStream/KV plus boot reconciliation.

## Required owner migration

The retained consume constructors change from error-only setup to exact-handle returns:

```go
func (c *Client) ConsumeStreamWithConfig(...) (*ManagedConsumer, error)
func (c *Client) ConsumeStreamWithConfigContexts(...) (*ManagedConsumer, error)
func (c *Client) ConsumeInternalStreamWithConfig(...) (*ManagedConsumer, error)
```

Every caller must retain the returned handle in the owner that started it. During `Stop(ctx)`, that owner must:

1. stop admitting new work;
2. invoke `Drain(ctx)` on each exact core `Subscription` and `ManagedConsumer`;
3. wait for authoritative native closure while accepted callback authority remains live;
4. commit required effects/publications before ACK, or leave unfinished durable work eligible for redelivery; and
5. cancel and join remaining Start-owned work only after drain completion.

### Compiler-directed outstanding-work migration

Outstanding-work observation moves with ownership. Replace
`Client.OutstandingWork(ctx, streamName, consumerName)` with
`managedConsumer.OutstandingWork(ctx)` on the exact handle returned during setup. There is no compatibility alias or
name-based fallback.

The in-repository compiler-directed migration has exactly three production callsites:

- `processor/graph-ingest/readiness.go` has two calls; both move to the graph-ingest-owned consumer handles.
- `processor/agentic-loop/inflight.go` has one call; it moves to the agentic-loop-owned consumer handle.

After the owner-handle PR, an unresolved `Client.OutstandingWork` compile error means the caller failed to retain its
resource. Fix ownership at setup and carry the handle to the query; do not reconstruct stream/durable identity or add
a Client helper.

Graceful Stop must not use managed-consumer `Stop` or core-subscription `Unsubscribe`. Those abrupt primitives can
discard buffered delivery and cannot contribute to a clean controlled shutdown. `Subscription` exposes Drain only;
there is no exported Abort or Unsubscribe lifecycle escape.

`ConsumeDurable` is retired because its measured production consumer census is zero. Durable adopters use one of the
retained exact-handle constructors with the existing heartbeat and settlement primitives. ADR-070 remains unchanged
historical context for durable gated-DAG dispatch, ack-after-terminal-marker, and redelivery semantics; retiring an
unused convenience helper does not rewrite that decision.

## Exact graceful deletion

`ManagedConsumer.DrainAndDelete(ctx)` is the only graceful durable-consumer deletion operation. The handle privately
binds deletion to the exact stream/durable identity acquired at construction.

- Drain must reach exact Closed before deletion starts; a drain deadline prevents deletion.
- A later caller may rejoin the same drain.
- Concurrent or repeated callers issue at most one bound deletion and observe one retained terminal result.
- Consumer-not-found after drain is benign success.
- Every other deletion failure, including an ambiguous deadline, remains non-clean and is not retried in process.
- Failed partial setup publishes no handle and cleans only its exact partial acquisition.
- No Client name lookup, catalog, or public administrative delete can substitute for the exact handle.

These rules fence partial acquisition, duplicate cleanup, and stale ownership from deleting another owner's durable.

## Terminal Client migration

Client Close owns transport only. It does not enumerate, drain, abort, delete, or compensate for subscriptions or
managed consumers. Before calling it, composition must have received every owner Stop result.

Close rejects later Client work, cancels and joins only Client-owned health and metrics workers, registers CLOSED
observation, initiates native connection Drain, observes CLOSED, clears native authority, and retains one result for
repeated callers. The result is deliberately conservative:

- an installed connection already closed before Close is failed even when native `LastError()` is nil;
- any historical or terminal non-nil `LastError()` makes Close non-clean;
- caller deadline may force native Close, but the retained result remains failed; and
- no detached cleanup or later caller can convert a failed boundary into clean.

Connect is synchronous and installs private `nats.FlusherTimeout(5*time.Second)`. There is no option, config, or
environment knob. A blocked native write or flush therefore fails visibly within the framework ceiling rather than
requiring an adopter to predict it.

## Retired and narrowed native roots

The authoritative per-symbol disposition is
`openspec/changes/require-restart-for-config-activation/native-surface-inventory.md`, preserved exactly from the
owner-approved artifact with SHA-256
`d79df592e7049d4f0e3412bf41e8c61d44ea0829a6fddc2734cff40ceb966617`.

Before the breaking tag, Client and framework constructors stop returning broad mutable ownership roots:

- raw `*nats.Conn`;
- `jetstream.JetStream`;
- `jetstream.Stream`;
- `jetstream.KeyValue`;
- `jetstream.ObjectStore`; and
- equivalent capabilities that permit unbounded work outside Client lifetime.

Broad injected roots narrow to their measured local method interfaces. Reviewed NATS message, config, value, watcher,
lister, and future seams may remain when caller context bounds acquisition/operation and local Stop or completion
ownership is explicit. There is no `Unsafe*` alias or compatibility shim. A missing narrow operation must receive
separate surface review; it does not justify restoring a raw root.

Sister repositories are read-only to this change. Their owners migrate compilation failures in their repositories
after the corresponding SemStreams tag; this work records the requirement but does not edit downstream code.

## Controlled shutdown sequence

For SIGTERM, SIGINT, or operator-requested configuration activation, composition must:

1. create one fresh bounded shutdown context, not reuse the canceled runtime context;
2. stop external listeners and admission owners;
3. call service/component Stop in dependency-safe reverse order while callback authority remains live;
4. let every owner drain exact handles, settle durable work, then cancel and join remaining work;
5. aggregate every Stop error without skipping later owners;
6. call terminal Client Close only after every owner Stop returns;
7. aggregate transport and owner results;
8. emit clean observability and exit zero when the aggregate is nil; or
9. emit failed phase/owner observability and exit nonzero when it is not nil.

Both outcomes terminate the current process. Neither reuses Client or restarts components in process. Supervision
starts a fresh process, which consumes the latest committed desired state without requiring a clean-exit marker.

## Ordered delivery and release gates

The migration is deliberately split so removing temporary ownership scaffolding cannot race owner adoption:

1. **PR2-reset-contract** — approved contract, inventory, spec deltas, ADR amendment, and this guide.
2. **PR2-owner-handles** — exact handles and owner migration while temporary Client catalogs remain.
3. **PR2-client-minimal** — remove catalogs and implement synchronous Connect plus terminal transport-only Close.
4. **PR2-raw-capabilities** — execute every approved RETIRE/NARROW native-surface disposition.
5. **PR2-composition-proof** — prove clean and failed controlled shutdown both exit and fresh-process restart works.
6. **PR2-dirty-proof** — prove retained-state recovery at deterministic kill and settlement boundaries.

The breaking tag remains blocked until all six PRs, focused/race/integration/contract checks, schema no-drift, and the
relevant real-process and E2E gates are green. Contract approval alone is not runtime or recovery evidence.
