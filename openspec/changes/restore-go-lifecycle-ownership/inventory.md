# Restore Go lifecycle ownership inventory

## Evidence boundary

- Repository: SemStreams.
- Exact baseline: `444b7912da67a8b7ca91fde4c6769e14bed52431`.
- The inventory is read-only design evidence. It does not claim that any target contract is implemented.
- Sister repositories were inspected only to identify migration surfaces. They remain read-only.
- File and line references below are exact at the baseline. Later edits may move them.

### Beta.161 target addendum

The baseline sections below remain forensic evidence, not current target design. At exact beta.161 candidate
`9d0ff67f377ea3dd82dca2f3bf614871c0100766`:

- the atomic context-bearing Stop prerequisite is complete;
- ADR-094 supersedes the replacement authority and phase-collision target;
- post-boot replacement/removal/Transitioning protocols are deleted rather than repaired;
- runtime composition is one sealed boot generation plus restart-safe terminal Stop; and
- the remaining direct-field census is five contexts across four structs:
  `processor/graph-ingest.Component` (`ingestPoolCtx`, `ingestSubmitCtx`), `processor/rule.Processor`
  (`watcherCtx`), `processor/rule.KVConfigManager` (`ctx`), and `processor/rule.CronScheduler` (`parentCtx`).

The beta.161 root census also found 39 unauthorized production `Background`, `TODO`, `WithoutCancel`, nil-fallback,
or indirect roots after the approved partial-start rollback and HTTP `BaseContext` exceptions. Phase 3 must refresh the
exact callsite list before implementation because this file's original nine/eight count is historical.

## SemStreams lifecycle contract surfaces

- Component lifecycle: `component/lifecycle.go:44-54`. `LifecycleComponent.Stop(time.Duration)` cannot receive caller
  authority.
- Service lifecycle: `service/base.go:485-500`. `Service.Stop(time.Duration)` cannot receive caller authority.
- Coordinated stop: `service/service_manager.go:500-589`. `Manager.StopAll(timeout)` forwards only a duration.
- Base-service root: `service/base.go:307-329`. `BaseService.Stop` invents `context.Background()`.
- Component-manager root: `service/component_manager.go:795-849`. `ComponentManager.Stop` invents a root.
- Exported cancel: `component/lifecycle.go:56-82`. `ManagedComponent.Cancel` exposes cancellation authority.
- Replacement entry: `component/registry.go:370-468`. A callback performs lifecycle work inside Registry.
- Reservation: `component/registry.go:420-443`. It is exclusive but invisible to ordinary readers.
- Commit: `component/registry.go:445-467`. It can fail after caller-controlled retirement work.
- Manager replacement: `service/component_manager.go:1788-1876`. Registry exposes the incumbent during Stop.
- Old visibility tests: `component/registry_generation_test.go:410-449`. Tests encode old-visible behavior while a
  replacement reservation is in flight.

### Closed raw-handle and construction-capability census

- Registry generation stores `Discoverable`: `component/registry.go:104-128`.
- Registry raw-return/construction APIs: `CreateComponent` at `component/registry.go:265-276`, `ReplaceComponent` at
  `component/registry.go:370-403`, `ListComponents` at `component/registry.go:555-564`, deprecated `GetComponent` at
  `component/registry.go:590-600`, `Component` at `component/registry.go:630-635`, and `GetFactory` at
  `component/registry.go:666-671`.
- ComponentManager raw-return APIs: `service/component_manager.go:977-990,1135-1148`.
- Exported runtime record: `component/lifecycle.go:56-82`.
- Flow graph handle: `component/flowgraph/flowgraph.go:21-24,83-90`.
- Sibling lookup bypass: `component/dependencies.go:23-26,80` exposes `component.Lookup` through
  `Dependencies.ComponentRegistry`.

The target removes all of these raw handle reads. Registry becomes registration-only or value-only at every exported
method; ComponentManager owns scoped callback borrows and exposes value observation DTOs.

