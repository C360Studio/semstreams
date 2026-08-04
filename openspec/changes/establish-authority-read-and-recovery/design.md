# Design — authority read and recovery (GS-01)

> **UNAPPROVED — DESIGN-ONLY.** This file records the problem, accepted process gates, reviewed inventory, and
> `INVENTORY PASS`. It contains no target state, options, recommendation, spec delta, or runtime authorization.

## 1. Problem boundary

GS-01 must establish what the current repository actually does before choosing
how, or whether, to change it. The bounded questions are:

1. What does an exact authoritative entity read return, which current owner
   supplies it, and how are found, absent, poison, unavailable, and canceled
   outcomes represented today?
2. What current state, referenced content, ingest guards, and operational seams
   participate in authority recovery, and what can each source prove after loss
   or restart?
3. Which existing status, lifecycle, ownership, catalog, reader, writer, and
   recovery mechanisms already govern graph-ingest instance safety?
4. Where do current behavior, current specs, ADR-090, and operator expectations
   conflict or leave a measured gap?

These are investigation questions, not assertions that new primitives are
required.

## 2. Accepted process gates

The only accepted design content is this sequence:

1. An architect produces a fresh, repository-first, problem-only inventory.
2. An independent reviewer reruns the enumeration and records `INVENTORY PASS` or
   blocking omissions.
3. Only after `INVENTORY PASS`, the architect frames options, costs, premises,
   adopter seams, and a recommendation.
4. An independent reviewer attempts to refute that design and records
   `DESIGN REVIEW PASS` or blocking findings.
5. The owner explicitly accepts, rejects, or redirects the reviewed design.
6. Only explicit owner acceptance can authorize adding capability spec deltas
   and TDD implementation tasks to this same GS-01 change, followed by
   implementation, validation, promotion, and archive. Do not open a second
   change for that work.

A prompt, briefing, issue, prior proposal, or proposed symbol is a hypothesis to
falsify. None substitutes for independent repository enumeration.

## 3. Failed-premise record

The prior attempt does not carry forward as design input:

- `GRAPH_INGEST_ACTIVE` was proposed as a missing graph-ingest coordination
  primitive. Review found overlapping `GRAPH_STATUS` and graph-ingest territory,
  so the claimed empty semantic class was false.
- A NATS CLI requirement was proposed without first inventorying current operator
  and recovery seams. That requirement is withdrawn.
- Prior GS-01 acceptance is revoked/not granted. No mechanism from the failed
  attempt is accepted by implication.

The fresh inventory must enumerate the existing territory without using these
corrections as a directed list to verify.

## 4. Reviewed inventory

**Status: COMPLETE AND INDEPENDENTLY REVIEWED.** Do not add target-state prose to this section.

The inventory-only deliverable must record:

- evidence baseline and exact repository revision;
- the claimed gaps under every plausible spelling;
- every current spelling and owner of each fact or coordination job;
- adjacent specs, ADRs, active changes, issues, and operator contracts;
- present consumers and the adopter do-nothing path;
- exact searches that close genuinely empty categories; and
- a same-semantic-class collision table for any candidate durable,
  communication, or runtime-coordination primitive.

The collision table must enumerate, with `file:line` evidence, semantic class,
owners, catalogs, status, lifecycle, ownership, readers, writers, and recovery.
Different names do not excuse omission. The table reports collisions and
unknowns; it does not select a solution.

### Reviewed inventory

**Status: COMPLETE AND INDEPENDENTLY REVIEWED.**

#### GS-01 corrected inventory-only handoff — fifth pass

Inventory is against committed baseline `fe725f8225941e778af459f927be5afe0cb862aa` on `codex/gs01-authority-recovery`.

The inspected worktree was not clean:

```text
 M .agents/contracts/semstreams-architect.md
 M .agents/contracts/semstreams-reviewer.md
 M docs/proposals/graph-state-read-write-program.md
?? openspec/changes/establish-authority-read-and-recovery/
```

Those uncommitted artifacts were read as the GS-01 baton, not as committed current-state evidence. No files were edited
and no mutating Git commands were run.

##### Claimed gap and spelling inventory

The current authority is `ENTITY_STATES`, an `EntityState` stored under its entity ID with KV history 1. Graph-ingest is
the catalog-declared sole writer.

Admitted raw-`EntityState` exact-read surfaces return values without NATS KV revisions. An exported lifecycle projection
surface already returns its current authority revision.

- **Kind:** Authority bucket
  **Existing spelling:** `ENTITY_STATES`
  **Result shape:** KV `EntityState`
  **Evidence:** `graph/constants.go:6`; `graph/kvcatalog.go:67-74`

- **Kind:** Exact authority RPC
  **Existing spelling:** `graph.ingest.query.entity`
  **Result shape:** raw `EntityState`, value-only
  **Evidence:** `processor/graph-ingest/query.go:24-105`

- **Kind:** Batch authority RPC
  **Existing spelling:** `graph.ingest.query.batch`
  **Result shape:** values plus missing-item records
  **Evidence:** `processor/graph-ingest/query.go:24-55,107-160`

- **Kind:** Prefix/suffix reads
  **Existing spelling:** `graph.ingest.query.prefix`, `.suffix`
  **Result shape:** collections without per-value KV revision
  **Evidence:** `processor/graph-ingest/query.go:24-55`

- **Kind:** Gateway exact query
  **Existing spelling:** `graph.query.entity`
  **Result shape:** proxied raw entity
  **Evidence:** `gateway/graph-gateway/component.go:946-960`

- **Kind:** GraphQL field
  **Existing spelling:** `entity(id: String!)`
  **Result shape:** entity under `data.entity`
  **Evidence:** `gateway/graph-gateway/component.go:1583`

- **Kind:** Embedded raw reader
  **Existing spelling:** `graph/query.Client.GetEntity`
  **Result shape:** `*graph.EntityState`, value-only
  **Evidence:** `graph/query/interface.go:12-67`; `graph/query/client.go:281-327`

