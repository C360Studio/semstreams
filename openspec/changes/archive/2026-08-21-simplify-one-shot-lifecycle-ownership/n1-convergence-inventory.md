# N1 Convergence Inventory

Inventory baseline: `2f974bdb7f22efb39ac5136e9c0b719b711249c2` (`HEAD` on 2026-08-20).

This is a read-only surface inventory. It records current code, current adopters, and collisions that a later N1
design must resolve. It makes no target-state or implementation choice. It grants no design, task, implementation,
test, gate, migration, release, archive, or tag credit.

The eight already-dirty design and migration documents were inputs only and were not changed while producing this
artifact. The untracked Metrics HTTP inventory was also left untouched.

## Inventory boundaries

- SemStreams source, tests, schemas, and current documentation are inventory inputs only.
- Sister repositories are read-only adopter evidence. Their checked-out source is not a SemStreams mutation target.
- Authoritative sibling roots are counted once. Worktrees and branch copies are reported separately.
- A source hit is not proof that a sibling currently resolves this exact SemStreams `HEAD`. Several siblings retain
  older method shapes, so their hits measure migration obligations, not a green cross-repository build.
- Historical and dirty design prose can contain stale censuses. Current source searches below are the evidence for
  this artifact.

The authoritative sister-root roster for every external API, holder, observer, and configuration census in this
artifact is exactly:

```text
../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage
../semsource ../semspec ../semstreams-ui ../semteams
```

These 11 roots are checked-out repositories, not worktree copies. `../semspec-ui-bmad` and
`../semspec-ui-run-visibility` are two non-authoritative SemSpec worktrees and are counted separately only where a raw
checkout census is useful. SemConnect has zero affected API, holder, observer, or configuration sites under the
patterns inventoried here; it remains in every authoritative-roster search so that zero is measured rather than
assumed.

## Accepted decision authority and current-source collision

Accepted ADR-095 is the current lifecycle-mechanics authority. Its status at
`docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:3-6` explicitly supersedes
ADR-094's managed-consumer, resumable running-Stop, drain-and-delete, name-routed child-catalog, and retained
repeated-result mechanics while leaving ADR-094 as immutable history. ADR-095 then separates terminal running Stop
from retryable failed-Start cleanup at `:10-18`, requires exact native owner handles and no deadline rejoin or result
replay at `:24-36`, removes Client child lifecycle/deletion authority at `:38-42`, and restates the superseded surface
at `:51-59`.

Current source has not yet converged to that accepted decision. It still contains the Client lifecycle catalog and
managed bindings at `natsclient/client.go:79-86,624-655`, Client-wide child stopping at `:566-598,769-781`, same-name
replacement and catalog insertion at `natsclient/stream.go:738-800`, resumable name-routed Stop and deletion at
`:1068-1125`, Stop-all at `:1128-1140`, and retained once/error/completion plus deadline rejoin in
`Subscription.Drain` at `natsclient/client.go:876-946`. These are current-source collisions with ADR-095, not
evidence that ADR-094 retains precedence and not a target disposition for how N1 performs convergence.

## Lifecyclejoin closure

Production Go imports and calls into `internal/lifecyclejoin` are zero at the inventory baseline. The closing search
was:

```text
rg -n '"github.com/c360studio/semstreams/internal/lifecyclejoin"|lifecyclejoin\.' \
  --glob '*.go' --glob '!**/*_test.go' .
=> 0 lines
```

The package nevertheless remains on disk:

- `internal/lifecyclejoin/generation.go:1-4` declares the package and its generation owner;
- `internal/lifecyclejoin/operation.go:1` declares its operation helper;
- `internal/lifecyclejoin/rollback.go:1` declares its rollback helper; and
- `internal/lifecyclejoin/generation_test.go:1` retains package tests.

Collision: production has converged to zero use while the implementation package and tests remain. This inventory
does not choose whether deletion is separate, combined with another change, or deferred.

### Adopter seam

- What must an adopter know now? Nothing about `internal/lifecyclejoin`; it is an internal package and has no legal
  external import path.
- What happens if they do nothing? No external source change follows from the current zero-use fact.
- Where do they find out? The package path and the zero-production-use search above are the discovery record.
- What should they have to know? Nothing. Any later disposition remains internal to SemStreams.

## Four port-consumption APIs and the local split

Four exported port-backed APIs coexist:

- `natsclient/stream.go:278-284` defines error-only `ConsumeStreamWithConfig` and delegates to the error-only contexts
  method;
- `natsclient/stream.go:510-578` defines exact-handle `ConsumeStreamWithConfigHandle`;
- `natsclient/stream.go:588-663` defines exact-handle `ConsumeStreamWithConfigContextsHandle`; and
- `natsclient/stream.go:679-688` defines error-only `ConsumeStreamWithConfigContexts`.

The exact-handle methods return `jetstream.ConsumeContext`; the canonical methods return only `error`. The canonical
path then creates a `consumerBinding` and records it in the Client catalog at `natsclient/stream.go:740-800`. The
handle path instead reserves the handle-free identity claim at `natsclient/stream.go:412-430`, returns the native
handle, and releases the claim only through the native-`Closed` cleanup at `natsclient/stream.go:496-500` or through
the equivalent port helper path.

The local production bridge census is 16 files. Fifteen reference the standard-context bridge and one references the
split-context bridge:

- `examples/processors/document/component.go:496`;
- `examples/processors/iot_sensor/component.go:496`;
- `output/file/file.go:404`;
- `output/httppost/httppost.go:403`;
- `output/websocket/websocket.go:1141`;
- `processor/agentic-dispatch/component.go:474`;
- `processor/agentic-governance/component.go:477`;
- `processor/agentic-loop/component.go:1015` (contexts bridge);
- `processor/agentic-model/component.go:408`;
- `processor/agentic-tools/component.go:404`;
- `processor/graph-ingest/component.go:1460`;
- `processor/json_filter/json_filter.go:366`;
- `processor/json_generic/json_generic.go:342`;
- `processor/json_map/json_map.go:388`;
- `processor/rule/processor.go:1155`; and
- `storage/objectstore/component.go:898`.

The closing local command was:

```text
rg -l 'ConsumeStreamWithConfig(Contexts)?Handle' \
  --glob '*.go' --glob '!**/*_test.go' examples output processor storage | sort
=> 16 files
```

Local canonical-call coverage is different from production owner coverage:

- `natsclient/consume_durable.go:47` is the only non-test production dot-call of the standard canonical method;
- `natsclient/stream.go:284` is the standard canonical method's internal delegation to the contexts method;
- no SemStreams production owner outside `natsclient` calls the error-only canonical methods directly; and
- `natsclient/consumer_policy_callsite_test.go:109-112` pins all four current signatures.

This is a callsite-census gap: a search only for canonical calls misses all 16 local owner bridge references. A search
only for bridge calls misses old-signature sister adopters and the tests that pin the canonical methods.

### Exact-handle internal consumption

A fifth exported creator is intentionally outside the four port-backed APIs:
`Client.ConsumeInternalStreamWithConfig` at `natsclient/stream.go:287-402`. It accepts no `PortConsumerContext`,
completes stream lookup, consumer creation/update, identity observation, and optional metrics setup before native
`Consume`, then returns the exact `jetstream.ConsumeContext` to its caller.

There are exactly three non-test production calls owned by two framework owners:

- AgentRun's one `milestoneConsumerOwner` acquires the complete and failed handles at
  `agentic/agentrun/agentrun.go:759,779`. Its Stop starts Drain on both, awaits both exact Closed signals while callback
  authority remains live, force-stops on a running Stop deadline, and then cancels at
  `agentic/agentrun/agentrun.go:586-654`.
- The MaxDelivery observer acquires one handle at `internal/maxdelivery/observer.go:252`. Its returned Stop drains,
  awaits exact Closed, force-stops on deadline, and then cancels at `internal/maxdelivery/observer.go:259-290`.

For a nonempty durable identity the creator reserves an opaque process-local claim before server consumer creation at
`natsclient/stream.go:332-340,412-431`. Every pre-commit return releases it through the deferred rollback at
`:334-340`; the acquisition commits at `:394`, and its closure releases only after the returned native handle closes
at `:397-400`. Release
is identity- and token-checked at `:665-673`, so an older owner cannot erase a newer claim. Empty durable identities
do not create a claim. The claim contains no handle, completion, Stop, deletion, or replay authority.