The component and service contracts have distinct test and migration sections, but their signatures form one atomic
prerequisite. `ComponentManager` both implements `Service` and calls `LifecycleComponent.Stop`; no intermediate public
signature set can preserve caller context without an invented root or a lossy context-to-duration adapter.

## Stored production contexts after graph-worker cleanup

The baseline still retains nine `context.Context` fields across eight production structs:

- `processor/graph-ingest/component.go:601,603`
- `service/service_manager.go:58`
- `gateway/graph-gateway/component.go:286`
- `output/otel/component.go:59`
- `processor/rule/kv_config_integration.go:47`
- `processor/rule/processor.go:162`
- `output/websocket/websocket.go:157`
- `processor/rule/cron_scheduler.go:61`

The baseline has no production `context.TODO` call. This is not a zero-debt claim: production roots and severances
remain outside this narrow list and require their own type-aware closeout inventory.

## Historical replacement authority and phase collision table

**Superseded by ADR-094.** The bullets below describe the abandoned repair design. They are retained only to explain
why boot-only composition removes material lifecycle complexity; none is an implementation target.

- Candidate preparation is compatible: both designs prepare off-Registry without runtime authority.
- Reservation extends current behavior with explicit, phase-typed replacement authority.
- Incumbent access is breaking: manager-scoped borrow returns typed Transitioning instead of a raw handle.
- Stop failure is breaking: the incumbent stays current but becomes `Failed` and unavailable.
- Commit is breaking: all fallible checks move before retirement, making post-retirement Commit infallible.
- Candidate Start keeps its ordering after Commit.
- Candidate Start failure becomes explicit: the candidate stays current in `Failed`; no predecessor resurrection.
- Declaration observation remains complete old/new identity; manager borrow state owns availability.

There are two points of no return. Generation cancellation irreversibly ends incumbent availability. Successful Stop
then yields declaration-commit authority. A post-cancel Start-drain expiry or Stop failure leaves the incumbent
current, Failed, and unavailable. Successful Stop permits only infallible candidate declaration commit.

## Existing specification collisions

The first two collisions below are historical and move to `require-restart-for-config-activation`; this change no
longer carries component-discovery or component-runtime-config deltas.

- `component-runtime-config`, `openspec/specs/component-runtime-config/spec.md:494-514`: modify replacement for the
  scoped-borrow transition and separate availability/commit points of no return.
- `component-discovery`, `openspec/specs/component-discovery/spec.md:163-200`: keep declaration identity and add the
  declaration/runtime-handle split.
- `service-shutdown`, `openspec/specs/service-shutdown/spec.md:47-66`: preserve Stop idempotency with caller context.
- `service-shutdown`, `openspec/specs/service-shutdown/spec.md:16-45`: preserve reverse order and error aggregation.
- `framework-composition`, `openspec/specs/framework-composition/spec.md:152-205`: preserve Start barrier semantics.

`semantic-tier-split` is the only other active OpenSpec change at this baseline. It does not overlap these lifecycle
contracts. No materialized spec or active change authorizes a stored context or an exported cancellation function.

## Adopter seam inventory

The adopter is a developer outside SemStreams who implements a component or service and has not read the manager.

- What must they know? `Start(ctx)` owns runtime lifetime. `Stop(ctx)` bounds quiesce, cancellation, join, and cleanup.
- What happens if they do nothing? Implementations and direct calls fail compilation.
- Where do they find out? Migration guide, release notes, compiler errors, and interface documentation.
- What should they have to know? No manager internals, replacement phases, timeout defaults, or cancel handles.

The framework owns per-instance cancellation. An adopter receives the lifetime context through `Start`, observes it
inside running work, and joins that work during `Stop`. The adopter does not store the context, invent a replacement
root, or reach into a managed record to cancel another owner.

### Runtime-consumer adopter seam

The second adopter is a runtime consumer that currently reads a component handle from Registry, ComponentManager,
flow graph, or `Dependencies.ComponentRegistry`.

- What must they know? Runtime access is callback-scoped; missing, stopping, and failed are typed errors.
- What happens if they do nothing? Retired raw-return APIs fail compilation at the exact consumer call site.
- Where do they find out? The lifecycle migration guide, release notes, compiler errors, and `WithComponent` GoDoc.
- What should they have to know? No gate, borrow counter, generation cancel, or drain ordering.

