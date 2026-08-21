# Design: One-shot running lifecycle with retained failed-Start cleanup authority

## Surface inventory

### 1. Claimed gap: OpenSpec transaction shape

OpenSpec `MODIFIED` blocks must use the exact title of a requirement already present in the current capability spec.
An invented or renamed title under `MODIFIED` cannot match current truth. A title change must be represented as the
exact current requirement under `REMOVED` plus the replacement under `ADDED`.

The exact current titles reached by this change are:

| Current spec and line | Exact current requirement title | Delta operation |
|---|---|---|
| `openspec/specs/service-shutdown/spec.md:16` | `Coordinated shutdown treats an already-stopped service as clean success` | `MODIFIED` exact title |
| `openspec/specs/service-shutdown/spec.md:47` | `A framework service Stop is idempotent on repeated invocation` | `MODIFIED` exact title |
| `openspec/specs/jetstream-consumer-policy/spec.md:37` | `Every exported port-backed consumption operation requires policy context` | `MODIFIED` exact title |
| `openspec/specs/jetstream-consumer-policy/spec.md:54` | `Non-port consumption is explicit and bounded` | `MODIFIED` exact title |
| `openspec/specs/jetstream-consumer-policy/spec.md:87` | `Consumer policy metrics never retain stale effective truth` | `MODIFIED` exact title |
| `openspec/specs/graph-ingest/spec.md:162` | `Concurrent ingest MUST bound in-flight work and preserve at-least-once ack` | `MODIFIED` exact title |
| `openspec/specs/graph-ingest/spec.md:181` | `A redelivered stale message MUST NOT overwrite a newer write` | `MODIFIED` exact title |

New concepts—failed-Start cleanup authority, duplicate durable identity rejection, and the new
`restart-safe-shutdown` capability—use `ADDED`, not `MODIFIED`.

The current `openspec/specs/lifecycle/spec.md` is domain workflow lifecycle and remains untouched.

### 2. Every retained PR #984 restart-safe guarantee

PR #984's conflicting handle/rejoin/deletion mechanics are superseded, but the following normative guarantees are all
retained and must be transferred before its old delta is removed:

| Guarantee | Existing source | New-delta destination |
|---|---|---|
| broad mutable NATS roots retire; no `Unsafe*`; sister repos read-only | old restart-safe spec `:170-186` | requirement `Broad NATS ownership roots retire before release` |
| durable handler ACK follows required effects/publications | `:188-210` | requirement `Durable settlement distinguishes completed from unfinished work` |
| accepted outbound NATS publications flush/drain before clean close | `:195-217` | same settlement requirement and scenario |
| all controlled shutdowns exit; fresh Client/process; clean/non-clean status | `:219-255` | requirement `Restart safety is proven across a real process boundary` |
| proof includes in-flight and pending work, semantic result, recovery, listener/consumer ownership, next-boot config | `:227-255` | same controlled-proof requirement |
| dirty recovery is independent of Stop, Drain, defer, finalizer, detached cleanup | `:257-267` | requirement `Dirty restart correctness does not depend on shutdown hooks` |
| crash-critical work/state uses durable JetStream or KV; core NATS excluded unless loss is explicitly noncritical | `:259-286` | same dirty-recovery requirement |
| crash-critical streams/KV are file backed and live storage/replica policy is validated at boot | `:264-293` | same dirty-recovery requirement |
| effect-before-ACK and idempotent/stable-key convergence; no false exactly-once external claim | `:269-300` | same dirty-recovery requirement |
| deterministic process kill after delivery/effect/publication/before ACK | `:302-319` | requirement `Dirty restart is proven at settlement boundaries` |
| SemStreams and isolated NATS are killed; NATS restarts from the same file store | `:304-326` | same dirty-proof requirement |
| every boot consumes latest committed desired state regardless of prior exit | `:309-333` | same dirty-proof requirement |
| clean-exit marker is observability, never activation prerequisite | `:311-333` | same dirty-proof requirement |
| stale prior-boot observation is never current evidence for new boot | `:328-333` | same dirty-proof scenario |

No row may be replaced by a summary sentence in proposal/design/tasks alone. Each remains a normative requirement or
scenario in the new `restart-safe-shutdown` delta.

### 3. Active `require-restart-for-config-activation` inventory

Exact files requiring reconciliation:

```text
openspec/changes/require-restart-for-config-activation/proposal.md
openspec/changes/require-restart-for-config-activation/design.md
openspec/changes/require-restart-for-config-activation/tasks.md
openspec/changes/require-restart-for-config-activation/specs/jetstream-consumer-policy/spec.md
openspec/changes/require-restart-for-config-activation/specs/restart-safe-shutdown/spec.md
docs/operations/migration-restart-safe-nats-client.md
```

The proposal, design, and tasks must explicitly delegate the complete restart-safe capability to
`simplify-one-shot-lifecycle-ownership`. The two old spec delta files leave PR #984's active target only after their
complete compatible content exists in the new delta. PR #984 retains boot-only composition, rule hot reload, flow
activation truth, raw-root dependency, controlled/dirty proof gates, and latest-desired-state dependency by reference
to the new capability.

These PR #984 files remain byte-identical:

```text
docs/adr/094-boot-only-composition-and-observable-rule-activation.md
openspec/changes/require-restart-for-config-activation/native-surface-inventory.md
```

The PR #984 inventory keeps its forensic body and approved hash as provenance, with only the explicit historical
supersession banner required by this reconciliation.

All non-lifecycle spec deltas remain owned by PR #984.

### 4. Active `restore-go-lifecycle-ownership` inventory

Exact package:

```text
openspec/changes/restore-go-lifecycle-ownership/proposal.md
openspec/changes/restore-go-lifecycle-ownership/design.md
openspec/changes/restore-go-lifecycle-ownership/inventory.md
openspec/changes/restore-go-lifecycle-ownership/tasks.md
openspec/changes/restore-go-lifecycle-ownership/specs/runtime-context-ownership/spec.md
openspec/changes/restore-go-lifecycle-ownership/specs/service-shutdown/spec.md
```

Compatible retained truth:

- context-bearing `Service.Stop`, `Manager.StopAll`, and `LifecycleComponent.Stop` signatures;
- Start context is lexical runtime authority; production structs retain cancel/join state, not `context.Context`;
- no invented/detached library roots, with the measured synchronous failed-Start rollback and HTTP BaseContext
  exceptions;
- caller Stop context bounds work but is not runtime authority;
- nil context is rejected before action;
- manager Start context is passed as a goroutine function parameter;

Lifecycle truth owned by this change:

- exact `startDone`/Start finalization and no Start/Stop method-body overlap;
- failed Start may retain exact cleanup authority, reject another Start, and later clean under manager Stop;
- callback-borrow shutdown fences new borrows, waits for admitted callbacks to return without manager/gate locks, and
  requires the callback to return before outer composition requests Stop rather than self-stopping.

Conflicting truth to remove/delegate:

- `design.md:29-31,118-121,132`: repeated running Stop rejoins and retained genuine error;
- `specs/service-shutdown/spec.md:5-8,28-31`: `stopping` is already clean;
- the same file `:69-82`: concurrent Stop shares completion and repeated Stop replays retained error;
- `tasks.md:21-30,42-43`: stateful `Generation`/`Operation` terminal-result authority and rejoinable deadline failure;
- `runtime-context-ownership/spec.md:78-128`: keep context ownership and exact ordering, but change generic idempotency to
  completed repeated no-op and explicitly prohibit pre-cancel WG wait.

`restore-go-lifecycle-ownership/specs/service-shutdown/spec.md` must leave that active change after the new change owns
the exact current-title modifications. The restore proposal/design/tasks/inventory must point service-shutdown semantics
to ADR-095 and `simplify-one-shot-lifecycle-ownership`; its runtime-context capability remains active.

### 5. Consumer at birth

| Surface | Present consumer | Disposition |
|---|---|---|
| native `jetstream.ConsumeContext` | every owner starting JetStream delivery | exact returned lifecycle handle |
| owner-local exact completion waits | each lifecycle owner with an exact done/Closed handle | inline context-bounded select; no shared wait helper |
| bounded failed-Start rollback | 21 paths | retained with cleanupPending authority |
| duplicate check | Client-local claim | preserve reject-not-replace; sealed validation deferred |
| backlog observer | graph readiness and accepted agent-loop inflight | read-only and separate from lifecycle |
| fixture/admin deletion | namespaced tests | no production Stop/config surface |
| concurrent/rejoin/retained running Stop | no production consumer | removed from both active changes |