No authoritative sister production call was found:

```text
rg -n 'ConsumeInternalStreamWithConfig' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semteams ../semstreams-ui
=> 0 lines
```

The current capability specification already admits this method only for consumers with no `JetStreamPort` contract
and requires the named internal census at `openspec/specs/jetstream-consumer-policy/spec.md:54-64`. The pending change
delta strengthens that current claim with exact-handle-through-Closed ownership at
`openspec/changes/simplify-one-shot-lifecycle-ownership/specs/jetstream-consumer-policy/spec.md:60-74`. This is a
present spec fact and a pending delta fact, not a new target choice made by this inventory.

#### Adopter seam

- What must an adopter know now? A framework-internal non-port owner must know that this method returns its exact
  child, that Client Close does not manage it, and that the owner must keep callback authority live through Drain and
  exact Closed. No authoritative sister currently calls the method.
- What happens if they do nothing? The two named framework owners retain their three native handles and current
  durable identities. Sister source has no migration obligation for this method at the baseline.
- Where do they find out? The method comment, the two owner Stop implementations, the current capability spec, and
  the exact empty sister search above.
- What should they have to know? Only whether their consumer is non-port and the native handle they just acquired;
  they should not need Client catalogs, name-routed Stop, or hidden replay state.

### Authoritative old-signature port adopters

There are nine authoritative production calls across three sister repositories:

- SemSpec has six:
  - `../semspec/cmd/sandbox/qa_subscriber.go:70`;
  - `../semspec/processor/lesson-decomposer/component.go:271`;
  - `../semspec/processor/plan-decision-handler/component.go:162`;
  - `../semspec/processor/qa-reviewer/qa_completed.go:45`;
  - `../semspec/processor/researcher-manager/component.go:182`; and
  - `../semspec/processor/structural-validator/component.go:164`.
- SemDev has two:
  - `../semdev/internal/conversationchannel/component.go:475`; and
  - `../semdev/internal/intake/component.go:377`.
- SemDragon has one:
  - `../semdragon/processor/questtools/handler.go:32`.

All nine checked-out calls have the older `(ctx, cfg, handler)` shape and do not pass the current
`PortConsumerContext`. They are migration obligations, not evidence that those checkouts compile against this `HEAD`.

A raw checkout scan yields 27 calls: the nine authoritative calls above plus nine in each of two SemSpec worktrees.
The non-authoritative copies are:

- `../semspec-ui-bmad`: nine calls, including `cmd/sandbox/qa_subscriber.go:69` and eight processor calls; and
- `../semspec-ui-run-visibility`: nine calls, including `cmd/sandbox/qa_subscriber.go:70` and eight processor calls.

The closing distinction was:

```text
rg -n '\.ConsumeStreamWithConfig(Contexts)?\(' --glob '*.go' \
  --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
=> 9 lines

rg -n '\.ConsumeStreamWithConfig(Contexts)?\(' --glob '*.go' \
  ../semspec-ui-bmad ../semspec-ui-run-visibility
=> 18 lines
```

Collision: the local tree has exact-handle behavior under temporary names, canonical behavior under error-only names,
and sister code on an older no-owner shape. The count is 16 local bridge files, 9 authoritative sister calls, and 18
non-authoritative worktree copies; it is not 16, 27, or 43 interchangeable adopters.

### Adopter seam

- What must an adopter know now? They must know which of four method names they compile against, whether the method
  returns a handle, and whether their dependency version requires `PortConsumerContext`.
- What happens if they do nothing? Existing source continues to follow its pinned dependency. Any later signature or
  name change would be a compile-time migration, but this inventory chooses none.
- Where do they find out? `natsclient/stream.go:275-688`, the callsite signature test, and the migration guide are the
  present discovery surfaces.
- What should they have to know? Ideally one canonical acquisition method, one exact ownership rule, and no temporary
  naming history. That principle is seam evidence, not a target ruling in this artifact.

## Client child catalogs, identity claims, observation, and metrics

Four relevant maps and one raw-child slice in `natsclient`, plus one package-level map in `output/otel`, currently
carry different authority and must not be conflated.

### Lifecycle catalog

`natsclient/client.go:78-80` stores `consumers map[string]consumerBinding`. Each binding contains a native consume
context, a consumer handle, policy identity, and a shared drain record at `natsclient/client.go:624-657`.

The error-only port path performs same-name replacement at `natsclient/stream.go:738-751` and adds the new binding at
`natsclient/stream.go:796-800`. The catalog supports:

- exported `Client.OutstandingWork` at `natsclient/client.go:707-763`;
- exported `Client.StopConsumer` at `natsclient/stream.go:1067-1103`;
- exported `Client.StopAndDeleteConsumer` at `natsclient/stream.go:1105-1126`;
- exported `Client.StopAllConsumers` at `natsclient/stream.go:1128-1140`; and
- private Client-close cleanup at `natsclient/client.go:567-598,770-802`.

The same Client also stores raw core NATS children in `subs []*nats.Subscription` at `natsclient/client.go:76`.
Subscription acquisition appends raw children at `natsclient/client.go:977` and `natsclient/request.go:428`.
`Client.Close` calls `unsubscribeAll`, which enumerates and clears that slice at `natsclient/client.go:785-802`.

### Reject-only internal claims

`natsclient/client.go:83-86` stores `internalClaims`, keyed by the identity types at
`natsclient/stream.go:405-410`. Reservation at `natsclient/stream.go:412-430` stores only an empty claim token, not a
consumer handle, context, error, completion channel, Stop function, or deletion authority. Release is identity- and
token-checked at `natsclient/stream.go:665-673`.

This map rejects duplicate native-handle ownership. It is not the `Client.consumers` lifecycle catalog.

### Optional generic-consumer metrics observation

`natsclient/jetstream_metrics.go:15-37` has its own mutex-protected `consumers` map of `trackedConsumer` observation
handles. `trackConsumer` and `forgetConsumer` are at `natsclient/jetstream_metrics.go:219-243`. The comment at
`:230-232` explicitly says removal follows exact native `Closed` and carries no lifecycle authority.

The metrics map exists only when metrics are configured. `OutstandingWork` deliberately reads the unconditional
Client lifecycle catalog, not the optional metrics map; that distinction is documented at
`natsclient/client.go:731-741`.

### Optional consumer-policy observation catalogs and shared collectors

`jetstreamMetrics.policies` is a second, distinct metrics catalog at `natsclient/jetstream_metrics.go:35-37,124`, but
it is not process-wide or registry-wide. Each successful `WithMetrics` application creates a new `jetstreamMetrics`
owner and assigns it to that Client at `natsclient/options.go:207-220`; construction allocates a fresh `policies` map
and a fresh mutex-protected observation owner at `natsclient/jetstream_metrics.go:44-50,122-164`. Metrics-enabled
Clients therefore have independent policy maps, locks, record objects, and pollers.

The three policy `GaugeVec` collectors have different multiplicity. Construction initially creates candidates, then
`RegisterOrGetGaugeVec` replaces them with the registry's canonical collectors at
`natsclient/jetstream_metrics.go:101-112,149-158`. Two metrics owners using the same registry consequently retain the
same three collector pointers and write or delete the same label series. The shared-collector test proves collector
identity and one visible set of series at `natsclient/jetstream_metrics_test.go:16-79`; the current specification
requires that exact result at `openspec/specs/jetstream-consumer-policy/spec.md:131-140`.

Each per-Client `policies` map is keyed by component, port, stream, consumer, and policy source through
`consumerPolicyKey` at `natsclient/consumer_policy.go:36-42`. Each `consumerPolicyRecord` at
`natsclient/consumer_policy.go:22-34` retains:

- the component, port, stream, consumer, policy source, and requested `MaxAckPending` labels;
- a `consumerPolicyInfoReader` handle whose only admitted operation is `Info(context.Context)`;
- a logger;
- `available`, the result state of the latest policy observation; and
- `active`, the guard that prevents an in-flight poll of a forgotten or replaced record from recreating metrics.

This handle is observation authority, not a `jetstream.ConsumeContext`: the record exposes no Drain, Stop, Closed,
deletion, callback, or message-processing authority.