- **Kind:** Projection authority reader
  **Existing spelling:** `projection.MutationClient.ReadAuthoritative`
  **Result shape:** `*graph.EntityState`, value-only
  **Evidence:** `pkg/projection/mutation_client.go:954-984`; `pkg/projection/mutation_types.go:158-161`

- **Kind:** Lifecycle projection read
  **Existing spelling:** `Manager.GetWithRevision`
  **Result shape:** `(Participant, uint64, error)`
  **Evidence:** `pkg/lifecycle/manager.go:483-552`

- **Kind:** Lifecycle raw read
  **Existing spelling:** `Manager.GetRaw`
  **Result shape:** `*graph.EntityState`, value-only
  **Evidence:** `pkg/lifecycle/manager.go:483-552`

- **Kind:** Lifecycle history
  **Existing spelling:** `Manager.History`
  **Result shape:** projected revision history from KV `History`
  **Evidence:** `pkg/lifecycle/manager_query.go:515-605`

- **Kind:** Rule exact snapshot
  **Existing spelling:** `entitySnapshot`
  **Result shape:** entity plus revision
  **Evidence:** `processor/rule/entity_watcher.go:990-1027`

- **Kind:** Agent-run exact reader
  **Existing spelling:** direct predicate lookup
  **Result shape:** predicate value
  **Evidence:** `agentic/agentrun/nats_reader.go:107-138`

- **Kind:** Component-local health
  **Existing spelling:** `graph-ingest.Health()`
  **Result shape:** `component.HealthStatus`
  **Evidence:** `processor/graph-ingest/component.go:914-953`

- **Kind:** Distributed readiness
  **Existing spelling:** `graph-ingest` in `GRAPH_STATUS`
  **Result shape:** operational readiness record
  **Evidence:** `graph/readiness/watcher.go:39-62`; `processor/graph-ingest/readiness.go:313-346`

- **Kind:** Component lifecycle status
  **Existing spelling:** `graph-ingest` in `COMPONENT_STATUS`
  **Result shape:** lifecycle diagnostic
  **Evidence:** `processor/graph-ingest/component.go:1474-1477`; `component/lifecycle_reporter.go:94-117`

- **Kind:** Graph-ingest metrics
  **Existing spelling:** package-level collectors/gauges
  **Result shape:** process-level Prometheus series
  **Evidence:** `processor/graph-ingest/component.go:44-90`; `processor/graph-ingest/readiness.go:349-377`

- **Kind:** Replay guard
  **Existing spelling:** `GRAPH_INGEST_APPLIED_SEQ`
  **Result shape:** entity/stream sequence
  **Evidence:** `graph/constants.go:48-55`; `processor/graph-ingest/keyed_ingest.go:245-299`

- **Kind:** Semantic owner enforcement
  **Existing spelling:** `enforce_owner_lease`
  **Result shape:** graph-ingest config flag
  **Evidence:** `processor/graph-ingest/component.go:452-459,2109-2209`

- **Kind:** Ownership catalogs
  **Existing spelling:** `OWNER_CLAIMS`, `OWNER_PRESENCE`
  **Result shape:** ownership epochs/presence
  **Evidence:** `graph/constants.go:65-77`; `pkg/ownership/doc.go`

- **Kind:** Content reference
  **Existing spelling:** `EntityState.storage_ref`
  **Result shape:** logical storage instance and key
  **Evidence:** `graph/types.go:24-47`; `message/storable.go:15-35`

- **Kind:** Live storage resolver
  **Existing spelling:** `storage/storeregistry.Registry`
  **Result shape:** process-local instance-to-store map
  **Evidence:** `storage/storeregistry/storeregistry.go:1-26,38-101`

- **Kind:** Referenced substrate
  **Existing spelling:** ObjectStore `OBJ_<bucket>`
  **Result shape:** stored bytes/objects
  **Evidence:** `storage/objectstore/store.go:82-150`

No admitted raw-`EntityState` exact-read result combines the raw authority value and KV revision.
`Manager.GetWithRevision` is an exported revision-bearing lifecycle projection result, not a raw-`EntityState` result.

##### Surface inventory

###### 1. Authority value

`EntityState` contains ID, triples, optional storage reference, source message type, logical entity version, and update
timestamp. It has no NATS KV revision field (`graph/types.go:24-47`).

Its logical `Version` is distinct from the KV revision returned by NATS.

###### 2. Graph-ingest exact-read RPC

`handleQueryEntityNATS`:

- waits for graph-ingest’s boot-validation gate;
- applies a five-second timeout;
- JSON-decodes the request;
- rejects an empty ID;
- executes `entityBucket.Get`;
- classifies absence as invalid `entity_not_found`;
- classifies other KV failures as transient `internal`;
- canonical-decodes the stored bytes;
- records poison against the requested key and entry revision;
- returns `entry.Value` unchanged.

Evidence: `processor/graph-ingest/query.go:60-105`.

The handler validates JSON and non-empty ID only. It does not canonically validate the requested entity ID before
reading. Canonical validation occurs while decoding the retrieved authority value.

The successful path receives the KV revision but omits it from the raw response.

Batch reads perform concurrent per-ID fetches. Missing items are represented separately; returned entities have no KV
revision (`processor/graph-ingest/query.go:107-160`).

The read gate is graph-ingest boot state. Snapshot transport loss and incomplete bootstrap return transient
`index_not_ready` (`processor/graph-ingest/query.go:483-499`). Canonical decode failures are inventoried with key and
revision (`processor/graph-ingest/query.go:501-518`).

Current specifications identify graph-ingest’s query lanes as the authoritative read surface
(`openspec/specs/graph-state-contract/spec.md:35-48`; ADR-079).

###### 3. Exported graph-query raw reader

`graph/query.Client` opens `ENTITY_STATES`, `SPATIAL_INDEX`, and `INCOMING_INDEX`, starts a retained `WatchAll`,
maintains a cache, and performs direct authority `Get` operations (`graph/query/client.go:147-254`).

`GetEntity`:

- is cache-first;
- returns `*EntityState`;
- does not expose KV revision;
- wraps raw KV absence as a generic “failed to get entity” error;
- is gated by a process-lifetime whole-client poison latch;
- treats watcher loss as transient but leaves the client unusable until replaced.

Evidence: `graph/query/client.go:256-327`.

Construction requires unrelated spatial and incoming buckets (`graph/query/client.go:161-177`).

###### 4. Projection mutation authority reader

`projection.MutationClient.ReadAuthoritative` canonically validates the requested entity ID, dispatches to the authority
RPC, and returns:

```go
(*graph.EntityState, error)
```

It is value-only and does not preserve the graph-ingest KV revision (`pkg/projection/mutation_client.go:954-984`;
`pkg/projection/mutation_types.go:158-161`).

Concrete outcomes from `pkg/projection/mutation_client.go:444-513`:

- **Outcome:** Invalid canonical requested ID
  **Mutation result:** `MutationInvalid`
  **Commit state:** `CommitNotCommitted`

- **Outcome:** `entity_not_found`
  **Mutation result:** `MutationNotFound`
  **Commit state:** `CommitNotCommitted`

- **Outcome:** Poison/reset-required
  **Mutation result:** `MutationInternal`, preserving underlying code/class
  **Commit state:** `CommitNotCommitted`

- **Outcome:** Cancellation/transient transport failure
  **Mutation result:** `MutationUnavailable`
  **Commit state:** `CommitNotCommitted`

This seam differs from the raw handler in request-ID validation and mutation-specific error mapping, but not revision
shape.

###### 5. Exported lifecycle authority readers

The lifecycle `Manager` exposes two exported reads (`pkg/lifecycle/manager.go:483-552`):

- `Manager.GetWithRevision` returns `(Participant, uint64, error)`. The revision is the current `ENTITY_STATES` KV
  revision associated with the projected lifecycle participant.
- `Manager.GetRaw` returns `*graph.EntityState` without KV revision.

Authority absence returns `ErrEntityNotFound`. A present authority entity that does not project into a lifecycle
participant can return `ErrEntityNotLifecycleManaged`.

Canonical poison behavior is Manager-wide (`pkg/lifecycle/manager.go:433-480`):

- poison latches the entire Manager;
- the Manager publishes its graph-state guard as not ready;
- later lifecycle accesses are blocked by the latched guard, not only reads of the poisoned entity.

Thus the revision-bearing result is a lifecycle projection result, while the exported raw result is value-only under the
same Manager guard.

###### 6. Lifecycle history surface

The lifecycle specification states that lifecycle state uses `ENTITY_STATES` KV revision replay as its audit trail and
assigns authority/history behavior to graph-ingest and the graph-state contract
(`openspec/specs/lifecycle/spec.md:5-7,41-47`).

`Manager.History` calls the authority bucket’s `History` operation and projects returned historical entries
(`pkg/lifecycle/manager_query.go:515-605`).

Current catalog state fixes `ENTITY_STATES` at history 1. Therefore the bucket cannot retain a multi-transition
lifecycle replay for one entity. At most the retained current revision is available through this substrate.

The troubleshooting guide separately instructs operators to inspect `ENTITY_STATES` revision history for a generic
triple-update symptom (`docs/operations/02-troubleshooting.md:340-349`). This is broader operator expectation, not
direct lifecycle-audit evidence.

ADR-090 separately states that current-state authority history 1 is not lifecycle audit history. These are conflicting
current claims on the lifecycle-history surface.

###### 7. Other component-specific exact readers

- `processor/rule/entity_watcher.go:990-1027` returns an `entitySnapshot` with entity and revision. Authority absence
  becomes synthesized `DELETED` with revision `0`; that is not an observed tombstone revision.
- `agentic/agentrun/nats_reader.go:107-138` reads authority for a predicate. Missing entity and present entity lacking
  the predicate both collapse into predicate absence.

These result semantics differ from graph-ingest, lifecycle, projection, and graph-query reads.

###### 8. Remote adopter surface

The graph gateway registers GraphQL and MCP handlers. The production prefix is typically `/graph-gateway/`; paths are
configuration-derived (`gateway/graph-gateway/component.go:695-746`).

GraphQL `entity` maps to `graph.query.entity`, extracts `id`, and returns downstream JSON under `data.entity`
(`gateway/graph-gateway/component.go:946-1013`, `1260-1276`).

Classified handler failures become HTTP 200 GraphQL errors; timeout is 504; transport failure is 500
(`gateway/graph-gateway/component.go:1855-1883`).

The response-shape fixture records:

```text
graph.query.entity -> graph.ingest.query.entity -> graph-ingest, raw EntityState
```

Evidence: `gateway/graph-gateway/response_shape_test.go:190`.

The GraphQL result is a raw value-only authority shape.

###### 9. Graph-ingest component-local health

`Health()` is distinct from `GRAPH_STATUS`, `COMPONENT_STATUS`, and lifecycle-manager readiness
(`processor/graph-ingest/component.go:914-953`).

`Healthy` requires:

- state is running;
- cumulative `errorCount` is zero;
- poison inventory is empty;
- snapshot-watch loss is not latched;
- bootstrap sweep is complete.

Poison or snapshot-watch loss changes component `Status` to degraded. A historical processing error can leave
`Healthy=false` while `Status` remains `running`.

Therefore `Healthy`, component `Status`, graph readiness, and lifecycle status are not interchangeable views of one
state machine.

###### 10. Catalog, status, lifecycle, and metrics surfaces

`ENTITY_STATES` is authoritative, owner `graph-ingest`, history 1 (`graph/kvcatalog.go:67-74`).

Catalog ownership is descriptive and call-site/review enforced; it has no runtime caller identity
(`graph/kvcatalog.go:1-24`).

`GRAPH_STATUS` is operational, history 3. Its catalog owner string names graph-index and graph-embedding as readiness
producers (`graph/kvcatalog.go:76-81`). The actual readiness-key inventory also contains `graph-ingest` and `rule`, and
those owners publish/read status through the same bucket (`graph/readiness/watcher.go:51-62`; graph-ingest publisher in
`processor/graph-ingest/readiness.go:313-346`).