## Adopter seam inventory

### Component author

The author retains the exact returned native handle and implements one owner Stop. The framework sequence is fixed:
fence; native Drain/Shutdown; exact native Closed while callback authority is live; cancel remaining runtime; await
owner WG/done; cleanup. They do not know consumer identity derivation, backlog math, deletion policy, generation IDs,
rejoin, or Client catalogs.

If Start fails after acquisition, the owner record remains authoritative until bounded rollback or later manager Stop
completes cleanup. Clearing it early is a typed boot failure, not a documentation concern.

### Agent-loop inflight caller

No adopter change. The accepted exported request/reply contract continues to hide stream/durable names and return
unknown rather than false zero. Only its internal observation source moves out of Client lifecycle bookkeeping.

### Supervisor and test author

The supervisor observes process exit status and never rejoins a running process generation. A test fixture records and
deletes only its own durable identities through fixture/admin teardown; it does not set a production deletion knob.

## Target-state decisions

### D1. Running Stop is one-shot and direct

The exact native owner order is fence, native Drain/Shutdown, exact Closed with callback authority live, cancel, owner
join, and terminal cleanup. Deadline failure remains non-clean and still issues cancellation before any ctx-driven join.
Completed repeated Stop is nil/no-op; concurrent Stop and running-generation rejoin are not contracts.

### D2. Failed Start retains cleanup authority

The owner publishes cleanup authority before acquisition escapes and retains `startDone` where Stop can race Start.
Failed or expired rollback retains every exact handle in `cleanupPending`, rejects another Start, and permits later
manager Stop. This is distinct from running-generation rejoin.

### D3. Native consumption is the commit point

All fallible setup and observation precedes `Consumer.Consume`; successful commit returns the exact native
`jetstream.ConsumeContext`. Duplicate local identity rejects rather than replaces. Observation and fixture deletion
remain separate from lifecycle.

### D4. Complete PR #984 guarantees transfer normatively

The restart-safe delta owns broad-root retirement, settlement and outbound flush, controlled process proof, dirty
recovery without hooks, durable-only crash-critical communication, live storage/replica validation, external-effect
limits, SemStreams/NATS kill proof, clean-marker independence, and latest-desired-state recovery.

### D5. Restore retains context prerequisite; lifecycle truth is owned here

Restore retains the completed context-bearing signature prerequisite and remaining context/root debt. This change
exclusively owns exact Start finalization, failed-Start cleanup, service shutdown, terminal sequencing, ACK ordering,
and controlled/dirty proof.

## Superseded six-ruling N1 design

### Accepted inventory and binding precedence

The N1 inventory gate passed independently for
[`n1-convergence-inventory.md`](n1-convergence-inventory.md), baseline
`2f974bdb7f22efb39ac5136e9c0b719b711249c2`, SHA-256
`2a95a0f5fd6683aeed585c8dca43d65ff662f32b2b046ce2262f6b97f74612e9`, verdict
`INVENTORY PASS`.

ADR-095 is binding and supersedes ADR-094 for managed-consumer, resumable running-Stop, drain/delete, name-catalog,
and retained-result mechanics. ADR-094 remains immutable history for its other accepted guarantees. The current
`openspec/specs/gated-dag-dispatch/spec.md:43-77` contract remains binding: gated-DAG requires typed durable
consumption and heartbeat validation.

The owner approved an earlier six-ruling package before the inventory gate and corrected design. That approval remains
historical evidence but is superseded for execution. Independent review of this corrected package returned pre-owner
`DESIGN APPROVE` with no findings against reviewed design SHA-256
`a9de5bd5cd86c484466eadee0947b8afe3d5dffb17249c2a8a48eeeba42faa0a`; the accepted inventory identity is unchanged.
The six-ruling execution package that followed is no longer the working target. It overreached by redesigning
`Subscription.Drain` without a demonstrated defect and made the cleanup harder to reason about. Its inventory and
review remain historical evidence; its R3 semantics and its reconfirmation blocker are withdrawn.

### Historical R1. Separate mechanical deletion from breaking convergence

Options:

1. **Do nothing.** Retain zero-use `internal/lifecyclejoin` production code and obsolete behavior tests.
2. **Extend the incumbent.** Move or facade the stateful helpers, preserving phantom generation, operation, rejoin, and
   retained-result authority under a different package.