`trackPolicy` at `natsclient/jetstream_metrics.go:166-184` deactivates and removes an exact-key predecessor, activates
the new record, stores it, and publishes requested, effective, and available metrics. `forgetPolicy` at `:186-205`
deactivates the current record, removes it, and deletes all three metric series. The periodic `updateStats` poller
snapshots the map at `:257-272`, calls each record's `Info` at `:310-332`, and checks `active` under the record mutex
before publishing. A failed observation deletes the effective series and changes availability to zero while retaining
the active record for a later poll; a later successful `Info` restores the effective series and availability to one.

The replacement and recovery evidence is executable:

- `natsclient/jetstream_metrics_test.go:99-142` proves unavailable-to-available recovery without replacing the record;
- `natsclient/jetstream_metrics_test.go:193-222` proves forgetting cannot be undone by an in-flight refresh;
- `natsclient/jetstream_metrics_test.go:224-261` proves exact replacement cannot be overwritten by an in-flight old
  record; and
- `natsclient/jetstream_metrics_test.go:263-301` proves exact-key replacement preserves a sibling policy identity.

Those guards and tests are local to one `jetstreamMetrics` owner. If two Clients share one registry and track the
same policy key, each Client's map can retain an independently active record under an independent lock while both
records address the same canonical label series. `trackPolicy`, `forgetPolicy`, and `updateStats` synchronize only
through their receiver's `m.mu` and the receiver-local record's `active` guard at
`natsclient/jetstream_metrics.go:166-205,257-272,310-332`. One Client can therefore forget its record and delete all
three shared label series while the other Client's same-key record remains active. A later poll by the other Client
may recreate effective/available values but cannot recreate the requested series, which `updateStats` never writes.
Either Client may overwrite shared values, but there is no cross-Client record replacement, deletion guard, refresh
ordering, or recovery owner. The shared-collector test stops after writing and gathering through the second owner; it
does not track, forget, refresh, or recover the same key through both owners. The single-owner lifecycle tests at
`natsclient/jetstream_metrics_test.go:99-301` likewise provide no cross-Client collision coverage. This is a current
ownership collision and test gap, not a disposition.

Policy writers exist on both managed port-consumer paths and one direct-consumer path. Managed acquisition creates a
record through `observePortConsumerPolicy` at `natsclient/consumer_policy.go:102-136`; setup failures, exact native
Closed, catalog replacement, name-routed Stop, Stop-all, and Client Close call `forgetPolicy` through
`natsclient/consumer_policy.go:144-183`, `natsclient/stream.go:449-500,740-745,1093-1099,1132-1138`, and
`natsclient/client.go:770-781`.

The exported direct path is `Client.ObserveDirectPortConsumerPolicy` at
`natsclient/consumer_policy.go:186-208`. It performs initial observation and validation, writes the same optional
policy catalog, and returns an opaque cleanup that forgets the exact policy key. There is exactly one non-test
production selector reference, the default observer method value used by the OTEL exporter at
`output/otel/component.go:350-364`. The syntactic dot-call count is zero because OTEL invokes that method value through
its owner-local observer function. OTEL
publishes that cleanup into its owner-local `policyCleanups` slice when it starts the corresponding pull loop at
`:367-381`; partial acquisition rollback invokes already-created cleanups at `:271-277,317-340`.

### OTEL direct-consumer identity claims

`output/otel` has a separate process-wide reject-only map, `localOTELConsumerClaims.active`, at
`output/otel/component.go:85-98`. Its key is only `{stream, durable}` and its value is an opaque pointer token.
`reserveOTELConsumerClaim` at `:384-396` rejects a duplicate identity even when the competing OTEL components use
different NATS clients. `releaseOTELConsumerClaim` at `:398-407` deletes only when both identity and token still
match, so an older owner cannot release a newer claim. The map stores no NATS Client, consumer handle, context,
completion, cleanup function, status, Stop result, or deletion authority.

OTEL reserves the claim before `CreateOrUpdateConsumer` at `output/otel/component.go:302-327`. Every failure before
publication releases the current claim and rolls back prior subscriptions at `:271-340`. Successful publication
appends both the claim and the policy cleanup to the component's retained owner-local slices at `:343-381`; these
records remain paired with the component's pull-loop lifecycle rather than the NATS Client catalogs.

Successful `Component.Stop` copies the retained cleanup and claim records at `output/otel/component.go:610-614`,
cancels and joins the runtime, retires policy observation at `:624-629`, shuts down the exporter, releases every exact
claim at `:637-639`, and clears the retained slices at `:645-655`. The cross-client integration test at
`output/otel/component_lifecycle_integration_test.go:116-157` proves a duplicate is rejected without stopping the
incumbent and that a fresh component can reacquire only after the incumbent's completed Stop releases the identity.

A runtime-join deadline follows a different terminal path: `output/otel/component.go:616-621` calls
`finishTerminalStop(false)` and returns before policy cleanup or claim release. The component retains both slices,
marks its one-shot lifecycle done, and repeated Stop returns nil at `:582-592`; there is no later rejoin or cleanup
replay. `output/otel/component_test.go:94-123` pins the no-replay behavior for policy cleanup. Thus a deadline-retained
OTEL claim and policy record can reject or continue observing within the same process even after that component has
terminalized. This is current recovery evidence, not a disposition choice.

### OutstandingWork and independent agentic-loop observation

The exported `Client.OutstandingWork` declaration remains, but its production call count is zero. The current
graph-ingest observer performs an independent stream and consumer lookup at
`processor/graph-ingest/readiness.go:208-227`; it does not call `Client.OutstandingWork`. Current hits outside the
declaration and comments are tests in `natsclient/consumer_handle_integration_test.go:15-128`.

Agentic-loop has a second, production outstanding-work observer that also does not call `Client.OutstandingWork`.
`Component.consumerForSubject` resolves the requested subject against the exact stream and consumer binding recorded
by that component at `processor/agentic-loop/inflight.go:127-144`. `outstandingForSubject` then obtains the stream and
consumer, reads `Consumer.Info`, and computes `NumPending + NumAckPending` at
`processor/agentic-loop/inflight.go:146-188`. Missing bindings and unreadable server state are classified as unknown,
not zero.

The binding is published only after successful exact-handle consumer acquisition: setup appends the native handle to
the lifecycle owner and then appends `{streamName, consumerName, subject}` to `consumerInfos` at
`processor/agentic-loop/component.go:1015-1034`. Terminal lifecycle clearing removes both the retained handles and the
recorded observation bindings at `processor/agentic-loop/component.go:738-747`. The observation record therefore
contains discovery authority for the consumer actually bound by this component, but no independent Stop, Drain,
delete, callback, or result-replay authority.

The current agentic-loop capability specification (requirement heading at
`openspec/specs/agentic-loop/spec.md:77`, normative body at `:78-216`) requires deployment-addressed request/reply
without caller-side stream or consumer-name reconstruction, exact recorded subject-to-consumer binding, and
`NumPending + NumAckPending` bookkeeping. It also requires an
absent or unreadable observation to remain unknown rather than render as no work. This is current authority for the
independent observer and its recovery behavior, not a target disposition for `Client.OutstandingWork`.

Closing search:

```text
rg -n '\.OutstandingWork\(' --glob '*.go' --glob '!**/*_test.go' .
=> 0 lines

rg -n '\.OutstandingWork\(' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
=> 0 lines
```

Collision: `Client.consumers` owns lifecycle, same-name replacement, name-routed stopping, observation, and Client
Close cleanup in one value. `internalClaims` and `localOTELConsumerClaims.active` independently own duplicate
rejection at different package and client scopes. Each metrics-enabled Client has its own
`jetstreamMetrics.consumers` generic-observation map and `jetstreamMetrics.policies` policy-observation map, while
Clients configured with the same registry share canonical collector series. Agentic-loop's `consumerInfos` is a
separate owner-local discovery record for the actual consumer it bound; it reads server state but cannot stop the
consumer. A count or design that collapses these maps, or treats shared collector identity or a recorded observation
binding as shared lifecycle ownership, would assign lifecycle, rejection, observation, deletion, or recovery
authority incorrectly.

### Adopter seam

- What must an adopter know now? A caller using name-routed APIs must predict and repeat a stream/durable identity.
  A caller using exact handles must retain that handle and must not expect `internalClaims` or metrics to stop it.
  An agentic-loop in-flight caller supplies deployment and task-subject selectors, not a stream or consumer name; the
  component resolves the exact recorded binding and reports server bookkeeping or unknown.