The callback-only handle cannot be retained. If terminal shutdown begins, an admitted callback returns before its
component is stopped; no post-boot remove or replace request exists.

## Sister-repository migration census

These are compile-time migration notices, not authorization to edit another repository. Test-only calls are omitted.

- semboids: implementations at `internal/api/service.go:126` and `internal/sim/component.go:546`; StopAll at
  `cmd/semboids/main.go:465`.
- semconnect: implementation at `gateway/cs-api/component.go:467`; no StopAll caller found.
- semdev: implementations at `internal/conversationchannel/component.go:516`, `internal/intake/component.go:650`,
  `internal/station/station.go:541`, and `internal/boot/runtime.go:836`; StopAll at `internal/boot/runtime.go:840`.
- semdragon: `service/api/service.go:277`, `questdag/component.go:317`, and processor components
  `agentprogression:300`, `agentstore:270`, `autonomy:483`, `boidengine:389`, `bossbattle:339`, `dmapproval:222`,
  `dmpartyformation:223`, `dmsession:238`, `dmworldstate:202`, `executor:297`, `guildformation:296`, `partycoord:277`,
  `questboard:296`, `questbridge:496`, `questdagexec:401`, `questtools:292`, and `redteam:236`; StopAll at
  `cmd/semdragons/main.go:560`.
- semmem: `input/filewatcher/filewatcher.go:159`, `output/graphql/server.go:175`, `output/mcp/server.go:154`, and
  processors `decision:146`, `docs:108`, `spec:112`, and `task:155`; StopAll at `cmd/semmem/main.go:355`.
- semops: implementations in `internal/components`: `dji/components.go:114,268,399`,
  `cot/components.go:176,398,630,843`, `sapient/components.go:196,499,721`,
  `cap/components.go:195,500,703`, `klv/components.go:126,353,558,760,1008`,
  `adsb/components.go:195,511,733`, `weather/components.go:127,291,484,713`,
  `mavlink/components.go:169,425,635`, and `fusion/components.go:157` plus `fusion/candidates.go:156`.
  No StopAll caller was found.
- semsage: implementations at `processor/ui-api/component.go:180` and `processor/ui-api/service.go:93`; StopAll at
  `cmd/semsage/main.go:154`.
- semsource: `storage/filestore/component.go:94`; processors `ast-source:944`, `audio-source:440`,
  `cfgfile-source:438`, `code-context:532`, `doc-source:622`, `git-source:476`, `image-source:438`, `mcp-gateway:148`,
  `source-manifest:550`, `supersession:294`, `url-source:442`, and `video-source:446`; StopAll at
  `cmd/semsource/run.go:202`.
- semspec: `output/workflow-documents/component.go:307`; processors `execution-bridge:239`, `github-submitter:493`,
  `github-watcher:466`, `lesson-curator:253`, `lesson-decomposer:293`, `plan-api:89`, `plan-decision-handler:446`,
  `project-manager:151`, `qa-reviewer:223`, `question-manager:521`, `recovery-consumer:250`,
  `researcher-manager:258`, `spec-sessions:228`, `structural-validator:360`, and `workflow-validator:274`; StopAll at
  `cmd/semspec/main.go:202`.
- semteams: no implementation found; StopAll at `cmd/semteams/main.go:1079`.
- semlink: no implementation or StopAll caller found, but direct component Stop occurs at
  `internal/semstreams/runtime.go:147`.

No production implementation or coordinated-stop call was found in semembed, seminstruct, semlink, semmachina, or
semsummarize. The semlink direct Stop call above still requires migration. Direct Registry runtime readers are a
**BREAKING** replacement-observation migration surface:

- semboids: `internal/api/service.go:134,145,225`
- semmachina: `internal/content/managed.go:54`; `internal/boot/components.go:350`
- semdragon: multiple runtime reads; its owner must compile and inventory the exact current checkout during migration

The census is intentionally read-only and time-bounded. Each downstream team owns its code changes and validation.