The catalog’s owner description is therefore narrower than the live producer/key set.

`IndexStatusResponse` carries readiness, bootstrap state, lag, failure, staleness, and optional revision fields
(`graph/index_status.go:8-160`).

Graph-ingest computes backlog from bound consumers. A configuration with no bound consumer is caught up once its
authority boot sweep completes (`processor/graph-ingest/readiness.go:108-244`).

`COMPONENT_STATUS` is diagnostic and write-open. Its catalog census records 24 production writers and zero production
readers; retention is unmanaged (`graph/kvcatalog.go:134-149`; `component/lifecycle_reporter_catalog.go:11-47`).
Graph-ingest writes under `graph-ingest`.

The in-process component manager keys status by component instance name within one process
(`service/component_manager.go:2182-2250`).

Graph-ingest readiness gauges and other collectors are package-level `sync.Once` singletons
(`processor/graph-ingest/readiness.go:349-377`; `processor/graph-ingest/component.go:44-90`):

- duplicate/recreated in-process graph-ingest instances share the first registered collectors;
- counters aggregate across instances;
- gauges reflect the latest writer;
- metric series do not identify the authority instance.

###### 11. Runtime component admission

Component registry and manager uniqueness is instance-name-only. They reject duplicate instance names, not duplicate
component factory/type registrations (`service/component_manager.go:940-955,1080-1093`;
`component/registry.go:253-280,574-600`).

Resource-conflict admission only checks ports declared `IsExclusive`.

Graph-ingest’s relevant resource ports are nonexclusive:

- KV write port: `component/port_kv.go:28-41`;
- NATS request port: `component/port_nats.go:34-48,71-86`;
- JetStream port: `component/port_jetstream.go:55-68`.

Consequently, two graph-ingest components with different instance names are admitted by current registry/manager checks
even though they can share authority buckets, mutation/query subjects, durable names, package metrics, and status
semantics.

Same-name duplication is rejected. Different-name same-type duplication is not a singleton violation in current
admission logic.

###### 12. Ownership and semantic owner-lease enforcement

`OWNER_CLAIMS` and `OWNER_PRESENCE` implement predicate-group producer ownership. They do not elect or fence the
graph-ingest process.

Graph-ingest’s `EnforceOwnerLease` setting defaults to false (`processor/graph-ingest/component.go:452-459`). Six
shipped configurations explicitly enable it; the remaining shipped graph-ingest configurations use the default
observe-only posture.

Owner-lease checking in `processor/graph-ingest/component.go:2109-2209` behaves as follows:

- with enforcement disabled, a confirmed mismatch is warned and metered, but the write proceeds;
- with enforcement enabled, only a confirmed fenced owner-token mismatch rejects;
- empty token fails open;
- missing claim reader fails open;
- unclaimed predicate fails open;
- legacy claim without the required fence fails open;
- claim-reader failure fails open.

This is semantic producer-write fencing for confirmed claimed predicates. It is not graph-ingest process election,
replica exclusion, or sole-responder enforcement.

###### 13. Mutation subjects and ingestion lanes

Graph-ingest registers plain core-NATS request/reply subscriptions for eight mutation subjects:

```text
graph.mutation.triple.add
graph.mutation.triple.add_batch
graph.mutation.triple.remove
graph.mutation.entity.create
graph.mutation.entity.create_with_triples
graph.mutation.entity.update
graph.mutation.entity.update_with_triples
graph.mutation.entity.delete
```

Evidence: `processor/graph-ingest/mutations.go`.

These RPC subjects are distinct from configured Graphable JetStream ingestion.

RPC handlers use plain subscriptions, not queue subscriptions (`natsclient/request.go:337-405`). Multiple graph-ingest
processes all receive the same mutation request and can execute it; replies race.

Configured Graphable ingestion uses JetStream durable consumers. Consumer names derive from input subjects and lack
runtime-instance identity (`processor/graph-ingest/component.go:1527-1571`).

###### 14. Runtime-instance topology

The keyed ingest pool is process-local. The applied guard is keyed by entity and stream, but its one-lane-per-entity
premise holds only inside one process (`processor/graph-ingest/keyed_ingest.go:245-299`).

ADR-072 records that multiple replicas sharing a durable can split one entity’s messages and break ordering
(`docs/adr/072-keyed-concurrent-entity-ingest.md:228-240`).

Inside one process, `natsclient/stream.go:321-337` indexes consume contexts by `StreamName:ConsumerName`. A duplicate
stops and deletes the first context before storing the replacement. The first component can remain lifecycle-reported as
running after losing its consume context.

Duplicate in-process instances also share package-level metrics. Counters aggregate and gauges overwrite one another.

Nineteen shipped JSON configurations contain graph-ingest:

```text
configs/flows/deep-research.json
configs/statistical.json
configs/flows/lesson-example.json
configs/flows/deep-research-test.json
configs/e2e-structural.json
configs/semantic-8b.json
configs/flows/ops-agent-test.json
configs/research-graph-e2e.json
configs/lifecycle-flow.json
configs/structural.json
configs/semantic-frontier.json
configs/hello-world.json
configs/protocol-flow.json
configs/agentic.json
configs/examples/research-graph-pipeline.json
configs/graph-backend.json
configs/semantic.json
configs/flows/ops-agent.json
configs/flows/crud-tools-test.json
```

Each observed declaration uses component name `graph-ingest`. Six explicitly enable owner-lease enforcement; the
remainder omit it and therefore use observe-only default behavior.

###### 15. Storage-reference runtime resolution

`storage/storeregistry.Registry` owns live `StorageInstance -> StreamableStore` resolution
(`storage/storeregistry/storeregistry.go:1-26,38-101`).

It is process-local:

- ObjectStore registers at start;
- deregisters at stop;
- fetchers resolve lazily;
- duplicate live instance registration is rejected within that process;
- entries are runtime pointers, not durable recovery metadata.