- What happens if they do nothing? Current error-only consumers remain Client-catalog children; exact-handle consumers
  remain caller children. OTEL direct consumers remain OTEL-owned, with process-wide exact-identity rejection and
  owner-local policy cleanup. Agentic-loop retains its owner-local observation binding for the same lifetime as its
  acquired consumer handles. Client Close currently also unsubscribes raw core NATS children.
- Where do they find out? The declarations and method surfaces above; the distinction is not represented by one
  unified public ownership document today. Agentic-loop's request/reply and unknown-state contract is separately
  authoritative in `openspec/specs/agentic-loop/spec.md:78-216`.
- What should they have to know? Ideally the framework should not make them predict names that the acquired resource
  already knows, or confuse policy availability with lifecycle status. This inventory records that seam pressure
  without choosing which APIs remain.

## Subscription state and holder census

The exported wrapper stores:

- native subscription and native closed channel at `natsclient/client.go:877-890`;
- `sync.Once` at `natsclient/client.go:880`;
- retained `drainErr` and `drainComplete` at `natsclient/client.go:881-882`; and
- construction-time `StatusChanged(SubscriptionClosed)` at `natsclient/client.go:886-890`.

`Subscription.Drain(ctx)` at `natsclient/client.go:904-946` starts native Drain through the retained once, stores its
result, waits on the retained closed channel, and permits a later call to rejoin after an earlier caller deadline. The
current contract is explicit in its comment at `natsclient/client.go:901-903` and in tests:

- completed repeat behavior at `natsclient/subscription_test.go:86-87,106-107,184`;
- native-error replay at `natsclient/subscription_test.go:117`;
- deadline followed by later rejoin at `natsclient/subscription_test.go:126-159`; and
- concurrent result sharing at `natsclient/subscription_test.go:162-184`.

The same exported signature can therefore hide a semantic break even when downstream code still compiles.

### Twenty-six local production Drain files

The closing local search found 26 production files containing the context-bearing Drain seam or a direct
`sub.Drain(ctx)` use:

- examples: `examples/processors/document/component.go:378`,
  `examples/processors/iot_sensor/component.go:378`, and
  `examples/processors/weather_station/component.go:308`;
- outputs: `output/file/file.go:135,506`, `output/httppost/httppost.go:135,509`, and
  `output/websocket/websocket.go:199,922`;
- agentic: `processor/agentic-loop/component.go:118,681,692` and
  `processor/agentic-tools/component.go:86,560`;
- graph: `processor/graph-clustering/component.go:1184`, `processor/graph-embedding/component.go:829`,
  `processor/graph-index-spatial/component.go:633`, `processor/graph-index-temporal/component.go:655`,
  `processor/graph-index/component.go:796`, `processor/graph-ingest/component.go:640,1085`, and
  `processor/graph-query/component.go:644`;
- JSON: `processor/json_filter/json_filter.go:499`, `processor/json_generic/json_generic.go:455`, and
  `processor/json_map/json_map.go:501`;
- research: `processor/research-graph-assess/component.go:345`,
  `processor/research-graph-classify/component.go:418`,
  `processor/research-graph-execute/component.go:348`,
  `processor/research-graph-route/component.go:365`, and
  `processor/research-graph-synthesize/component.go:324`;
- rule and service: `processor/rule/processor.go:1298` and
  `service/message_logger.go:167,483,557,901`; and
- storage: `storage/objectstore/component.go:95,606`.

The exact counting command was:

```text
rg -l 'Drain\(context\.Context\) error|\.Drain\(ctx\)' \
  --glob '*.go' --glob '!**/*_test.go' . | sort
=> 26 files
```

### Twelve authoritative sister type-holder files

Twelve production files in authoritative sister roots explicitly mention `natsclient.Subscription`:

- `../semboids/internal/sim/component.go:140`;
- `../semdev/internal/station/station.go:243`;
- `../semdragon/processor/autonomy/component.go:91-92`;
- `../semdragon/processor/questbridge/component.go:118`;
- `../semops/internal/app/runtime.go:82`;
- `../semsage/tools/spawn/executor.go:34`;
- `../semsource/processor/code-context/component.go:113`;
- `../semsource/processor/source-manifest/component.go:65,75-76,86,90,96,563`;
- `../semsource/processor/source-manifest/ingest.go:127`;
- `../semsource/processor/supersession/component.go:43-45,146`;
- `../semspec/processor/recovery-consumer/component.go:74`; and
- `../semspec/processor/workflow-validator/component.go:42`.

The authoritative-roster search found no production `.Drain(` calls in any of the 11 roots; present owner code
predominantly calls `Unsubscribe`. The 12-file count is therefore an explicit-type-holder census, not a claim that 12
external sites exercise the current rejoin semantics. Inferred-type uses may exist outside this explicit-type census.

```text
rg -l '\*natsclient\.Subscription|natsclient\.Subscription' \
  --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams | sort
=> 12 files

rg -n '\.Drain\(' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
=> 0 lines
```

Collision: 26 local files exercise or expose context-bearing Drain, 12 sister files explicitly hold the wrapper type,
and Client separately retains the raw native subscriptions for Close. These are three overlapping populations, not
one call count.

### Adopter seam

- What must an adopter know now? A Drain caller must know that the wrapper stores a once, error, completion state, and
  later-rejoin authority. A holder that only calls Unsubscribe need not know those details.
- What happens if they do nothing? Present calls retain current replay and rejoin behavior. A same-signature semantic
  change would not produce a compiler diagnostic.
- Where do they find out? The `Subscription.Drain` comment, unit tests, and migration documentation are the only
  reliable discovery surfaces for a same-signature change.
- What should they have to know? They should know the owner-level shutdown action and outcome, not wrapper-internal
  synchronization or historical result retention.

## ConsumeDurable adopters, interfaces, and observability coupling

SemStreams currently defines `Client.ConsumeDurable` at `natsclient/consume_durable.go:34-59`. It returns only error,
delegates acquisition to the error-only canonical port API at `:47`, and logs every handler error through the package
logger at `:54-57` after `ConsumeWithHeartbeat` has performed settlement.

The current `gated-dag-dispatch` capability specification independently requires a typed durable at-least-once
consume primitive at `openspec/specs/gated-dag-dispatch/spec.md:43-64`: its typed handler receives bytes, nil ACKs,
error NAKs with delay, and `InProgress` heartbeats keep long-running work from redelivery. That requirement is broader
than the present `ConsumeDurable` method name and error-only acquisition shape. A later N1 design must preserve it or
change it explicitly; deleting or renaming the current helper would not by itself retire the current capability
requirement. This records authority and collision only, not the replacement surface.

### Ten authoritative production calls

The authoritative sister census has ten production calls:

- SemMachina has eight:
  - `../semmachina/internal/accusation/consumer.go:75`;
  - `../semmachina/internal/caseflow/consumer.go:65`;
  - `../semmachina/internal/egress/notifier.go:161`;
  - `../semmachina/internal/knowledge/consumer.go:92`;
  - `../semmachina/internal/ledger/writer.go:218`;
  - `../semmachina/internal/stage/loopfailure.go:337`;
  - `../semmachina/internal/stage/runner.go:217`; and
  - `../semmachina/internal/turn/intake.go:216`.
- SemSpec has one at `../semspec/processor/execution-bridge/gated_dag_dispatch.go:36`.
- SemDragon has one at `../semdragon/questdag/component.go:294`.

Closing search:

```text
rg -n '\.ConsumeDurable\(' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
=> 10 lines
```

Like the nine port callers, these sister calls use an older no-owner method shape. The census measures source
migration impact, not compatibility with this `HEAD`.

### Seven SemMachina interfaces and global shutdown

Seven SemMachina interfaces return only error from `ConsumeDurable`:

- `../semmachina/internal/accusation/consumer.go:29-31`;
- `../semmachina/internal/caseflow/consumer.go:27-29`;
- `../semmachina/internal/egress/notifier.go:85-91`;
- `../semmachina/internal/knowledge/consumer.go:29-31`;
- `../semmachina/internal/ledger/writer.go:61-67`;
- `../semmachina/internal/stage/runner.go:51-57`; and
- `../semmachina/internal/turn/intake.go:63-69`.

The stage loop-failure watcher shares the stage `Consumer` interface rather than declaring an eighth interface.

SemMachina boot defines `clientShutdown.StopAllConsumers` at
`../semmachina/internal/boot/engine.go:101-105` and invokes it at `:330` after component stop. Its comment at
`:323-329` states that the global call catches consumers bound directly by the engine. This is a shutdown ownership
dependency beyond the ten acquisition calls.

