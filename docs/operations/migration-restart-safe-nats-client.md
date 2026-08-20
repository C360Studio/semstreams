# Migrating to native one-shot lifecycle ownership

## Status and scope

This guide records the owner-approved pre-v1 target from ADR-095 and
`simplify-one-shot-lifecycle-ownership`. Contract application does not claim that runtime migration, controlled proof,
dirty proof, or settlement proof is complete. Do not use the target signatures until their ordered implementation
lands and its checks pass.

The landed caller-owned context signature prerequisite is documented in
[Migrate to caller-owned lifecycle contexts](migration-restore-go-lifecycle-ownership.md). Execution status and sole
lifecycle completion authority are recorded in the OpenSpec
[`recovery-ledger.md`](../../openspec/changes/simplify-one-shot-lifecycle-ownership/recovery-ledger.md).

ADR-095 supersedes PR #984's proposed stateful `ManagedConsumer`, `DrainAndDelete`, lifecycle-local backlog,
running-generation rejoin, name-routed child catalog, and retained Close-result mechanics. ADR-094 remains immutable
history. Its boot-only composition, dedicated rule-definition hot reload, raw-root retirement, always-exit controlled
shutdown, dirty recovery, and proof gates remain accepted.

ADR-095 and `simplify-one-shot-lifecycle-ownership` supersede PR #984's managed-consumer, lifecycle deletion,
concurrent/rejoin, and retained-result mechanics and own the complete `restart-safe-shutdown` and
`jetstream-consumer-policy` lifecycle target. This change retains boot-only composition and depends on the new change's
broad-root retirement, settlement/outbound-flush, controlled-process proof, dirty-recovery, durable-communication,
live-storage/replica validation, NATS restart, clean-marker independence, and latest-desired-state guarantees. No
runtime or proof task is completed by delegation.

## Native owner migration

The three retained consume constructors change from error-only setup to exact native ownership:

```go
func (c *Client) ConsumeStreamWithConfig(...) (jetstream.ConsumeContext, error)
func (c *Client) ConsumeStreamWithConfigContexts(...) (jetstream.ConsumeContext, error)
func (c *Client) ConsumeInternalStreamWithConfig(...) (jetstream.ConsumeContext, error)
```

All fallible stream, consumer, policy, and observation setup must finish before `Consumer.Consume`. Successful Consume
is the delivery commit point; no fallible setup follows it. The caller retains that exact native handle through Closed.
There is no SemStreams managed lifecycle wrapper.

`ConsumeDurable` retires because its measured production-adopter census is zero. Durable owners use a retained native
handle plus the existing heartbeat and settlement primitives. ADR-070 remains historical context.

For a successfully running owner, `Stop(ctx)` uses one direct order:

1. return nil without side effects if Stop already completed;
2. fence external and queue admission;
3. initiate concrete native Drain or Shutdown while accepted callback contexts remain live;
4. await exact native Closed under the shutdown context;
5. cancel remaining runtime work;
6. await owner done/WaitGroup under the shutdown context;
7. perform terminal best-effort cleanup; and
8. return the aggregate observed by this invocation.

A class with no native callback resource omits Drain/Closed but cancels ctx-driven work before awaiting its WaitGroup.
Deadline expiry is a failed process result. It does not create authority for later running-generation rejoin. Completed
repeated Stop is nil/no-op; concurrent Stop and replay of the first result are not supported contracts.

## Failed-Start cleanup migration

Failed Start is not running-generation rejoin. Any owner that can acquire a resource during Start must publish its owner
record before the acquisition can escape. Where manager Stop can race Start, retain `startDone` and observe it before
choosing cleanup.

On Start failure, finalize Start and attempt the one bounded synchronous rollback. Clear handles only when cleanup
succeeds. If rollback fails or expires, retain cancel, done, and every exact acquired handle; enter `cleanupPending`;
reject another Start; and permit later manager `Stop(ctx)` to retry cleanup using its caller context. Keep
`RunPartialStartRollback` or an exactly equivalent bounded helper until all 21 measured paths prove this invariant.