3. **Recommend.** N1a mechanically deletes only `internal/lifecyclejoin` and its obsolete tests. N1b later performs the
   breaking NATS convergence. Neither wave adds a replacement, alias, or state machine, and both remain under one
   no-release/no-tag boundary.

`internal/lifecyclecleanup.RollbackFailedStart` remains the single reviewed stateless failed-Start helper. N1a
receives no N1b API, configuration, runtime-proof, release, or tag credit.

### Historical R2. Make canonical port operations return exact ownership

Options:

1. **Do nothing.** Retain four exported port methods: two error-only canonical methods and two temporary handle bridges.
2. **Extend the incumbent.** Keep `*Handle` aliases or wrappers, preserving two contracts and two discovery paths.
3. **Recommend.** Replace the canonical methods with the exact signatures below, migrate all 16 local bridge callers,
   and remove the error-only/catalog path and both bridges without aliases.

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

Validation, stream and consumer creation, policy observation, identity claim, and final context checks all precede
`Consumer.Consume`. Successful Consume is the delivery commit; no fallible setup follows it. The exact native handle
returns to the owner. `ConsumeInternalStreamWithConfig` remains unchanged.

The authoritative read-only downstream census is nine production calls: SemSpec 6, SemDev 2, and SemDragon 1. Two
SemSpec worktrees add 18 raw duplicates, not additional authoritative adopters. SemStreams documents the compile-time
migration and does not edit sister repositories.

### Historical R3. Make Subscription Drain state-free and one-shot

Options:

1. **Do nothing.** Preserve stored `sync.Once`, completion, error, replay, and later-rejoin state.
2. **Extend the incumbent.** Formalize that state with a public Closed or handle API and make concurrent/rejoin
   authority an adopter contract.
3. **Recommend.** Keep exactly `Drain(context.Context) error`, reject nil before action, invoke native Drain once by
   owner convention, preserve its native error, and await native closure under the caller context.

The Subscription stores no once, result, completion, election, replay, or rejoin state and exposes no new public
`Closed`. Native and caller-context errors are returned uncached and both are preserved when both occur. Concurrent
callers receive no shared-result contract.
A fresh lifecycle requires a fresh Subscription. Its owner chooses Drain or Unsubscribe; Client neither tracks nor
closes subscriptions.

### Historical R4. Replace stateful durable consumption with a stateless handler builder

Options:

1. **Do nothing.** Retain `ConsumeDurable` hidden ownership, BackOff-blind validation, overflow arithmetic, and global
   Stop coupling.
2. **Extend the incumbent.** Add handle returns, aliases, or options to the old method, retaining a stateful front door.
3. **Recommend.** Add exactly this builder and delete `ConsumeDurable` without an alias:

```go
func NewDurableHandler(
    cfg StreamConsumerConfig,
    heartbeat time.Duration,
    work func(context.Context, []byte) error,
) (func(context.Context, jetstream.Msg), error)
```

The builder rejects nil work and nonpositive heartbeat. When `cfg.BackOff` is nonempty, every interval must be
positive and the effective acknowledgement wait is the minimum interval regardless of order. Invalid BackOff errors
identify the index and value. Without BackOff, a positive AckWait is effective; otherwise the default is 30 seconds.

Validation requires `heartbeat <= effectiveAckWait/2`, permits equality, and uses division only. Errors identify the
heartbeat and computed ceiling. Tests cover nonmonotonic BackOff, a shorter later entry, nonpositive entries, default
AckWait, equality, one nanosecond over, and overflow-scale durations.

The returned callback delegates Ack, Nak, Term, InProgress, cancellation, heartbeat failure, and work join exclusively
to `ConsumeWithHeartbeat`. Every nonnil result emits WARN with exact message `ConsumeDurable handler error` and fields
`stream`, `consumer`, and `error`; it is never suppressed, sampled, or downgraded. The builder retains no context and
owns no consumer, handle, goroutine, identity, catalog, Stop, deletion, or replay state.

The migration covers ten production calls in SemMachina 8, SemSpec 1, and SemDragon 1, seven SemMachina interfaces,
and SemMachina boot's `StopAllConsumers` call. Owners compose the builder with R2, retain exact handles, and stop them.
Sister repositories remain read-only.