### Current log-observability dependency

SemMachina's Intake documentation says retries are observable because consumer counters remain non-zero and
`ConsumeDurable` logs a warning on every attempt at `../semmachina/internal/turn/intake.go:193-198`. Its terminal
refusal analysis at `:246-260` further says a Term outcome surfaces as one `ConsumeDurable` warning and an unsubscribed
server advisory. The implementation that supplies those warnings is `natsclient/consume_durable.go:54-57`.

Any inventory that counts only call signatures misses an operator-visible behavior relied on by adopter reasoning.
This artifact records the dependency without deciding where that logging should live.

### Adopter seam

- What must an adopter know now? Work handlers return settlement intent indirectly: nil acknowledges, terminating
  errors terminate, and other errors are retried while the wrapper owns heartbeat and warning emission.
- What happens if they do nothing? Their pinned wrapper retains current settlement and logging. A future surface
  change could affect acquisition ownership, shutdown, and warning behavior even if handler bodies are unchanged.
- Where do they find out? `natsclient/consume_durable.go`, ADR-070, the seven narrow interfaces, SemMachina Intake's
  operator comments, and the migration guide.
- What should they have to know? Ideally only the work-result contract and exact acquired owner handle; settlement
  arithmetic, global catalogs, and logging side effects should be explicit framework behavior.

## Heartbeat validation defects and BackOff collision

`StreamConsumerConfig` declares:

- `AckWait` at `natsclient/stream.go:40-42`; and
- `BackOff` at `natsclient/stream.go:57-60`, documented as overriding AckWait per retry attempt.

`buildConsumerConfig` supplies a 30-second fallback for nonpositive AckWait at `natsclient/stream.go:871-876` and
copies any configured BackOff to the server at `:885-888`.

The current durable validator at `natsclient/consume_durable.go:67-81` has two independent defects:

1. It evaluates only `cfg.AckWait`, even when nonempty `cfg.BackOff` is sent to the server and overrides AckWait.
   Therefore its accepted heartbeat relation can differ from the server's actual retry timing.
2. It compares `heartbeat*2 > effectiveAckWait` at `:75`. `time.Duration` is signed `int64`; sufficiently large
   positive heartbeat values can overflow the multiplication and be accepted because the product wraps.

Current unit coverage at `natsclient/consume_durable_test.go:9-32` includes ordinary valid, invalid, and zero-AckWait
cases. It does not cover BackOff interaction or multiplication overflow.

The current capability specification separately requires creation-time heartbeat validation at
`openspec/specs/gated-dag-dispatch/spec.md:66-77`: an interval not safely below AckWait must fail fast and the error
must name both values. The implementation at `natsclient/consume_durable.go:62-81` is the current attempt to satisfy
that requirement, but its BackOff omission and overflow-prone multiplication mean the specification and source are
not equivalent evidence. A later N1 design must preserve this validation contract or change it explicitly; this
inventory does not select the effective-timer formula or the API that owns validation.

Collision: configuration says BackOff overrides AckWait, server construction honors BackOff, and durable validation
still validates only AckWait with overflow-prone arithmetic. This is current-code defect evidence, not a choice of
replacement formula or API.

### Adopter seam

- What must an adopter know now? They must predict which server timer governs a delivery and avoid duration values
  that expose overflow in framework validation.
- What happens if they do nothing? Ordinary configurations may work, but BackOff configurations can pass validation
  against a timer the server does not actually use; overflow-scale values can also pass incorrectly.
- Where do they find out? The `StreamConsumerConfig` field comments, `buildConsumerConfig`, and the durable validator.
- What should they have to know? They should supply intent and receive validation against the effective server policy,
  without duplicating precedence or arithmetic rules.

## Configuration and generated-consumer surface

Five local production Go fields accept `delete_consumer_on_stop`:

- `output/otel/config.go:50-51`;
- `processor/agentic-dispatch/config.go:18`;
- `processor/agentic-loop/config.go:54`;
- `processor/agentic-model/config.go:16`; and
- `processor/agentic-tools/config.go:18`.

The only non-test production hits are these declarations. There is no production read of any field, so all five are
currently inert. Tests still construct or decode them in:

- `processor/agentic-loop/loop_integration_test.go:35,79` and
  `processor/agentic-loop/recovery_integration_test.go:62`;
- `processor/agentic-model/lifecycle_integration_test.go:26` and
  `processor/agentic-model/model_integration_test.go:122,265,439,584`; and
- `processor/agentic-tools/slice_f2_integration_test.go:38` and
  `processor/agentic-tools/startup_atomic_integration_test.go:118`.

Five local generated schemas expose the property:

- `schemas/otel-exporter.v1.json:19`;
- `schemas/agentic-dispatch.v1.json:33`;
- `schemas/agentic-loop.v1.json:87`;
- `schemas/agentic-model.v1.json:13`; and
- `schemas/agentic-tools.v1.json:29`.

### Authoritative downstream generated consumers

SemStreams UI carries all five schema copies plus one generated TypeScript field per schema:

- `../semstreams-ui/contracts/semstreams/schemas/otel-exporter.v1.json:19`;
- `../semstreams-ui/contracts/semstreams/schemas/agentic-dispatch.v1.json:33`;
- `../semstreams-ui/contracts/semstreams/schemas/agentic-loop.v1.json:87`;
- `../semstreams-ui/contracts/semstreams/schemas/agentic-model.v1.json:13`;
- `../semstreams-ui/contracts/semstreams/schemas/agentic-tools.v1.json:29`; and
- `../semstreams-ui/src/lib/types/api.generated.ts:2844,3572,3838,4099,7949`.

SemSpec carries the generated field in
`../semspec/ui/src/lib/types/semstreams.generated.ts:3158,3212,3544,3612,3675,3744,4228,4291,5047`.

SemTeams carries four schema copies and generated TypeScript uses:

- `../semteams/schemas/agentic-dispatch.v1.json:33`;
- `../semteams/schemas/agentic-loop.v1.json:87`;
- `../semteams/schemas/agentic-model.v1.json:13`;
- `../semteams/schemas/agentic-tools.v1.json:29`; and
- `../semteams/ui/src/lib/types/api.generated.ts:2510,2562,2863,2913,2974,2999,3469,3532,3855`.

The generated TypeScript files contain more hits than the five source schemas because the generated API/type model
projects component config through multiple schemas and response shapes. Counts must preserve that distinction.

The generated/configuration census used the full authoritative roster, not only the three roots with nonzero copied
or generated results:

```text
rg -n 'DeleteConsumerOnStop|delete_consumer_on_stop' \
  --glob '*.go' --glob '*.json' --glob '*.ts' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
```

### Authoritative SemDragon configuration consumers

SemDragon carries two additional production configuration fields that are not generated copies of the five
SemStreams schemas:

- `../semdragon/processor/questtools/config.go:33` declares `DeleteConsumerOnStop`. No non-test production read exists
  under `processor/questtools`; tests set it at
  `../semdragon/processor/questtools/component_test.go:90,564,759`. It is an authoritative inert adopter field.
- `../semdragon/processor/questbridge/config.go:48-50` declares and documents the same JSON property.
  `../semdragon/processor/questbridge/handler.go:463-469` actively reads it on the `stopChan` path and directly calls
  `jetstream.JetStream.DeleteConsumer` with the derived stream and durable names under a new five-second background
  context. Tests enable the behavior at
  `../semdragon/processor/questbridge/component_test.go:56,586,643`. It is an authoritative active lifecycle-deletion
  consumer, not a copied schema or generated type.

The two SemDragon fields therefore add two adopter configuration declarations, six test assignments, one production
read, and one direct durable-delete call. They do not change the local cardinalities of five Go fields and five local
schema properties.

Collision: SemStreams runtime behavior is inert, while accepted Go JSON shape, generated schemas, copied schemas, and
generated TypeScript still expose the property. SemDragon independently has one inert declaration and one active
Stop-time deletion path. A local runtime no-op does not erase source, validation, generated, or active sister
lifecycle consumers.

### Adopter seam

- What must an adopter know now? In SemStreams and SemDragon questtools, authors can provide a property whose name
  promises deletion but whose production value is never read. In SemDragon questbridge, the same property directly
  deletes the derived durable from production Stop code.