A logical instance does not by itself identify its backing bucket offline. The mapping disappears on restart and is
reconstructed from configuration/startup.

###### 16. Recovery surface

Current procedures cover:

- per-entity poison capture/delete/repair/republish, with restart of sticky consumers where necessary
  (`docs/operations/33-graph-poison-response-runbook.md:12-65`);
- destructive beta cutover, removing authority, guards, and derived buckets before reseed from a product-owned source
  (`docs/operations/17-predicate-cutover-clean-wipe.md:35-70`;
  `docs/operations/29-entity-id-contract-clean-cutover.md:81-153`).

The poison runbook warns that recreating an input stream without clearing `GRAPH_INGEST_APPLIED_SEQ` can make reseed
messages appear already applied.

The cutover runbook provides no export, preservation, or rollback and instructs operators not to delete ObjectStore
state.

ADR-090 distinguishes authority snapshot/restore from Graphable replay and includes authority, referenced ObjectStore
content, and applied guard state (`docs/adr/090-authoritative-current-state-and-materialized-views.md:16-46`). No
current coordinated procedure or implementation was found.

###### 17. Referenced-content shapes

`EntityState.StorageRef` contains:

```text
storage_instance
key
content_type
size
```

Graph-ingest accepts a reference from any `message.Storable` (`processor/graph-ingest/component.go:2245-2255`). A
universal object shape cannot be inferred.

At least two current shapes exist:

1. Ordinary ObjectStore processing stores raw serialized message bytes and emits a direct reference
(`storage/objectstore/component.go:441-516`).

2. `StoreContent` emits a reference to a `StoredContent` envelope with zero or more one-hop `BinaryRef` keys
(`storage/objectstore/store.go:433-526`).

For the second shape only:

```text
StorageReference
  -> StoredContent envelope
     -> zero or more BinaryRef objects
```

`BinaryRef` has no nested reference field, so that schema is not recursively open-ended.

Other `message.Storable` implementations can use other stored shapes. Graph-ingest records no universal shape
discriminator beyond the reference and entity provenance.

No graph-ingest boot sweep or admitted raw read resolves the storage registry, identifies the backing bucket, fetches
the referenced object, or validates binary children.

##### Collision tables

###### Exact-reader semantic class

- **Surface:** `graph.ingest.query.entity`
  **Result shape:** raw `EntityState`, no revision
  **Validation:** JSON + non-empty ID
  **Absence/projection semantics:** typed `entity_not_found`
  **Poison/availability behavior:** per-entity decode; boot gate

- **Surface:** `projection.MutationClient.ReadAuthoritative`
  **Result shape:** `*EntityState`, no revision
  **Validation:** canonical ID
  **Absence/projection semantics:** `MutationNotFound`
  **Poison/availability behavior:** invalid/internal/unavailable; not committed

- **Surface:** `graph/query.Client.GetEntity`
  **Result shape:** `*EntityState`, no revision
  **Validation:** client-specific
  **Absence/projection semantics:** generic wrapped KV failure
  **Poison/availability behavior:** whole-client sticky poison

- **Surface:** `Manager.GetRaw`
  **Result shape:** `*EntityState`, no revision
  **Validation:** Manager authority path
  **Absence/projection semantics:** `ErrEntityNotFound`
  **Poison/availability behavior:** poison latches whole Manager

- **Surface:** `Manager.GetWithRevision`
  **Result shape:** `Participant` plus revision
  **Validation:** authority plus lifecycle projection
  **Absence/projection semantics:** `ErrEntityNotFound` / `ErrEntityNotLifecycleManaged`
  **Poison/availability behavior:** Manager-wide poison; guard not-ready

- **Surface:** `Manager.History`
  **Result shape:** projected KV revisions
  **Validation:** lifecycle projection over bucket history
  **Absence/projection semantics:** authority/projection errors
  **Poison/availability behavior:** H1 cannot supply multi-transition replay

- **Surface:** Rule `entitySnapshot`
  **Result shape:** entity plus revision
  **Validation:** watcher/fetch
  **Absence/projection semantics:** synthesized `DELETED`, rev 0
  **Poison/availability behavior:** rule fence behavior

- **Surface:** Agent-run predicate read
  **Result shape:** predicate value
  **Validation:** entity/predicate
  **Absence/projection semantics:** missing entity and predicate collapse
  **Poison/availability behavior:** direct authority read

- **Surface:** GraphQL `entity`
  **Result shape:** raw entity, no revision
  **Validation:** gateway/downstream
  **Absence/projection semantics:** GraphQL classified error
  **Poison/availability behavior:** 200 classified; 504 timeout; 500 transport

- **Surface:** Raw KV diagnostics
  **Result shape:** bytes/tool metadata
  **Validation:** tool-specific
  **Absence/projection semantics:** raw KV semantics
  **Poison/availability behavior:** bypasses graph-ingest gate

###### Status, admission, and runtime ownership

- **Surface:** Catalog authority owner
  **Identity/scope:** `graph-ingest`
  **Meaning:** declared bucket owner
  **Collision:** not process identity

- **Surface:** Component registry
  **Identity/scope:** instance name
  **Meaning:** uniqueness
  **Collision:** same name rejected; different-name duplicate graph-ingest admitted

- **Surface:** Resource conflict check
  **Identity/scope:** `IsExclusive` ports only
  **Meaning:** admission conflict
  **Collision:** graph-ingest KVWrite/NATSRequest/JetStream ports are nonexclusive

- **Surface:** `Health()`
  **Identity/scope:** individual Go object
  **Meaning:** running + zero errors + no poison/loss + boot complete
  **Collision:** unhealthy can coexist with status running

- **Surface:** `GRAPH_STATUS`
  **Identity/scope:** shared producer key
  **Meaning:** distributed readiness
  **Collision:** replicas overwrite; catalog omits live graph-ingest/rule producer names