## Rule context API migration

The owner-approved RU1 source target makes five Rule APIs context-first. The source break deliberately requires callers
to propagate explicit cancellation and operation authority instead of relying on retained or invented roots. It does
not change rule configuration, subjects, persisted state, or schemas:

```go
// Before
configManager.InitializeKVStore(natsClient)
executionContext.SubstituteVariables(template)
executionContext.SubstituteVariablesWithIterVar(template, varName, value)
rule.SubstituteConditionValues(conditions, executionContext)
evaluator.EvaluateEntityState(entityState)

// After
configManager.InitializeKVStore(ctx, natsClient)
executionContext.SubstituteVariables(ctx, template)
executionContext.SubstituteVariablesWithIterVar(ctx, template, varName, value)
rule.SubstituteConditionValues(ctx, conditions, executionContext)
evaluator.EvaluateEntityState(ctx, entityState)
```

Pass the exact composition, watcher, evaluation, or action context that owns the operation. Implementations must not
retain that context or replace it with a new root. `EntityStateEvaluator` implementations should treat nil context as
invalid caller input; framework call paths provide a non-nil operation context.

The time-bounded read-only sister census for this source break is exact:

- SemTeams has one call in `cmd/semteams/main.go`; change it to
  `rcm.InitializeKVStore(ctx, natsClient)` using the function's existing `ctx` parameter.
- SemSpec has two `ExecutionContext.SubstituteVariables` calls in
  `workflow/execrules/rulepack_test.go`; pass the test context as the new first argument.
- The read-only sister census found zero calls to `SubstituteVariablesWithIterVar` or `SubstituteConditionValues`.
  External adopters calling either exported method must still pass their exact operation context as the new first
  argument.
- SemSpec has 15 `EvaluateEntityState` calls in `workflow/intakerules/rulepack_test.go`; pass a non-nil test context as
  the new first argument.

SemStreams does not edit those repositories. Their owners make and validate the source migrations in their own
repositories. Independent RU1 implementation review and the final isolated structural E2E are green. Owner-migrated
credit is limited to `processor/rule/processor.go`; this migration map grants no supporting-file, broader task,
release, or tag credit.

## Metrics HTTP server context migration

The existing `metric.Server` lifecycle methods now take the caller's context directly:

```go
server := metric.NewServer(port, path, registry, securityConfig)
if err := server.Start(runtimeCtx); err != nil {
    return err
}
if err := server.Stop(shutdownCtx); err != nil {
    return err
}
```

`Start` binds synchronously and uses its exact context as the HTTP server base context. `Stop` attempts graceful HTTP
shutdown within the supplied shutdown budget. If that attempt fails or the budget expires, it force-closes the
server and waits for the exact serving goroutine under a separate fixed one-second bound. A completed repeated Stop
is a nil no-op; concurrent Stop is unsupported and returns a typed transient error. Each Server is one-shot, so
restart constructs a fresh instance. The read-only sister-repository census found no direct `metric.NewServer`,
`service.NewMetrics`, or metrics-server lifecycle callers; external direct users discover the source break at compile
time and pass their existing runtime and shutdown contexts. Configuration, endpoint paths, and scrape behavior do not
change.

## Duplicate durable identity

Two local owners cannot share one `(stream,durable)` identity. Canonically derive and validate every identity knowable
from sealed composition before the parallel Start barrier. If an identity is genuinely unknowable before acquisition,
use only a minimal active identity-plus-opaque-owner-token claim.

A duplicate names both owners and fails boot. It never stops, drains, deletes, or replaces the incumbent. The fallback
claim is not a handle catalog and stores no lifecycle, observation, deletion, generation, or result authority.

## Backlog observation and topology deletion

Outstanding-work observation remains an independent, concurrency-guarded exact-consumer read. Graph-ingest readiness
and the accepted agent-loop inflight API keep their current semantics. The observer exposes no Stop, Drain, deletion,
or child cleanup. Unknown observation remains an error, not zero. `NumPending + NumAckPending == 0` means no currently
outstanding deliverable work; it is not semantic completion and does not prove the absence of MaxDeliver-parked work.