- What happens if they do nothing? Current validation and generated clients continue to accept and expose the inert
  property. SemDragon questtools retains a no-op promise, while questbridge continues deleting its derived durable on
  the configured Stop path. Any later schema or API removal would require configuration, generated-client, and active
  questbridge lifecycle synchronization.
- Where do they find out? Go schema tags, the five local schemas, copied downstream schemas, generated types, and a
  migration note. SemDragon owners must also inspect the two package-local config declarations and questbridge's
  direct deletion branch because those fields do not originate in a copied SemStreams schema.
- What should they have to know? A configuration surface should have one discoverable effect; callers should not need
  repository archaeology to learn that the same property is inert in one owner and lifecycle-active in another.

## Fixture deletion seam is absent

The current exported deletion surface is `Client.StopAndDeleteConsumer` at
`natsclient/stream.go:1105-1126`. Its comment names test cleanup, but a search of SemStreams tests finds no call to that
method. The only test-side `DeleteConsumer` hit is a fake method declaration at
`natsclient/getstream_circuit_breaker_test.go:135`.

The integration tests instead set the inert configuration fields listed above. There is no private fixture helper
that records the exact stream and durable identities created by one fixture and deletes only those identities during
cleanup.

The authoritative SemDragon scan does not supply that missing private seam. Questtools only sets its inert production
field from tests. Questbridge tests set an active production configuration flag, causing production Stop code to
re-derive and directly delete the durable identity; the tests do not retain a fixture-private deletion owner.

Closing searches:

```text
rg -n 'StopAndDeleteConsumer|DeleteConsumer\(' --glob '*_test.go' .
=> natsclient/getstream_circuit_breaker_test.go:135 (fake declaration only)

rg -n 'fixture.*DeleteConsumer|DeleteConsumer.*fixture' \
  --glob '*.go' test natsclient processor output storage service
=> 0 lines

rg -n 'DeleteConsumerOnStop|delete_consumer_on_stop' \
  --glob '*.go' --glob '*.json' --glob '*.ts' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
=> 36 lines: 2 SemDragon production declarations, 1 field comment, 1 production read, and 32 generated/copy hits

rg -n 'DeleteConsumer\(' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
=> 1 line: SemDragon questbridge's direct production deletion
```

Collision: an exported name-routed production method is documented for test cleanup, test configurations still claim
cleanup through inert fields, SemDragon questbridge performs configured production deletion for tests, and no
identity-scoped fixture seam exists. This inventory does not select a fixture API or deletion policy.

### Adopter seam

- What must an adopter know now? Test authors must know server stream and durable names and arrange cleanup outside an
  existing private fixture abstraction.
- What happens if they do nothing? Durable fixture state can survive tests even when an inert cleanup flag is true;
  questbridge instead deletes through its production Stop path without a fixture-private ownership record.
- Where do they find out? The exported method comment, config declarations, and integration test setup; there is no
  single fixture-owned discovery surface.
- What should they have to know? Ideally nothing beyond resources their fixture created; deletion should never require
  prediction, wildcard discovery, or authority over neighboring identities.

## Collision register

