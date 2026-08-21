# Migrating to native one-shot lifecycle ownership

## Status and scope

This guide records the working-system-first pre-v1 target from ADR-095 and
`simplify-one-shot-lifecycle-ownership`. The refreshed N1 inventory at baseline
`2f974bdb7f22efb39ac5136e9c0b719b711249c2`, SHA-256
`2a95a0f5fd6683aeed585c8dca43d65ff662f32b2b046ce2262f6b97f74612e9`, remains accepted evidence, not a mandate to
redesign every inventoried surface.

N1a is complete and independently reviewed `APPROVE` at commit
`8da1b83ae9c2f323bf484dc28e0574d81504bef9`: four `internal/lifecyclejoin` package files were deleted and one
test-only diagnostic changed, for 1 insertion, 749 deletions, net -748, and zero production additions. Remaining N1
work was deliberately limited to exact native handles, Client catalog/API removal, inert configuration removal, and a
stateless durable handler. The exact-handle, Client, and durable-handler work now exists as one independently approved,
commit-authorized atomic diff on baseline `18cd4fcefeaa6e10780776dc0450b5b1dd877a46`; its implementation SHA-256 is
`887ffc0a3b61d52c7497b889756bd02b36e269be64919cdbe606bde40062fe60`. The earlier six-ruling execution package is
superseded. In particular,
`Subscription.Drain` semantics and tests do not change in this pass.

The atomic diff changes 35 tracked files and removes 591 net lines: production is 23 files at +102/-570 (net -468),
and tests are 12 files at +292/-415 (net -123). All 16 local port owners use the canonical exact-handle methods;
temporary bridges, the error-only path, `ConsumeDurable`, Client child catalogs/bindings/replacement, name-routed
Stop/delete APIs, `StopAllConsumers`, `OutstandingWork`, and Close-time child enumeration are absent. Claims, metrics,
policy/OTEL observation, internal creation, graph-ingest readiness, and agentic-loop inflight observation remain.

The five Go fields and generated-schema properties have not been removed by this atomic cutover. Their fixture work is
the only remaining implementation boundary inside the working-system-first four-boundary subset. Tasks 2.3 and 3.3,
read-only sister migrations, candidate E2E, controlled/dirty proof, release, and tag gates remain unchecked and
outside that completed subset.

This narrowed N1 does not claim complete ADR-095 conformance. It preserves the already-landed Client-local
reject-not-replace durable claim and defers ADR-095's stronger sealed pre-Start validation and error naming both owners.

The complete convergence budget remains visibly net-negative: seven exports deleted and one added (net -6), five Go
fields/schema properties removed, child catalogs/state deleted, and zero new lifecycle structs, interfaces, maps,
mutexes, goroutines, contexts, or configuration switches. The atomic code cutover has met the export/catalog/state
portion; only the five field/schema removals and fixture cleanup remain.

The landed caller-owned context signature prerequisite is documented in
[Migrate to caller-owned lifecycle contexts](migration-restore-go-lifecycle-ownership.md). Execution status and sole
lifecycle completion authority are recorded in the OpenSpec
[`recovery-ledger.md`](../../openspec/changes/simplify-one-shot-lifecycle-ownership/recovery-ledger.md).

ADR-095 is binding and supersedes PR #984's proposed stateful `ManagedConsumer`, `DrainAndDelete`, lifecycle-local
backlog,
running-generation rejoin, name-routed child catalog, and retained Close-result mechanics. ADR-094 remains immutable
history. Its boot-only composition, dedicated rule-definition hot reload, raw-root retirement, always-exit controlled
shutdown, dirty recovery, and proof gates remain accepted.

The current `openspec/specs/gated-dag-dispatch/spec.md:43-77` contract also remains binding: gated-DAG uses typed
durable consumption and validates heartbeat against acknowledgement timing.

ADR-095 and `simplify-one-shot-lifecycle-ownership` supersede PR #984's managed-consumer, lifecycle deletion,
concurrent/rejoin, and retained-result mechanics and own the complete `restart-safe-shutdown` and
`jetstream-consumer-policy` lifecycle target. This change retains boot-only composition and depends on the new change's
broad-root retirement, settlement/outbound-flush, controlled-process proof, dirty-recovery, durable-communication,
live-storage/replica validation, NATS restart, clean-marker independence, and latest-desired-state guarantees. No
runtime or proof task is completed by delegation.

## Native owner migration

The three retained consume constructors return exact native ownership. The two port-backed signatures are exactly:

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