### Historical R5. Remove Client child and name lifecycle authority

Options:

1. **Do nothing.** Retain consumer/subscription catalogs, replacement, name APIs, and Client Close enumeration.
2. **Extend the incumbent.** Merge lifecycle, observation, and identity into richer descriptors or shared stores.
3. **Recommend.** Remove Client consumer bindings, their mutex and shared-drain state, subscription catalog,
   same-name replacement, `StopConsumer`, `StopAndDeleteConsumer`, `StopAllConsumers`, `OutstandingWork`, and Client
   Close child enumeration. Client Close owns transport and Client workers only.

These distinct mechanisms remain and must not be deleted or merged:

- Client-scoped `internalClaims` identity-plus-token duplicate map. Precommit failure releases; committed acquisition
  releases only after exact Closed, never from Client Close.
- Generic `jetstreamMetrics.consumers` and policy `jetstreamMetrics.policies` observation catalogs, including
  `consumerPolicyRecord` and `ObserveDirectPortConsumerPolicy`.
- Existing shared registry policy collectors. Per-Client maps still share process collectors; the known cross-Client
  label collision remains unresolved rather than being reclassified as lifecycle.
- OTEL `localOTELConsumerClaims.active`, owner cleanup, and deadline-retention semantics.
- `ConsumeInternalStreamWithConfig` and its exact internal-owner handle contract.
- Graph-ingest private readiness lookup.
- Agentic-loop recorded binding, where `NumPending + NumAckPending == 0` is backlog zero and unknown is never zero.

### Historical R6. Remove inert delete configuration without a replacement

Options:

1. **Do nothing.** Keep five local no-op knobs and the active sister delete path.
2. **Extend the incumbent.** Deprecate the fields, make them functional, or add a replacement production knob.
3. **Recommend.** Remove five local Go fields and generated-schema properties with no replacement: OTEL exporter,
   agentic dispatch, agentic loop, agentic model, and agentic tools. Stale configuration fails visibly.

Test cleanup is private and deletes only exact identities created by that fixture. It never discovers neighbors, uses
a wildcard, or becomes a production Client method.

Read-only downstream impact is complete:

- SemStreams UI has five copied schemas and `src/lib/types/api.generated.ts`.
- SemSpec has `ui/src/lib/types/semstreams.generated.ts`.
- SemTeams has four copied schemas and `ui/src/lib/types/api.generated.ts`.
- SemDragon questtools has an inert field plus three tests. Questbridge has an active field/read/direct-delete path
  plus three tests.
- SemConnect and the other inventoried sisters have zero affected configuration consumers.

Each sister owner removes or regenerates its copies and validates its own repository. SemStreams makes no downstream
write.

### Historical adopter seam inventory

#### Port component author

Must retain the exact handle and Drain and await Closed in owner Stop. Doing nothing causes a compile failure at the
changed return. Discovery is compiler output, the migration guide, and release notes. The author should know no names,
catalogs, deletion policy, or rejoin mechanics.

#### Durable component author

Must build the handler, call the canonical method, and retain its handle. Doing nothing causes a compile failure when
the old method disappears. Discovery is compiler output and the migration guide. The author should know no timing
arithmetic or settlement rules.

#### Subscription holder

Uses one Drain attempt per Subscription. Doing nothing relies on unsupported retry or rejoin behavior. Discovery is the
mandatory migration and release notes. The signature is unchanged; the holder only needs a fresh object for a fresh
lifecycle.

#### Configuration author

Removes `delete_consumer_on_stop`. Doing nothing fails schema validation visibly. Discovery is generated schema and
migration guidance. There is no replacement deletion policy.

#### Test author

Records and deletes exact identities the fixture created. Discovery-based cleanup is unavailable. The private fixture
contract is the discovery surface; no production cleanup knob is required.

#### Metrics and inflight caller

Nothing changes. Existing APIs remain the discovery surface and the lifecycle migration stays invisible.

### Historical artifact identity and removed residues

The removed conformance table and TDD list belonged only to the reviewed artifact with SHA-256
`a9de5bd5cd86c484466eadee0947b8afe3d5dffb17249c2a8a48eeeba42faa0a`. Their old line anchors, R3 Subscription tests,
and six-ruling “Final” targets are not current requirements. The working-system-first section below is the only
active N1 map and test boundary.