| Dimension | Current evidence | Collision that must remain distinct |
|---|---|---|
| Catalogs | `Client.consumers` stores lifecycle bindings at `natsclient/client.go:78-80,624-657`; `Client.subs` stores raw core subscriptions at `natsclient/client.go:76,785-802`; `internalClaims` stores only identity/token at `natsclient/client.go:83-86` and `natsclient/stream.go:405-430`; every metrics-enabled Client gets fresh `jetstreamMetrics.consumers` and `jetstreamMetrics.policies` maps at `natsclient/options.go:207-220` and `natsclient/jetstream_metrics.go:33-50,122-164`; process-wide `localOTELConsumerClaims.active` stores OTEL identity/token at `output/otel/component.go:85-98,384-407`; and agentic-loop records owner-local subject/stream/consumer observation bindings in `consumerInfos` at `processor/agentic-loop/component.go:1024-1034`. Clients using one registry share the three canonical policy collectors, not their maps or locks, at `natsclient/jetstream_metrics.go:149-158`. | These store classes have lifecycle, raw-child, Client-scoped duplicate-rejection, per-Client generic observation, per-Client policy observation, OTEL process-scoped duplicate-rejection, and agentic-loop exact-binding discovery authority respectively. Metrics catalog cardinality follows configured Clients, while policy collector cardinality follows registries. Overlapping keys and shared label series do not make the per-Client catalogs one owner; the agentic-loop binding can discover server state but cannot stop its consumer. |
| Status | `Client.Status` is connection/circuit state at `natsclient/client.go:201-245`; `consumerPolicyRecord.active` means the exact record remains current, while `available` means its latest `Info` observation succeeded at `natsclient/consumer_policy.go:22-34` and `natsclient/jetstream_metrics.go:310-332`; server consumer state is read through `Consumer.Info`. | No process-local consumer-owner status registry was found. The search `rg -n 'consumer(Status\|State)\|status.*consumer\|consumer.*status' natsclient --glob '*.go' --glob '!**/*_test.go'` finds comments, metrics/server observations, and connection guards, but no additional owner-status catalog. Connection health, policy-record currency, policy availability, server state, and owner lifecycle are not substitutes. |
| Lifecycle | Error-only canonical port consumption writes `consumerBinding` and supports replacement, Stop, Stop-and-delete, Stop-all, OutstandingWork, and Client Close at `natsclient/stream.go:679-801,1067-1140` and `natsclient/client.go:567-598,707-802`. Exact-handle bridges and the internal creator return native handles; their owners Drain/await Closed. Subscription separately retains once/error/completion at `natsclient/client.go:877-946`. OTEL directly owns Fetch loops and retains its policy cleanups and claims at `output/otel/component.go:45-69,343-381`. | Client-catalog lifecycle, exact owner lifecycle, Subscription retained-result lifecycle, and OTEL direct-consumer lifecycle coexist. Neither metrics catalog nor either claim map can Stop a child. Production lifecyclejoin use is zero while four package/test files remain. The 26 local Drain files, 12 sister wrapper holders, and zero production lifecyclejoin lines are different populations. |
| Ownership | Canonical error-only port calls publish no handle and make Client the child owner. Sixteen local bridge files retain returned handles. `ConsumeInternalStreamWithConfig` has three calls under two owners. OTEL has the one production selector reference to `ObserveDirectPortConsumerPolicy`; it owns the direct consumer's pull loop, opaque policy cleanup, and exact local claim. Metrics records retain only `Info` observation handles. Agentic-loop retains its exact consume handle in `consumers` and separately publishes the matching subject/stream/consumer discovery record in `consumerInfos` at `processor/agentic-loop/component.go:1015-1034`; `clearLifecycleHandles` clears both at `:738-747`. | API name, returned value, and actual Stop authority disagree across paths. Nine authoritative sister port calls use an older no-owner signature; 18 worktree copies are not additional owners. Policy cleanup retires observation but does not stop the OTEL consumer, and claim release permits reacquisition but does not stop it. Agentic-loop's observation binding avoids caller prediction but does not own lifecycle separately from the retained exact handle. |
| Readers | `OutstandingWork` reads `Client.consumers` at `natsclient/client.go:746-763`; name Stop reads it at `natsclient/stream.go:1079-1101`; each metrics owner snapshots only its own observation maps at `natsclient/jetstream_metrics.go:257-332`, although registry exposition reads the shared canonical label series; internal and OTEL claim reservation read only exact duplicate presence at `natsclient/stream.go:419-430` and `output/otel/component.go:384-395`; agentic-loop resolves subject to its recorded stream/consumer binding and reads `Consumer.Info` at `processor/agentic-loop/inflight.go:127-188`. | `rg -n '\.OutstandingWork\(' --glob '*.go' --glob '!**/*_test.go' .` is empty, as is the same authoritative-sister-roster search. The graph-ingest readiness reader independently resolves server stream/consumer state at `processor/graph-ingest/readiness.go:208-227`; agentic-loop independently reports `NumPending + NumAckPending` or unknown from its actual binding. One Client's metrics reader cannot see or guard another Client's record despite both publishing to the same collector series. Generic observation, policy observation, duplicate checks, and agentic-loop discovery authority cannot be counted as lifecycle readers. |
| Writers | The error-only path replaces/deletes/adds lifecycle bindings at `natsclient/stream.go:740-800`; Close and Stop methods clear them at `natsclient/client.go:770-781` and `natsclient/stream.go:1093-1138`. Subscription acquisition appends raw children at `natsclient/client.go:977` and `natsclient/request.go:428`. Internal claims reserve/release at `natsclient/stream.go:412-430,665-673`; generic metrics track/forget at `natsclient/jetstream_metrics.go:219-243`; each Client's policy records track/forget/poll under receiver-local locks at `natsclient/jetstream_metrics.go:166-205,257-332`, while same-registry Clients write and delete shared series; direct observation supplies an OTEL-owned cleanup at `natsclient/consumer_policy.go:186-208`; OTEL claims reserve/publish/release at `output/otel/component.go:317-381,384-407,610-655`; and agentic-loop appends its actual subject/stream/consumer observation binding only after exact-handle acquisition, then clears all such bindings with terminal lifecycle handles at `processor/agentic-loop/component.go:1015-1034,738-747`. | These writer families mutate store instances under different locks and cleanup triggers. One Client can delete a shared policy label series while another Client retains an active same-key record; neither Client's `active` guard orders the other's refresh or deletion. Metrics deletion, either claim release, and agentic-loop observation-binding mutation do not stop a consumer; lifecycle-catalog deletion does. Agentic-loop lifecycle owns the handle and observation record together, while the record itself has discovery authority only. OTEL's successful Stop invokes policy cleanup and claim release, while its runtime-join deadline terminalizes without either write. |
| Recovery and rebind | The error-only port path stops and removes an existing same-name local binding before `CreateOrUpdateConsumer` at `natsclient/stream.go:738-758`. Native durable state remains server-side and exact-handle owners normally Drain without deleting it. Within one metrics owner, policy polling retains active records across `Info` failure and restores availability/effective state on later success; exact replacement deactivates the predecessor so an in-flight old poll cannot overwrite the replacement at `natsclient/jetstream_metrics.go:166-205,310-332`. Subscription permits a later caller to rejoin a prior Drain after deadline. Agentic-loop publishes its actual observation binding only after successful handle acquisition and clears it with terminal lifecycle handles at `processor/agentic-loop/component.go:738-747,1015-1034`; query failure or absence remains unknown rather than idle at `processor/agentic-loop/inflight.go:157-188`. | Same-name local replacement, server durable rebind, single-owner policy-observation recovery, single-owner exact record replacement, retained local result rejoin, and agentic-loop fail-closed observation are different recovery behaviors. Across Clients sharing collectors, no replacement or recovery owner prevents one Client's delete or refresh from erasing or overwriting another active record's series, and current tests do not cover that collision. OTEL completed Stop enables exact-identity reacquisition, while a runtime-join deadline retains claims and policy cleanup without a later Stop rejoin. Agentic-loop restart does not reconstruct names or trust stale loop state; a running responder uses its newly recorded binding and server bookkeeping, while no responder is unknown. `ConsumeDurable` adds 10 sister acquisitions, seven SemMachina error-only interfaces, one `StopAllConsumers` boot seam, and warning-log coupling; none is represented by the local production-call count of zero. |
| Process-local claim recovery | A durable internal/handle acquisition reserves a Client-local opaque token before server creation, rolls it back on every pre-commit return, and releases it after exact native Closed at `natsclient/stream.go:332-340,397-400,412-430,665-673`; Client Close neither reads nor clears it. OTEL reserves a separate process-wide stream/durable token before direct consumer creation, publishes it into the component owner, rolls it back on pre-publication failure, and releases it after successful joined Stop at `output/otel/component.go:271-340,343-407,610-655`. | Neither map has rejoin, rediscovery, lease, persistence, or manual-clear authority. Process death discards both. Same-process internal recovery depends on original native Closed. Same-process OTEL recovery depends on completed owner Stop; a runtime-join deadline terminally retains the claim and repeated Stop cannot release it. These current behaviors are evidence, not a disposition. |
| Internal consumer | `ConsumeInternalStreamWithConfig` is an exact-handle non-port creator at `natsclient/stream.go:287-402`. AgentRun has two calls owned by one milestone owner; MaxDelivery has one call owned by one observer Stop. | The count is three calls but two lifecycle owners. The authoritative sister search is exactly empty. This surface is distinct from four port-backed APIs, 16 local port bridge files, and `ConsumeDurable`. |
| Settlement and retry policy | Current `openspec/specs/gated-dag-dispatch/spec.md:43-77` requires a typed bytes-handler durable-consume primitive with ACK/NAK settlement, `InProgress` heartbeat protection, and fail-fast heartbeat/AckWait validation naming both values. `ConsumeDurable` composes `ConsumeWithHeartbeat` and warning logs at `natsclient/consume_durable.go:34-59`. `buildConsumerConfig` applies BackOff at `natsclient/stream.go:871-888`, while validation checks only AckWait using overflow-prone `heartbeat*2` at `natsclient/consume_durable.go:67-81`. | Ten sister calls, seven SemMachina interfaces, one global-stop seam, and operator-visible warning reasoning survive despite zero SemStreams production callers. BackOff server precedence and the validator's AckWait arithmetic are independent defects, not one count. The current capability requirement is not identical to the current helper name or acquisition shape and must be preserved or explicitly changed by a later design; this register selects neither. |
| Configuration and deletion | SemStreams has five inert Go fields and five local schema properties, with copied/generated consumers in SemStreams UI, SemSpec, and SemTeams. SemDragon adds questtools' inert field plus three test assignments and questbridge's active field/read/direct durable deletion plus three test assignments. The SemStreams fixture-helper search is empty. | Local inert runtime, accepted/generated configuration, sister inert configuration, active sister production deletion, and test setup are five distinct consumers. No fixture-private identity owner was found; the exported name-routed deletion method and questbridge's configured production branch are not such a helper. |
| Current specs and ADRs | Current `openspec/specs/jetstream-consumer-policy/spec.md:37-64` requires policy context for the three named port operations and explicitly admits the internal method only for non-port consumers. Current `openspec/specs/gated-dag-dispatch/spec.md:43-77` requires typed durable consume, ACK/NAK settlement, heartbeat protection, and fail-fast heartbeat/AckWait validation. Current `openspec/specs/agentic-loop/spec.md:78-216` requires deployment-addressed in-flight observation from the exact recorded subject/stream/consumer binding, `NumPending + NumAckPending`, and unknown rather than zero when state is absent or unreadable. ADR-070 records the historical `ConsumeDurable` design at `docs/adr/070-gated-dag-durable-dispatch.md:88-185`. ADR-094 records exact owner handles, transport-only Client Close, and `ConsumeDurable` retirement at `docs/adr/094-boot-only-composition-and-observable-rule-activation.md:94-112,163-164`. Accepted ADR-095 explicitly supersedes ADR-094's managed-consumer, resumable running-Stop, drain-and-delete, name-routed child-catalog, and retained-result mechanics at `docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:3-6`, then requires one-shot running lifecycle and separates failed-Start cleanup authority at `:10-42,51-59`. The pending change delta specifies handle-return canonical methods and retained internal exact ownership at `openspec/changes/simplify-one-shot-lifecycle-ownership/specs/jetstream-consumer-policy/spec.md:1-74`. | Current source still has error-only canonical methods, temporary handle bridges, Client child catalogs, resumable name-routed Stop/delete, retained-result Subscription Drain, and `ConsumeDurable` at `natsclient/client.go:79-86,566-598,624-655,769-781,876-946`, `natsclient/stream.go:679-800,1068-1140`, and `natsclient/consume_durable.go:34-81`. Agentic-loop independently implements its current observation authority at `processor/agentic-loop/inflight.go:127-188` and `processor/agentic-loop/component.go:738-747,1015-1034`; it is not a `Client.OutstandingWork` caller. ADR-095 has precedence over the listed ADR-094 lifecycle mechanics; ADR-094 and ADR-070 remain historical context. The current capability specs, accepted decision, pending delta, and implementation describe distinct authority and implementation facts. The typed durable-consume, heartbeat, and exact agentic-loop observation requirements must be preserved or explicitly changed by a later design, but this inventory selects no disposition. |

This register is an evidence index, not a target disposition table. Its dimensions intentionally prevent one shared
name, key, handle, or count from collapsing distinct status, lifecycle, ownership, observation, and recovery facts.

## Reproduction and closing commands

Run from the SemStreams repository root at the pinned baseline. Sister repositories remain read-only.