- **Surface:** `COMPONENT_STATUS`
  **Identity/scope:** component name
  **Meaning:** lifecycle diagnostic
  **Collision:** replicas with same name overwrite; stale rows persist

- **Surface:** Component manager state
  **Identity/scope:** local instance name
  **Meaning:** in-process status
  **Collision:** not cross-process

- **Surface:** Prometheus counters
  **Identity/scope:** package singleton
  **Meaning:** accumulated process activity
  **Collision:** duplicate instances aggregate

- **Surface:** Prometheus gauges
  **Identity/scope:** package singleton
  **Meaning:** latest written process value
  **Collision:** latest writer masks another instance

- **Surface:** Mutation RPCs
  **Identity/scope:** eight fixed subjects
  **Meaning:** authority mutations
  **Collision:** all plain subscribers execute

- **Surface:** Query RPCs
  **Identity/scope:** fixed subjects
  **Meaning:** authority reads
  **Collision:** all subscribers answer

- **Surface:** JetStream ingestion
  **Identity/scope:** subject-derived durable
  **Meaning:** Graphable delivery
  **Collision:** messages distribute across process-local lanes

- **Surface:** Consume registry
  **Identity/scope:** `StreamName:ConsumerName`
  **Meaning:** local consume context
  **Collision:** duplicate replaces earlier context

- **Surface:** Applied guard
  **Identity/scope:** entity/stream
  **Meaning:** sequence memory
  **Collision:** no process fence

- **Surface:** Owner lease
  **Identity/scope:** semantic owner token
  **Meaning:** producer-write fencing
  **Collision:** default observe-only; several fail-open cases; not process election

- **Surface:** Storage registry
  **Identity/scope:** `StorageInstance`
  **Meaning:** live local resolver
  **Collision:** local-only and lost on restart

###### Recovery-state collision

- **State:** `ENTITY_STATES`
  **Semantic class:** current authority, history 1
  **Owner:** graph-ingest
  **Existing recovery evidence:** poison repair and wipe/reseed

- **State:** Lifecycle audit claim
  **Semantic class:** multi-transition replay
  **Owner:** lifecycle spec/Manager
  **Existing recovery evidence:** runtime calls H1 authority history; no multi-transition retention

- **State:** Direct raw referenced object
  **Semantic class:** message content
  **Owner:** configured ObjectStore
  **Existing recovery evidence:** preserved by cutover; no coordinated procedure

- **State:** `StoredContent` envelope
  **Semantic class:** structured content
  **Owner:** ObjectStore
  **Existing recovery evidence:** no coordinated procedure

- **State:** `BinaryRef` objects
  **Semantic class:** one-hop children
  **Owner:** ObjectStore
  **Existing recovery evidence:** no coordinated procedure

- **State:** Storage registry
  **Semantic class:** runtime logical map
  **Owner:** running components
  **Existing recovery evidence:** reconstructed; not durable evidence

- **State:** `GRAPH_INGEST_APPLIED_SEQ`
  **Semantic class:** replay guard
  **Owner:** graph-ingest
  **Existing recovery evidence:** must match source stream

- **State:** Input streams
  **Semantic class:** pending facts/commands
  **Owner:** product/config owner
  **Existing recovery evidence:** recreation changes sequence identity

- **State:** Derived buckets
  **Semantic class:** rebuildable views
  **Owner:** projection owners
  **Existing recovery evidence:** rebuilt from authority

- **State:** Status/health/metrics
  **Semantic class:** operational observations
  **Owner:** components/process
  **Existing recovery evidence:** not authority recovery state

##### Adopter seam inventory

###### Developer using `graph/query.Client`

- Must know: unrelated buckets are required, a retained watcher/cache is created, poison is whole-client, watcher loss
  requires replacement, and no revision is returned.
- If they do nothing: construction can fail for unrelated storage, a dead client can remain in use, and authority
  currency is unavailable.
- Where they find out: comments, errors, implementation, ADR-079, and query/state specs.
- What they should have to know: request, value, and failure semantics.

###### Lifecycle adopter

- Must know: `GetRaw` is value-only; `GetWithRevision` returns projected participant plus authority revision; authority
  absence and projection miss differ; poison latches the entire Manager; `History` reads authority bucket history, which
  currently retains one revision.
- If they do nothing: they can choose the wrong result, conflate projection miss with absence, assume unrelated entities
  remain accessible after poison, or interpret `History` as a multi-transition audit trail.
- Where they find out: Manager implementation, lifecycle spec, and ADR-090; the troubleshooting guide supplies a broader
  operator expectation. The lifecycle spec, `Manager.History`, and H1 provide the direct conflict evidence.
- What they should have to know: raw versus projected read, actual retained history, readiness, and errors.

###### Projection producer

- Must know: requested IDs are canonically validated; result is value-only; failures map to mutation results; all
  failures are not committed.
- If they do nothing: they can assume revision-bearing state or conflate poison with availability.
- Where they find out: mutation client/types.
- What they should have to know: read result and commit state.

###### Semantic mutation producer using owner tokens

- Must know: enforcement defaults off; only six shipped configs enable it; default mismatches warn/meter and still
  write; even when enabled, empty token, missing reader, unclaimed predicate, legacy claim, and reader error fail open.
- If they do nothing: they can treat token presence as a universal write fence when only confirmed fenced mismatches
  reject under enabled enforcement.
- Where they find out: graph-ingest config and owner-lease check implementation.
- What they should have to know: whether their specific claimed write was admitted or rejected. Owner lease does not
  identify the active graph-ingest process.

###### Remote GraphQL developer

- Must know: `entity(id:)` forwards to graph-ingest; classified errors use HTTP 200; timeout/transport differ; logical
  version is not KV revision.
- If they do nothing: HTTP 200 can be mistaken for success and logical version for currency.
- Where they find out: gateway schema/tests and graph-state docs.
- What they should have to know: GraphQL result/error semantics.

###### Internal exact-reader adopter

- Must know: raw RPC, projection client, graph-query client, lifecycle reads/history, rule snapshots, and agent-run
  reads differ in validation, result, revision, absence, projection, poison, and history retention.