### Historical handoff status

The accepted inventory and this corrected target check no task. Independent pre-owner `DESIGN APPROVE` returned no
findings and changed no binding semantics or inventory identity. At that historical checkpoint owner reconfirmation
of R1-R6 was the sole remaining design-authority blocker. The reset below supersedes it; every remaining runtime and
proof task stays unchecked.

## Working-system-first N1 reset — current execution target

This section supersedes the entire six-ruling N1 execution package above. The owner directed the project to return to
a system that works well and can be understood before considering further lifecycle improvements. That approves this
simplification and the removal of speculative semantics; it does not approve a new `Subscription.Drain` contract.

### Completed boundary: N1a deletion

Reviewed commit `8da1b83ae9c2f323bf484dc28e0574d81504bef9` deleted
`internal/lifecyclejoin/generation.go`, `generation_test.go`, `operation.go`, and `rollback.go`, and replaced one
test-only diagnostic in `processor/rule/lifecycle_owner_test.go`. The exact diff is 1 insertion and 749 deletions, net
-748, with zero production additions. Production and test imports, qualified symbols, and declarations are zero; the
package directory is empty; `internal/lifecyclecleanup` is unchanged. Independent implementation and merge review
returned `APPROVE` with no findings.

This proves the central simplification: migrated owners use ordinary Go ownership—private cancel functions, native
handles, completion channels or wait groups, and caller contexts—without a shared lifecycle state machine.

### Remaining boundary 1: canonical exact-handle port API

The two canonical port methods return the exact native handle:

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

All fallible setup precedes `Consumer.Consume`; successful Consume is the delivery commit point. Local callers retain
the returned handle. The temporary `*Handle` bridges are then deleted without aliases. The internal-consume method and
its owners are unchanged. This uses JetStream's own lifecycle primitive and removes duplicate framework ownership.

### Remaining boundary 2: remove hidden Client lifecycle authority

Delete the consumer and subscription child catalogs, their lifecycle-only state, same-name replacement,
`StopConsumer`, `StopAndDeleteConsumer`, `StopAllConsumers`, `OutstandingWork`, and Close-time child enumeration.
Client Close owns only transport and Client workers. Component owners stop the exact resources they acquired.

Keep independently owned mechanisms exactly separate: duplicate identity claims, consumer-policy and metrics
observation, `ObserveDirectPortConsumerPolicy`, OTEL claims, internal consumption, graph-ingest readiness, and
agent-loop inflight observation. None receives Stop, delete, replay, or Client Close authority.

### Remaining boundary 3: remove inert deletion configuration

Delete the five local `DeleteConsumerOnStop` Go fields and their generated-schema properties with no production
replacement. Private test fixtures may record and delete only exact stream/durable identities that they created.
There is no wildcard, discovery-by-name, or production cleanup switch.

Downstream copied schemas and generated types remain read-only migration obligations for their repository owners:
SemStreams UI, SemSpec, SemTeams, and SemDragon. SemDragon's active questbridge read/direct-delete path is called out
separately from its inert questtools field. SemStreams changes no sister repository.

### Remaining boundary 4: stateless durable handler

Delete `ConsumeDurable` and add exactly one replacement export:

```go
func NewDurableHandler(
    cfg StreamConsumerConfig,
    heartbeat time.Duration,
    work func(context.Context, []byte) error,
) (func(context.Context, jetstream.Msg), error)
```

The builder retains no context and owns no consumer, handle, goroutine, identity, catalog, Stop, deletion, or replay
state. Its callback delegates to existing `ConsumeWithHeartbeat`, preserving current Ack, Nak, Term, InProgress,
cancellation, heartbeat-failure, redelivery, and synchronous work-join behavior. Every nonnil result keeps the current
operator-visible WARN message `ConsumeDurable handler error` with `stream`, `consumer`, and `error` fields.

Validation is the only correction included with the move: nil work and nonpositive heartbeat fail before acquisition;
nonempty BackOff requires positive entries and uses the minimum interval regardless of order; otherwise positive
AckWait or the 30-second default is effective. Heartbeat may equal but not exceed half that interval. Division avoids
overflow. This preserves settlement behavior while moving lifecycle ownership to the caller.

### Explicit deferment: Subscription Drain