```text
# Pin and worktree state.
git rev-parse HEAD
git status --short

# Authoritative sister-roster zero for SemConnect across affected surface classes.
rg -n \
  -e 'ConsumeStreamWithConfig|ConsumeInternalStreamWithConfig|ConsumeDurable' \
  -e 'OutstandingWork|StopConsumer|StopAllConsumers|StopAndDeleteConsumer|\.Drain\(' \
  -e 'natsclient\.Subscription|ObserveDirectPortConsumerPolicy' \
  -e 'DeleteConsumerOnStop|delete_consumer_on_stop|DeleteConsumer\(' \
  --glob '*.go' --glob '*.json' --glob '*.ts' ../semconnect
=> 0 lines

# Accepted lifecycle precedence and current durable-consume capability authority.
rg -n 'Accepted|supersedes|managed-consumer|resumable running-Stop|retained repeated-result|lifecycle catalog' \
  docs/adr/094-boot-only-composition-and-observable-rule-activation.md \
  docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md
rg -n 'typed durable-consume|heartbeat_interval|AckWait|InProgress' \
  openspec/specs/gated-dag-dispatch/spec.md

# Production lifecyclejoin closure and remaining package.
rg -n '"github.com/c360studio/semstreams/internal/lifecyclejoin"|lifecyclejoin\.' \
  --glob '*.go' --glob '!**/*_test.go' .
rg -n '^package lifecyclejoin' internal/lifecyclejoin

# Four port APIs and 16 local bridge files.
rg -n \
  -e 'func \(c \*Client\) ConsumeStreamWithConfig' \
  -e 'func \(c \*Client\) ConsumeStreamWithConfigContexts' \
  natsclient/stream.go
rg -l 'ConsumeStreamWithConfig(Contexts)?Handle' \
  --glob '*.go' --glob '!**/*_test.go' examples output processor storage | sort

# Explicit internal creator, its three calls/two owners, claim lifecycle, and empty sister census.
rg -n '\.ConsumeInternalStreamWithConfig\(' \
  agentic/agentrun/agentrun.go internal/maxdelivery/observer.go \
  --glob '*.go' --glob '!**/*_test.go'
rg -n 'reserveInternalConsumer|releaseInternalConsumer|internalClaims' \
  natsclient/client.go natsclient/stream.go --glob '*.go' --glob '!**/*_test.go'
rg -n 'ConsumeInternalStreamWithConfig' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams

# Authoritative port calls versus worktree copies.
rg -n '\.ConsumeStreamWithConfig(Contexts)?\(' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
rg -n '\.ConsumeStreamWithConfig(Contexts)?\(' --glob '*.go' \
  ../semspec-ui-bmad ../semspec-ui-run-visibility

# Client catalogs, claims, both metrics-observation catalogs, and exported name APIs.
rg -n \
  -e 'consumerBinding|internalClaims|trackConsumer|forgetConsumer' \
  -e 'consumerPolicyRecord|trackPolicy|forgetPolicy|policies' \
  -e 'OutstandingWork|StopConsumer|StopAllConsumers|StopAndDeleteConsumer' \
  natsclient --glob '*.go'

# Per-Client metrics owners, registry-shared policy collectors, and absent cross-owner lifecycle coverage.
rg -n 'func WithMetrics|newJetStreamMetrics|RegisterOrGetGaugeVec' \
  natsclient/options.go natsclient/jetstream_metrics.go
rg -n 'TestJetStreamPolicyMetricsShareCanonicalCollectorsAcrossOwners|first\.policy|second\.policy' \
  natsclient/jetstream_metrics_test.go
rg -n 'first\.(trackPolicy|forgetPolicy|updateStats)|second\.(trackPolicy|forgetPolicy|updateStats)' \
  natsclient/jetstream_metrics_test.go
=> 0 lines

# Direct policy-observation caller, OTEL process claim, publication, and terminal cleanup paths.
rg -n '\.ObserveDirectPortConsumerPolicy\b' \
  --glob '*.go' --glob '!**/*_test.go' .
rg -n 'ObserveDirectPortConsumerPolicy' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
rg -n \
  -e 'localOTELConsumerClaims|reserveOTELConsumerClaim|releaseOTELConsumerClaim' \
  -e 'policyCleanups|claims|finishTerminalStop' \
  output/otel/component.go

# Local Drain surfaces and authoritative sister type holders.
rg -l 'Drain\(context\.Context\) error|\.Drain\(ctx\)' \
  --glob '*.go' --glob '!**/*_test.go' . | sort
rg -l '\*natsclient\.Subscription|natsclient\.Subscription' \
  --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams | sort
rg -n '\.Drain\(' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams

# Zero Client.OutstandingWork calls and the two independent local server-state observers.
rg -n '\.OutstandingWork\(' --glob '*.go' --glob '!**/*_test.go' .
rg -n '\.OutstandingWork\(' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
rg -n 'consumerInfos' --glob '*.go' --glob '!**/*_test.go' \
  processor/agentic-loop/component.go processor/agentic-loop/inflight.go
rg -n 'consumerForSubject|outstandingForSubject|NumPending|NumAckPending' \
  processor/agentic-loop/inflight.go processor/agentic-loop/component.go \
  processor/graph-ingest/readiness.go openspec/specs/agentic-loop/spec.md

# Durable production calls, SemMachina interface seams, boot shutdown, and log dependency.
rg -n '\.ConsumeDurable\(' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
rg -n 'ConsumeDurable|StopAllConsumers' --glob '*.go' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
rg -n 'ConsumeDurable logs|ConsumeDurable handler error' \
  ../semmachina/internal/turn/intake.go natsclient/consume_durable.go

# Heartbeat/BackOff and overflow evidence.
rg -n 'AckWait|BackOff|heartbeat\*2|validateHeartbeatBelowAckWait' \
  natsclient/stream.go natsclient/consume_durable.go natsclient/consume_durable_test.go

# Local and generated configuration consumers.
rg -n 'DeleteConsumerOnStop|delete_consumer_on_stop' \
  --glob '*.go' --glob '*.json' .
rg -n 'DeleteConsumerOnStop|delete_consumer_on_stop' \
  --glob '*.go' --glob '*.json' --glob '*.ts' --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams
rg -n 'DeleteConsumerOnStop|DeleteConsumer\(' --glob '*.go' \
  --glob '!**/*_test.go' \
  ../semboids ../semconnect ../semdev ../semdragon ../semmachina ../semops ../semsage \
  ../semsource ../semspec ../semstreams-ui ../semteams

# Missing fixture seam.
rg -n 'StopAndDeleteConsumer|DeleteConsumer\(' --glob '*_test.go' .
rg -n 'fixture.*DeleteConsumer|DeleteConsumer.*fixture' \
  --glob '*.go' test natsclient processor output storage service
```

Expected closing cardinalities are zero production lifecyclejoin lines, 16 local bridge files, 9 authoritative port
calls, 18 worktree port copies, 26 local Drain files, 12 authoritative sister type-holder files, 10 authoritative
ConsumeDurable calls, 7 SemMachina interfaces, 3 internal-creator calls owned by 2 framework owners, zero authoritative
sister internal-creator calls, zero production `Client.OutstandingWork` calls in SemStreams and across the 11-root
authoritative sister roster, 2 independent local production server-state observer implementations (graph-ingest and
agentic-loop), one optional `jetstreamMetrics.policies` instance per successful `WithMetrics`
application (independent across Clients), 3 registry-canonical policy `GaugeVec` collectors shared by Clients using
that registry, zero tests exercising same-key track/forget/refresh across those independent metrics owners, 1
production selector reference to `ObserveDirectPortConsumerPolicy` (OTEL; zero syntactic dot-calls), zero
authoritative sister calls of that observer, 1 process-wide OTEL stream/durable claim map, 5 local Go fields, 5 local
schema properties, and 2 authoritative SemDragon production configuration fields (1 inert and 1 actively read for
direct deletion). SemConnect contributes zero affected sites to every listed API, holder, observer, and configuration
cardinality. The two SemSpec worktrees contribute only the separately reported 18 raw port-call copies and are not
authoritative roots.

## Inventory conclusion

The N1 surface is not one deletion. It is a set of colliding current contracts spanning method names, return values,
Client child authority, same-signature Subscription semantics, settlement and logging, retry timing, configuration,
generated consumers, and missing fixture ownership.

This artifact closes the current census only. It deliberately makes no target or design choice.