Production owner Stop and Client Close never delete durable consumers. Retire without aliases:

- `Client.StopConsumer`;
- `Client.StopAndDeleteConsumer`;
- `Client.StopAllConsumers`; and
- the five production `DeleteConsumerOnStop` fields.

A namespace-scoped fixture/admin helper records exact test-created stream/durable identities, drains local owners, and
deletes only those recorded identities. It is not a production Stop option and Client Close never calls it.

## Settlement and poison boundaries

A durable callback ACKs only after its required durable or externally acknowledged effect. Transient effect failure
controls NAK; structurally permanent input may control Term. Settlement-changing errors must reach disposition rather
than be logged and swallowed. Shutdown and heartbeat cancellation join long-running work before overlapping redelivery.

Graph-ingest keeps two explicit paths:

- metadata/decode/extract poison occurs before keyed admission and follows the existing counted policy ACK-drop;
  caught-up remains “no deliverable work,” never semantic completion; and
- keyed work validates, reads the durable guard, applies graph effects, commits the guard, updates the in-memory guard,
  and only then ACKs.

Every externally repeatable lane declares stable idempotency, durable progress/outbox, or explicit at-most-once
semantics. File append, HTTP POST, WebSocket, paid model calls, rule actions, raw ObjectStore writes, and multi-output
transforms do not receive fabricated exactly-once claims. A path claiming server-confirmed source settlement records a
latency/failure SLO and uses `DoubleAck(ctx)` only when that SLO requires it. Plain Ack is not synchronous confirmation;
DoubleAck timeout or failure remains replay-safe and non-clean.

## Terminal Client and process shutdown

After all owner Stops, Client Close rejects new work, initiates native connection Drain, observes exact CLOSED, cancels
remaining Client-owned health/metrics runtime, awaits those workers, performs terminal credential/transport cleanup,
and returns the observed aggregate. Client never enumerates, rediscover, drains, stops, deletes, or waits for
component-owned consumers or subscriptions. Preclosed transport, native LastError, and deadline-forced close remain
non-clean. Completed repeated Close is nil/no-op; concurrent Close and retained result replay are not contracts.

Composition retains the live runtime context, creates one fresh bounded shutdown context, stops owners in reverse
dependency order, aggregates every owner and Client result, and exits. Clean and failed controlled shutdown both exit;
only status and observability differ. Supervision constructs the fresh process and Client from latest committed desired
state. A clean marker is never an activation precondition.

## Raw roots remain a separate gate

The authoritative per-symbol disposition remains
`openspec/changes/require-restart-for-config-activation/native-surface-inventory.md`, SHA-256
`d79df592e7049d4f0e3412bf41e8c61d44ea0829a6fddc2734cff40ceb966617`. Execute every RETIRE/NARROW row without an
`Unsafe*` alias. ADR-095 does not weaken this separately approved surface gate.

## Ordered delivery and release gates

1. **Contract supersession** — ADR-095, new OpenSpec change/deltas, PR #984 supersession, task truth, and this guide.
2. **Owner handles, admission, lifecycle simplification** — native handles, fallible-before-Consume setup, duplicate
   rejection, all 42 owner migrations, failed-Start authority, fixture deletion, and removal of stateful helpers.
3. **Client minimal** — remove child catalogs and same-name replacement; retain independent observation; make Close
   terminal transport-only.
4. **Raw-root narrowing** — execute every approved RETIRE/NARROW disposition.
5. **Controlled proof** — real SIGTERM/SIGINT ordering, cleanupPending, duplicate rejection, aggregate exit truth,
   listener release, and fresh boot.
6. **Dirty and settlement proof** — deterministic kill at delivery/effect/guard/publication/pre-ACK boundaries,
   redelivery/convergence, effect guarantees, and the declared DoubleAck decision.

The breaking tag remains blocked until runtime migrations, focused/race/integration/contract checks, schema no-drift,
and relevant controlled, dirty, and E2E gates are green. This contract-only application checks none of those tasks.