`Subscription.Drain(context.Context)` and all of its current semantics and tests remain unchanged. The prior R3 plan
to delete stored once/result/rejoin behavior is not part of N1. No new Drain semantics, public Closed surface, or
subscription state rewrite is authorized. Revisit only after the working system is green and a concrete defect or
adopter requirement demonstrates value.

### Complexity budget

The complete convergence must:

- delete seven exported APIs and add only `NewDurableHandler`, net -6 exports;
- remove five Go fields and five generated-schema properties;
- delete child catalogs and retained lifecycle state; and
- add zero lifecycle structs, interfaces, maps, mutexes, goroutines, contexts, or configuration switches.

A candidate that exceeds this budget stops for design review; it does not justify new machinery by extending the old
plan.

### Current four-boundary map

- **Exact port handles:** `n1-convergence-inventory.md:82-240` and `natsclient/stream.go`; canonical methods return the
  native handle and temporary bridges retire.
- **Minimal Client:** `n1-convergence-inventory.md:241-447,828-845`, `natsclient/client.go`, and
  `natsclient/stream.go`; child catalogs, name lifecycle APIs, `OutstandingWork`, and Close child cleanup retire.
- **Configuration:** `n1-convergence-inventory.md:674-827`; five inert Go/schema fields retire and fixture cleanup is
  private and exact-identity-scoped.
- **Durable handler:** `n1-convergence-inventory.md:552-673`, `natsclient/consume_durable.go`, and the gated-DAG spec
  `:43-77`; the stateless builder preserves settlement, redelivery, and WARN behavior.

The exact-handle, minimal-Client, and durable-handler boundaries now exist as one atomic implementation diff on
baseline `18cd4fcefeaa6e10780776dc0450b5b1dd877a46`, SHA-256
`887ffc0a3b61d52c7497b889756bd02b36e269be64919cdbe606bde40062fe60`. Independent final review returned `APPROVE`
and authorized commit. They were executed together because the honest RED intermediate—removing `OutstandingWork`
and `StopAllConsumers` during exact-handle cutover—made catalog-backed natsclient integration tests fail. Baseline
`18cd4fce` had zero SemStreams production calls to either method; agentic-loop already used direct JetStream
observation and lost only a comment reference. Atomic packaging was selected to avoid publishing an incoherent outward
API while SemMachina's downstream design still paired direct `ConsumeDurable` acquisition with `StopAllConsumers`.
The final state gives all 16 local owners exact native handles and deletes the superseded Client authority without
introducing an adapter.

The implementation ratchets down measured complexity: production changes 23 files by +102/-570 (net -468); tests
change 12 files by +292/-415 (net -123); total net is -591. `NewDurableHandler` is the sole replacement export and
retains no lifecycle state. `Subscription.Drain` is byte-for-byte outside the change. Inside this four-boundary map,
the five configuration/schema fields and private fixture cleanup remain the only unimplemented boundary. Tasks 2.3
and 3.3 remain unchecked, receive no credit here, and sit outside this narrowed map.

The landed Client-local `internalClaims` map keeps its exact reject-not-replace behavior, opaque pointer token,
precommit rollback, and exact-Closed release. N1 does not add owner labels or another claim map. Canonical sealed
pre-Start validation and an error naming both owners are deferred future improvements, not a fifth boundary.

Because that stronger ADR-095 admission requirement is deferred, current N1 does not claim complete ADR-095
conformance. It implements the simpler lifecycle subset above and preserves the landed fallback claim.

### Minimal TDD and verification

Use focused causal tests only for changed behavior: exact returned handles and fallible-before-commit ordering;
owner-held shutdown after Client catalog removal; preserved independent observers/claims; schema rejection and
identity-scoped fixture cleanup; and durable validation plus existing settlement/redelivery/WARN behavior. Use real
NATS where settlement semantics require it and channels/listeners rather than arbitrary sleeps.

Run affected and repository race tests, integration race, contract tests, lint, build, intended-only schema generation,
strict change/all-spec validation, and the relevant core/structural/agentic/semantic E2E tiers before release. Known
baseline scanner/census failures must be recorded rather than misreported as candidate regressions or green proof.

N1a and the atomic exact-handle/minimal-Client/durable-handler cutover have implementation credit. Configuration/schema
removal, full candidate proof, controlled/dirty recovery proof, release, archive, and tag readiness remain incomplete.