- If they do nothing: they can collapse absence distinctions, treat revision 0 as observed, inherit Manager-wide poison,
  or assume H1 supplies audit replay.
- Where they find out: separate packages/specs/docs.
- What they should have to know: the selected operation’s exact contract.

###### Deployment author

- Must know: registry uniqueness is by instance name only; different-named graph-ingest duplicates are admitted;
  graph-ingest ports are nonexclusive; one active process is nevertheless assumed for entity ordering; RPCs fan out;
  durable names are subject-derived; package metrics and some status keys are shared.
- If they do nothing: an admitted configuration can split updates, execute mutations repeatedly, race replies, replace
  local consumers, aggregate counters, overwrite gauges, and overwrite distributed status.
- Where they find out: registry/manager, port declarations, ADR-072, NATS wiring, and metric/status code.
- What they should have to know: whether a rendered topology is admitted and which instance each signal covers. Current
  admission does not encode same-type singleton safety.

###### Operator interpreting graph-ingest status

- Must know: local `Health`, local `Status`, `GRAPH_STATUS`, `COMPONENT_STATUS`, component-manager state, and Prometheus
  metrics are distinct. The GRAPH_STATUS catalog description omits actual graph-ingest/rule producers. Gauges are
  latest-writer; counters aggregate.
- If they do nothing: one signal can be mistaken for another, a gauge can mask an instance, a historical error can
  appear as current unhealth while status remains running, and a lifecycle-running component can lack its original
  consumer.
- Where they find out: component/readiness implementations, catalog, lifecycle reporter, and metrics.
- What they should have to know: exact instance and semantic dimension. Current surfaces lack common instance identity.

###### `message.Storable` author

- Must know: graph-ingest accepts refs without resolving them; `StorageInstance` requires a live local registry mapping;
  object shape is producer-defined.
- If they do nothing: authority can retain unresolved refs after drift, restart, registration loss, or object loss.
- Where they find out: Storable, graph-ingest, ObjectStore, and storeregistry.
- What they should have to know: logical handle and fetch contract.

###### Recovery operator

- Must know: account/context; writer stop boundary; authority; each storage instance and backing bucket;
  producer-specific object shape; binary children where applicable; stream identity; guards; derived buckets; restart
  order.
- If they do nothing: restored authority can contain unresolved refs, binaries can be omitted, raw objects can be
  misread, and guards can suppress replay.
- Where they find out: ADR-090, configuration, storage/producer code, catalog, and separate runbooks.
- What they should have to know: deployment-scoped inputs and completion evidence. Status, metrics, and live registries
  are not durable recovery evidence.

###### Product source/reseed owner

- Must know: Graphable replay is bounded catch-up, not disaster recovery; cutover depends on independent source; guards
  must match stream identity.
- If they do nothing: incomplete source prevents wipe/reseed and stale guards can suppress restoration.
- Where they find out: cutover and poison runbooks.
- What they should have to know: source and literal reseed operation.

##### Measured premises and test inventory

- `ENTITY_STATES` history is 1.
- Raw graph-ingest request validation is JSON plus non-empty ID, not canonical requested-ID validation.
- Raw graph-ingest success receives revision and returns value bytes only.
- `ReadAuthoritative` validates IDs but returns value-only `*EntityState`.
- Projection failures map to invalid/not-found/internal/unavailable; all are not committed.
- `Manager.GetRaw` is value-only.
- `Manager.GetWithRevision` returns lifecycle projection plus current authority revision.
- Lifecycle absence is `ErrEntityNotFound`; projection miss can be `ErrEntityNotLifecycleManaged`.
- Lifecycle poison latches the entire Manager and publishes guard not-ready.
- `Manager.History` calls KV `History`, while authority retains only one revision.
- The lifecycle spec claims revision-history audit behavior that `Manager.History` over H1 cannot supply;
  troubleshooting adds a broader operator expectation.
- Rule absence is synthesized as deleted revision 0.
- Agent-run predicate reads collapse missing entity and predicate.
- Raw GraphQL and graph-query results are value-only.
- `Health()` requires running, zero cumulative errors, no poison/watch loss, and completed bootstrap.
- Historical error can leave `Healthy=false` with `Status=running`.
- Graph-ingest metrics are package-level `sync.Once` singletons.
- Duplicate in-process instances aggregate counters and overwrite gauges.
- GRAPH_STATUS catalog description names graph-index/embedding but live keys include graph-ingest/rule.
- Registry uniqueness is instance-name-only.
- Resource conflicts check only exclusive ports.
- Graph-ingest KVWrite, NATSRequest, and JetStream ports are nonexclusive.
- Different-named graph-ingest duplicates are admitted.
- `EnforceOwnerLease` defaults false.
- Six shipped configurations enable owner-lease enforcement; the rest are observe-only.
- Owner-lease rejection occurs only for confirmed fenced mismatch when enabled; enumerated incomplete/legacy/error cases
  fail open.
- Owner lease is not graph-ingest process election.
- `COMPONENT_STATUS` census records 24 production writers and zero production readers.
- Nineteen graph-ingest declarations were found.
- Eight mutation RPCs use plain subscriptions.
- Same-process stream consumption uses `StreamName:ConsumerName` replacement.
- Keyed entity lanes are process-local.
- Storage registry state is process-local and not recovery metadata.
- References can point to raw bytes or StoredContent plus one-hop binaries.
- No test was found proving safe multi-instance graph-ingest ordering on shared subjects/durables.
- No test was found proving coordinated authority/content/stream/guard recovery.

##### Exact searches and empty results

Searches excluded the uncommitted GS-01 baton and ADR-090 where stated.

```text
rg -n -i 'snapshot.*ENTITY_STATES' \
  --glob '!docs/proposals/graph-state-read-write-program.md' \
  --glob '!docs/adr/090-authoritative-current-state-and-materialized-views.md' \
  --glob '!openspec/changes/establish-authority-read-and-recovery/**' .
```