All fallible stream, consumer, policy, and observation setup must finish before `Consumer.Consume`. Successful Consume
is the delivery commit point; no fallible setup follows it. The caller retains that exact native handle through Closed.
There is no SemStreams managed lifecycle wrapper. SemStreams' temporary `ConsumeStreamWithConfigHandle` and
`ConsumeStreamWithConfigContextsHandle` bridges disappear in the same breaking cutover; do not migrate to those names.
All 16 local bridge callers move to the canonical names and retain the returned exact handle. The internal
`ConsumeInternalStreamWithConfig` method is unchanged by N1.

The authoritative read-only old-signature port census is nine production calls across three sister repositories:
SemSpec has six in `cmd/sandbox/qa_subscriber.go`, `processor/plan-decision-handler/component.go`,
`processor/researcher-manager/component.go`, `processor/structural-validator/component.go`,
`processor/qa-reviewer/qa_completed.go`, and `processor/lesson-decomposer/component.go`. SemDev has two in
`internal/conversationchannel/component.go` and `internal/intake/component.go`. SemDragon has one in
`processor/questtools/handler.go`. A raw checkout scan reports 27 because it also traverses nine corresponding calls in
each of the `semspec-ui-bmad` and `semspec-ui-run-visibility` SemSpec worktrees. Those 18 copies are not additional
authoritative adopters. SemStreams does not edit any of those repositories.

`ConsumeDurable` also retires, but its measured production-adopter census is ten calls, not zero: eight in SemMachina,
one in SemSpec, and one in SemDragon. The exact production call map is:

- SemMachina: `internal/stage/loopfailure.go`, `internal/stage/runner.go`, `internal/knowledge/consumer.go`,
  `internal/ledger/writer.go`, `internal/accusation/consumer.go`, `internal/caseflow/consumer.go`,
  `internal/turn/intake.go`, and `internal/egress/notifier.go`;
- SemSpec: `processor/execution-bridge/gated_dag_dispatch.go`; and
- SemDragon: `questdag/component.go`.

SemMachina has seven interfaces shaped around the old method in stage runner, knowledge consumer, ledger writer,
accusation consumer, caseflow consumer, turn intake, and egress notifier. Its boot engine also calls
`StopAllConsumers`. Migrate each owner to the stateless builder and canonical handle-return method:

```go
handler, err := natsclient.NewDurableHandler(cfg, heartbeat, work)
if err != nil {
    return err // invalid configuration; no consumer has been acquired
}

consumeHandle, err := client.ConsumeStreamWithConfig(ctx, owner, cfg, handler)
if err != nil {
    return err
}
```

The exact builder signature is:

```go
func NewDurableHandler(
    cfg StreamConsumerConfig,
    heartbeat time.Duration,
    work func(context.Context, []byte) error,
) (func(context.Context, jetstream.Msg), error)
```

The builder is stateless. It rejects nil work and nonpositive heartbeat. When BackOff is nonempty, every interval must
be positive and the minimum interval is the effective AckWait regardless of order; an invalid interval error names its
index and value. Without BackOff, positive AckWait is effective and nonpositive AckWait uses the 30-second default.
Heartbeat equal to half the effective AckWait is valid; a larger value fails with heartbeat and ceiling evidence. The
comparison uses division because the old `heartbeat*2` expression can overflow.

The returned handler delegates Ack, Nak, Term, InProgress, cancellation, heartbeat failure, and work join to
`ConsumeWithHeartbeat`; work code does not settle the message itself. Every nonnil result remains operator-visible as
a WARN with exact message `ConsumeDurable handler error` and fields `stream`, `consumer`, and `error`. Do not suppress,
sample, or downgrade that event.

SemMachina interfaces must return the exact `jetstream.ConsumeContext`, their owners must retain it, and boot shutdown
must stop those handles rather than call `StopAllConsumers`. SemSpec keeps its retry policy around acquisition.
SemDragon passes its owning Start context instead of inventing a Background root. `ConsumeDurable` has no alias.

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

Retain a returned Subscription and terminate it from its owner. N1 removes Client's hidden subscription catalog and
Close-time child cleanup, but it does not change `Subscription.Drain(context.Context)` behavior or tests. Any future
Drain simplification requires a concrete defect or adopter requirement after the working system is green.

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

Preserve the existing Client-local internal claim exactly. For a nonempty durable name it reserves
`(stream,durable)` with an opaque pointer token before acquisition, rejects a second live local claim without stopping,
draining, deleting, or replacing the incumbent, rolls back precommit failure, and releases only after the exact native
handle closes. It is not a child-handle catalog and stores no owner label or lifecycle result.

N1 does not add sealed pre-Start validation or change the error to name both owners. Those are deferred future
improvements to evaluate after the working system is green; callers continue to receive the existing duplicate local
durable identity error at acquisition.

## Backlog observation and topology deletion