Matches were boot snapshots, archived design, an old proposal sketch, and rule snapshots. No operator authority snapshot
implementation/runbook matched.

```text
rg -n -i 'restore.*ENTITY_STATES' [same exclusions] .
```

Only archived design text matched.

```text
rg -n -i 'backup.*ENTITY_STATES' [same exclusions] .
```

No matches.

```text
rg -n -i 'single.active.*graph-ingest' [same exclusions] .
```

No matches.

```text
rg -n -i 'graph-ingest.*single.active' [same exclusions] .
```

A decision draft and archived design matched; no implementation/config enforcement matched.

```text
rg -n -i 'graph-ingest.*replica' [same exclusions] .
```

Only ADR-072’s warning matched.

```text
rg -n -i 'authority.*revision' [same exclusions] .
```

Matches were proposal/concept/archived prose and unrelated retention review. No admitted raw-`EntityState` authority
response combining value and revision matched. This does not negate `Manager.GetWithRevision`, which returns a projected
lifecycle participant plus revision.

Additional inventories used:

```text
rg -n 'graph\.ingest\.query\.entity|GetEntity\(|/entities|entity.*handler|EntityQuery' \
  gateway service cmd
```

```text
rg -n '"graph-ingest"|type: graph-ingest|component_type: graph-ingest|graph-ingest:' \
  --glob '*.json' --glob '*.yaml' --glob '*.yml' \
  config configs test cmd examples docs
```

```text
rg -n 'QueryEntityNATS|Revision|replica|single.active|same durable|snapshot|restore|applied.*guard|watch.*lost|poison' \
  processor/graph-ingest/*_test.go graph/query/*_test.go test docs/operations
```

##### Conflicts and unresolved current-state gaps

- Raw graph-ingest and projection reads differ on canonical request-ID validation but are both value-only.
- Exact readers differ in validation, raw/projected result, revision, absence, poison scope, and dependencies.
- Admitted raw-`EntityState` reads omit KV revision.
- Lifecycle APIs split value-only raw access from revision-bearing projection access.
- Lifecycle absence and projection miss are distinct; other readers classify absence differently.
- Lifecycle poison is Manager-wide; graph-ingest query poison is inventoried per entity.
- The lifecycle spec claims KV revision audit history, but `Manager.History` over `ENTITY_STATES` H1 cannot retain
  multi-transition replay; ADR-090 rejects that audit-history claim. Troubleshooting adds only a broader operator
  expectation.
- Logical entity version and KV revision coexist without a raw public distinction.
- Rule absence synthesizes revision 0.
- Agent-run predicate lookup cannot distinguish missing entity from missing predicate.
- Local Health, local Status, GRAPH_STATUS, COMPONENT_STATUS, component-manager state, and metrics are different
  mechanisms.
- Historical error can leave `Healthy=false` while `Status=running`.
- Singleton counters aggregate duplicate instances; gauges carry latest writer.
- GRAPH_STATUS catalog owner prose omits actual graph-ingest and rule producer keys.
- Shared status/metric surfaces lack authority-instance identity.
- Component admission rejects only duplicate instance names, not duplicate graph-ingest type/factory.
- Graph-ingest’s authority-related ports are nonexclusive, so differently named duplicates pass resource-conflict
  admission.
- Owner-lease enforcement defaults observe-only and has explicit fail-open cases even when enabled.
- Semantic owner leases do not elect or fence graph-ingest processes.
- ADR-090 states coordinated recovery, but operational docs provide poison repair and destructive wipe/reseed only.
- Authority references do not imply one object shape.
- Storage registry state is process-local wiring, not persistent backing-bucket evidence.
- Graph-ingest validates neither live reference resolution nor referenced-object existence during boot or raw reads.
- ADR-072’s single-instance dependency is documented but not enforced.
- Mutation/query RPCs use plain subscriptions, allowing multiple execution/responders.
- Duplicate local consume identities replace an earlier context without necessarily changing lifecycle state.
- Backlog zero is consumer accounting, not independent proof that parked MaxDeliver messages were applied.
- Recovery docs depend on product-owned source authority and literal reseed commands.
- Live GitHub issue state was not queried; repository references do not establish current issue status.

This handoff stops at inventory. It contains no target state, options, recommendation, specification delta, or task
plan.

### Independent inventory-review verdict

**INVENTORY PASS.** The independent reviewer confirmed:

- runtime admission covers instance-name-only uniqueness and the nonexclusive KV, NATS, and JetStream ports;
- owner-lease behavior covers its observe-only default, enabled rejection, enumerated fail-open cases, and the six
  enabling configurations;
- lifecycle-history evidence directly ties the lifecycle specification and `Manager.History` to the H1 conflict,
  while troubleshooting records only the broader operator expectation;
- `GRAPH_STATUS` covers the catalog-owner attribution mismatch with live graph-ingest and rule producers; and
- the collision tables cover all inventoried exact-reader, catalog, health, readiness, lifecycle, metrics,
  admission, ownership, writer, recovery, referenced-content, and runtime-instance mechanisms.

The reviewer found no remaining same-class owner or blocking false claim. The evidence-precision correction above
is nonblocking and incorporated into the reviewed inventory.

## 5. Design placeholder — next after `INVENTORY PASS`

**Status: NEXT; NOT STARTED.** No options, recommendation, representation, interface, or runtime behavior is accepted
or proposed here.

The architect may now produce genuine options including do nothing and extension of an existing owner, measured
premises, adopter-seam consequences, triggered decision-skill outcomes, and a recommendation. The architect must then
stop for independent pre-owner design review.

### Independent pre-owner design-review verdict

PENDING — design has not started and cannot be reviewed.

### Owner decision

PENDING — the owner has not accepted a GS-01 design.

## 6. Implementation and promotion lock

There is no GS-01 capability delta, implementation plan, runtime code, or test
plan in this change. Creating or promoting one before a recorded
`DESIGN REVIEW PASS` and explicit owner acceptance violates this baton and the
canonical program.