The exported name-routed `Client.OutstandingWork(stream,name)` method is removed; its production caller census is now
zero. Graph-ingest readiness and the accepted agent-loop inflight API keep their current semantics through independent
owner-bound observation. That observation exposes no Stop, Drain, deletion, or child cleanup. Unknown remains an
error, not zero. `NumPending + NumAckPending == 0` means no currently outstanding deliverable work; it is not semantic
completion and does not prove the absence of MaxDeliver-parked work.

Production owner Stop and Client Close never delete durable consumers. Retire without aliases:

- `Client.StopConsumer`;
- `Client.StopAndDeleteConsumer`;
- `Client.StopAllConsumers`; and
- the five production `DeleteConsumerOnStop` fields.

The five removed Go fields are in the OTEL exporter, agentic dispatch, agentic loop, agentic model, and agentic tools
configuration. Their five `delete_consumer_on_stop` generated-schema properties are removed with them. This is a
breaking configuration migration even though the fields are now inert: delete the property from configuration before
validating against the new schema.

Read-only downstream inventory found generated copies that their repository owners must migrate:

- SemStreams UI: five copied schemas and `src/lib/types/api.generated.ts`;
- SemSpec: `ui/src/lib/types/semstreams.generated.ts`;
- SemTeams: four copied schemas and `ui/src/lib/types/api.generated.ts`; and
- SemDragon: an inert questtools field with three tests, plus an active questbridge field/read/direct-delete path and
  three tests.

SemConnect and the other inventoried sisters have no affected configuration consumer. SemStreams does not edit any
sister repository; each owner removes or regenerates its copies and runs its own validation.

Private fixture-owned cleanup records exact test-created stream/durable identities, drains local owners, and deletes
only those recorded identities. It is not a production Stop option or exported Client method; it never uses a wildcard
or discovers neighboring names, and Client Close never calls it.

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
and returns the observed aggregate. Client never enumerates, rediscovers, drains, stops, deletes, or waits for
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
3. **N1a mechanical deletion — complete** — reviewed commit `8da1b83a` removed the unused lifecyclejoin package with
   net -748 lines and zero production additions.
4. **N1b atomic code convergence — independently approved, commit authorized** — canonical handles, hidden Client
   child/name APIs, and the stateless durable handler are implemented together; Subscription Drain is unchanged.
5. **N1 configuration/schema completion — outstanding** — remove the five inert fields/schema properties and add
   private exact-identity fixture cleanup; sister repositories remain read-only migration obligations.
6. **Client minimal** — retain independent observation and the handle-free reject-only identity claim; make Close
   terminal transport-only.
7. **Raw-root narrowing** — execute every approved RETIRE/NARROW disposition.
8. **Controlled proof** — real SIGTERM/SIGINT ordering, cleanupPending, duplicate rejection, aggregate exit truth,
   listener release, and fresh boot.
9. **Dirty and settlement proof** — deterministic kill at delivery/effect/guard/publication/pre-ACK boundaries,
   redelivery/convergence, effect guarantees, and the declared DoubleAck decision.

N1a proved production and test import/symbol zeros, an empty package directory, and no lifecyclecleanup diff. Focused
rule race and ten repeated ownership race runs, lint, both builds, diff check, and strict OpenSpec 52/52 passed.
Independent review approved the landed commit. The atomic N1b code cutover also passed focused and full natsclient
race, race coverage for all 16 changed owners, and the full real-NATS natsclient runtime except three
worktree-scanner tests,
graph-ingest and agentic-loop integration, lint, build, diff check, and strict validation. Independent final review
returned `APPROVE` and authorized commit. The exact-handle intermediate made catalog-backed natsclient integration
tests fail; their call expressions were deleted with the obsolete catalog contract. Baseline `18cd4fce` had zero
SemStreams production calls to `OutstandingWork` or `StopAllConsumers`; agentic-loop already observed JetStream
directly and lost only a comment reference. Atomic packaging instead avoids publishing an incoherent outward API while
SemMachina's authoritative downstream owners still combine direct `ConsumeDurable` acquisition with
`StopAllConsumers` shutdown.

Full repository/service green is not claimed: known scanner and stale-baseline failures remain outside the approved
atomic surface. Full race/contract failures are not claimed green: the same failures
reproduce on the clean baseline because of user-owned worktree scanner pollution, stale natsclient census, and four
stale testinfra rows. Before full N1 can land, the remaining candidate must complete intended-only schema generation
and pass affected/full repository race, integration race, contracts, lint, build, strict change and all-spec validation,
`task e2e:core`, `task e2e:structural`, `task e2e:agentic`, and `task e2e:semantic`, plus independent implementation
review. If structural E2E does not exercise `NewDurableHandler`, record that coverage gap rather than count the tier as
builder evidence. The breaking tag remains blocked until broader controlled-process restart, dirty recovery,
settlement, latest-desired-state, and CI gates are green. The branch remains under the no-release/no-tag invariant.
