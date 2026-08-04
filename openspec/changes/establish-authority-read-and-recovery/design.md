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

## 5. Reviewed design — revision 13

**Status: DESIGN REVIEW PASS; REVIEWER CLEARANCE ONLY; OWNER DECISION PENDING.**

- **Artifact:** `/private/tmp/gs01-design-revision13.txt`
- **Artifact SHA-256:** `24f99453d108d4f8dd3b9b9879e7a0083a9ed6adc2eaf74bd3b5f3e124ff2103`
- **Baseline:** clean checkpoint `52dc5e3031131dda0a3a55c4de252b2df9d3d8fc`
- **Review verdict:** `DESIGN REVIEW PASS`, with no findings

The verdict clears revision 13 for an explicit owner decision. It is not owner approval and authorizes no capability
spec delta, runtime implementation, or spec promotion.

## CAP and pragmatism envelope

SemStreams is offline-first, edge-capable, and tiered. GS-01 chooses local authority availability over cross-tier
synchronous consistency:

- local `ENTITY_STATES` remains usable during upstream/cloud partition;
- cross-tier propagation and derived views are eventual;
- one statically admitted graph-ingest, per-primary Graphable ordering, effect-specific authority convergence, and
  durable progress constrain Graphable writes without blocking unrelated mutation RPCs;
- local revisions are currency only inside one authority domain;
- future cross-tier writers must declare entity/shard ownership and deterministic semantic conflict resolution, never
  arrival-time last-writer-wins;
- corrupt/unavailable authority, incomplete destructive maintenance, or target conflict fails closed;
- exact poison/broken references localize to the item, while projection poison remains sticky whole-view.

There is no end-to-end exactly-once promise. UDP is best effort. JetStream authority ingest is unlimited at-least-once.
A strict plan effect may partially commit before a later conflict; partial state and exact gap remain degraded.
Recovery is an offline observed reconstruction, not a transaction cut.

## Accepted inventory preservation

The independently reviewed fifth-pass surface inventory and adopter seam inventory in
`openspec/changes/establish-authority-read-and-recovery/design.md` §§1–4 remain byte-for-byte unchanged with
recorded `INVENTORY PASS`. Revision 13 is the post-inventory replacement. Every inventoried conflict remains live
unless explicitly dispositioned below.

Revisions 2–12 are superseded and carry no review pass or accepted target state.

## Recommended bounded increment

Recommend total revision-bearing exact reads; static graph-ingest admission; fixed consumer semantics; bounded
progress; deterministic staged AuthorityPlans with effect-specific convergence; flat status; immutable observed
checkpoint with whole physical ObjectStore closure; isolated restore with restore-owned capacity proof; canonical
authority-scan suffix resolution with no suffix bucket; one authority-domain recovery fact; and lifecycle History
removal.

Reject general client/raw application KV/MCP, compatibility decoder, online restore, global runtime lease,
transaction snapshot, audit ledger, general recovery workflow, and operator retry/timing/capacity knobs.

## Exact quality, projection poison, and flat status

- Malformed one-key authority returns `poison` plus observed revision; unrelated exact reads remain total.
- Valid entity with missing/broken content remains `found` plus bounded `content_unresolved` diagnostics naming logical
  storage instance/key.
- Absence, poison, content failure, projection absence, and backend unavailability remain distinct.
- Every projection keeps the binding contract: malformed watched authority latches whole affected view to sticky
  `reset_required`; no partial view serves healthy; owner reset/restart remains.

`graph.IndexStatusResponse` stays flat. Add `unavailable`, no wrapper:

- ready: authority/progress/source plans valid and no unresolved ingest/recovery fault;
- degraded: reachable authority with blocker/gap, partial/conflicted plan, incomplete recovery, unresolved content, or
  named derived failure; projection poison remains `reset_required`;
- unavailable: authority/progress invalid/unreachable, reconnect rebootstrap incomplete, or recovery control
  corrupt/unfinished; always `Ready=false`.

Unavailable hard-stops shared readiness gate, all readiness sets, and every fusion mirror for either bootstrap value.
Update enums, fusion type, metrics, catalogs, fixtures, clustering, and raw decoding together. Unknown JSON cannot
zero-decode success.

## Complete exact authority-read surface

```go
type AuthorityReadOutcome string

const (
	AuthorityFound       AuthorityReadOutcome = "found"
	AuthorityAbsent      AuthorityReadOutcome = "absent"
	AuthorityPoison      AuthorityReadOutcome = "poison"
	AuthorityUnavailable AuthorityReadOutcome = "unavailable"
	AuthorityCanceled    AuthorityReadOutcome = "canceled"
	AuthorityInvalid     AuthorityReadOutcome = "invalid"
)

type AuthorityReadRequest struct { ID string `json:"id"` }
type AuthorityReadBatchRequest struct { IDs []string `json:"ids"` }

type AuthorityDiagnostic struct {
	Code string `json:"code"`
	Class string `json:"class"`
	StorageInstance string `json:"storageInstance,omitempty"`
	Key string `json:"key,omitempty"`
	Message string `json:"message,omitempty"`
}

type AuthorityReadError struct {
	Code string `json:"code"`
	Message string `json:"message"`
	Retryable bool `json:"retryable"`
	Details map[string]string `json:"details,omitempty"`
}

type AuthorityReadResponse struct {
	Outcome AuthorityReadOutcome `json:"outcome"`
	ID string `json:"id"`
	Entity *graph.EntityState `json:"entity,omitempty"`
	Revision string `json:"revision,omitempty"`
	Diagnostics []AuthorityDiagnostic `json:"diagnostics,omitempty"`
	Error *AuthorityReadError `json:"error,omitempty"`
}

type AuthorityReadBatchItem struct {
	Index int `json:"index"`
	ID string `json:"id"`
	Result AuthorityReadResponse `json:"result"`
}
type AuthorityReadBatchResponse struct { Items []AuthorityReadBatchItem `json:"items"` }
```

Revision is same-entry base-10 uint64, required found, preserved observed poison, empty otherwise, distinct from
`EntityState.Version`. Valid decoded requests return total envelopes. Batch preserves count/order/index/ID; duplicate
valid IDs read once/repeat identically; invalid per-item; distinct keys not snapshot; empty `{"items":[]}`. Only
malformed top-level, classified pre-envelope failure, or total cancellation fails whole call.

```go
type AuthorityEntity struct {
	Entity *graph.EntityState
	Revision uint64
	Diagnostics []AuthorityDiagnostic
}
type AuthorityOutcomeError struct {
	Outcome AuthorityReadOutcome
	Code string
	Revision uint64
	Retryable bool
	Details map[string]string
	Cause error
}
type AuthorityItem struct {
	Index int
	ID string
	Entity *graph.EntityState
	Revision uint64
	Diagnostics []AuthorityDiagnostic
	Err *AuthorityOutcomeError
}
type EntityReader interface {
	ReadEntity(ctx context.Context, entityID string) (AuthorityEntity, error)
	ReadEntities(ctx context.Context, entityIDs []string) ([]AuthorityItem, error)
}
```

Single returns value only found. Other outcomes typed; poison keeps revision. Caller cancellation wraps context.
No-responder/disconnect/timeout is retryable unavailable unless caller context won, never absence. Internal subjects
remain entity/batch; malformed top-level classified invalid_argument; response formation failure internal.

Migrate graph-query, agentic-loop todos/graph_writer, agentic-tools emit_lesson, projection mutation client, graph
gateway
indirect path, tests/mocks. Replace `agentic/agentrun.NATSLoopTripleReader` with EntityReader-injected
`AuthorityLoopTripleReader` in both production binaries. Found predicate, found missing predicate, wrong type, entity
absent, poison, unavailable, canceled remain distinct. Search raw subjects and direct `ENTITY_STATES` acquisition;
application readers have neither. No alias/general client/MCP/KV fallback.

### GraphQL and #851

Shared Entity gets no revision. Exact root returns:

```graphql
type ExactEntityResult {
  entity: Entity!
  authorityRevision: String!
  diagnostics: [AuthorityDiagnostic!]!
}
```

Other roots remain valid without fabricated revision. Exact invalid/absent/poison-with-revision/unavailable/canceled are
typed operation errors; unresolved content returns data+diagnostics. Malformed HTTP/JSON 400, valid outcomes 200.

### Canonical suffix resolution without a derived index

GS-01 removes `ENTITY_SUFFIX_INDEX`, its catalog constant/row, graph-ingest bucket field, provision/open path, TTL
suffix cache, update/delete/backfill helpers, readiness assumptions, metrics, maintenance, and tests that treat it as a
source. No replacement persistent or lazy suffix index is introduced.

`graph.ingest.query.suffix` preserves its response and matching feature through a new operation-specific internal seam:

```go
type AuthorityKeyLister interface {
	ListAuthorityKeys(ctx context.Context) ([]string, error)
}
```

This is not `natsclient.KVStore.Keys`, `jetstream.KeyValue.ListKeys`, `jetstream.KeyLister`, or a general application
KV API. The pinned NATS adapter uses raw `jetstream.KeyValue.WatchAll` exclusively as
`WatchAll(ctx, jetstream.IgnoreDeletes(), jetstream.MetaOnly())`. It does not pass `IncludeHistory`, `UpdatesOnly`, or a
resume option: default DeliverLastPerSubject supplies current active keys and the initial snapshot marker, while
MetaOnly avoids value transfer. It drains `watcher.Updates()` with an explicit two-value receive:

```go
entry, ok := <-updates
```

The **only** completion condition is `ok && entry == nil`, the raw watcher’s end-of-initial-snapshot marker. If
`!ok` occurs before that marker, the listing is incomplete. A `KeyLister.Keys()` channel close is never accepted as
completion because the pinned wrapper erases whether closure came from the real marker, cancellation, or premature raw
watch closure. A future adapter may replace raw `WatchAll` only after owner review cites concrete API evidence for a
distinct, unambiguous completion signal.

The adapter checks `ctx.Err()` before setup, before/after every receive, at the marker, and again before return. It
stops the raw watcher on every exit. Entries before completion accumulate in an internal set using current-key operation
semantics. Sorting/deduplication and suffix matching happen **only after** the completion proof. `!ok` before marker
returns typed `authority_listing_incomplete`: canceled if caller context won, otherwise retryable unavailable for
internal timeout, disconnect, watcher failure, or unexplained closure. Setup failure is unavailable. No accumulated
prefix, empty result, or candidate ID escapes on any incomplete path.

After successful composition startup and the existing entity-query readiness check, each request validates a nonempty
suffix, calls `ListAuthorityKeys`, and, only from its proven-complete sorted result, returns the first key equal to the
suffix or ending in `"." + suffix`; no match returns the existing empty-ID response. This deliberately resolves IDs
without decoding entity bytes: a matching poisoned entity ID can be returned, and its subsequent exact state read
returns the typed poison outcome and revision.

Classification is explicit: malformed/empty request is invalid; completed no-match is a successful absent suffix
result; caller cancellation is canceled; every unproven listing is unavailable/canceled, never absence. No cache can
serve a deleted/stale mapping. A completed initial snapshot is not a global transaction with later writes; it is the
single operation’s total authoritative key catalog at its completion boundary.

Cost is `O(N)` authority keys and `O(N log N)` ordering per suffix request, with bytes/latency proportional to current
entity count and one watcher lifecycle per request. That bill is accepted for the bounded GS-01 clean break; a future
owner-specific increment may propose a properly owned, fully rebuilt suffix view.

Exact entity and suffix responders require only authority plus graph-ingest progress resources to initialize. This does
not create an authority-only partial-boot mode: the configured composition still publishes no external surface unless
all configured components pass the existing startup barrier.

#851 proof: read R; two ExpectedRevision=R writes; one commits R2; loser typed mismatch/current R2; refetch/recompute
can commit. Public docs include actual-type example. Closure remains owner action.

## Admission, fixed consumer, and resumable legacy cutover

Registration carries one internal `ENTITY_STATES` claim. Preflight rejects duplicates before factories, metrics, stores,
subscriptions, consumers, starts; init failure aborts. Same-host lock advisory only; no global lease.

Graph-ingest rejects generic consumer overrides. Framework owns AckExplicit, unlimited MaxDeliver, DeliverAll, one
durable/physical stream, InProgress, internal backpressure, and logical ceiling 3 completed transient failures. Filters
sort/dedupe; durable digest includes fixed contract; Description marker/digest. Overlap delivers once; random order
identical; only marker-owned obsolete durables removed.

Any legacy unmarked consumer/guard **or either exact obsolete suffix resource** independently causes
`legacy_ingest_cutover_required`: the literal `ENTITY_SUFFIX_INDEX` KV bucket or its exact expected backing-stream
identity. Detection uses exact direct lookups, never a prefix/list heuristic; a similarly named bucket/stream is
unrelated. Exact not-found proves absence; permission, disconnect, timeout, or ambiguous lookup failure is unavailable
and refuses rather than assuming absence. This narrow detector remains in startup/recovery tooling despite removal of
every normal suffix-index path.

Startup preflight, `authority recovery init`, and restore preflight run that detector before factories or mutation.
Each refuses with the cutover-required error and directs the operator to confirmed legacy teardown. Recovery init does
not create `AUTHORITY_RECOVERY` on such a target. No lifetime coupling to an old guard/consumer is assumed.

Offline teardown derives exact old name/config, replacement, bucket configs, sorted items, whole digest; confirms before
deletion. Bounded plan items/manifest live at:

```text
v1.control.legacy.<planDigest>.item.<zero-padded-index>
v1.control.legacy.<planDigest>.manifest
```

Manifest confirms only after all items/digests. No deletion before confirm. Pre-confirm absent old refuses.
Post-confirm:
create/verify replacements earliest retained; delete exact old (absence complete); delete old guard (absence complete);
verify/complete. The same confirmed cutover contains exact items for the literal `ENTITY_SUFFIX_INDEX` bucket and
expected backing stream: observe identity/configuration, remove the obsolete bucket through its owning API only after
confirmation, remove/verify any exact orphan backing stream under its own item postcondition, and verify both literal
resources absent. If one was initially absent, that item’s absence postcondition is already satisfied. No prefix
inference or deletion of similarly named resources is allowed. Item phases/postconditions make every crash resumable
elsewhere. Authority/unrelated consumers remain untouched. Startup/recovery init/restore remain refused until both exact
postconditions hold.

## Catalog descriptors

- **Bucket:** `GRAPH_INGEST_PROGRESS`
  **Owner:** graph-ingest authority progress runtime/offline control
  **Class:** operational
  **Retention:** no-lifecycle
  **Write:** owner-only
  **Posture:** owner-creates
  **History:** 1
  **Replicas:** 1

- **Bucket:** `AUTHORITY_RECOVERY`
  **Owner:** authority recovery command
  **Class:** operational
  **Retention:** no-lifecycle
  **Write:** owner-only
  **Posture:** owner-creates
  **History:** 1
  **Replicas:** 1

Both are literal `graph.KVCatalog` rows; guard/inventory derive. Generic update_kv literal/substituted rejects. Owners
use
EnsureFrameworkBucket; readers OpenFrameworkBucket. Graph-ingest and progress CLI share progress owner. Recovery/init
CLI is only recovery Ensure/write owner; server startup Open must-exist. Fresh target runs offline recovery init; legacy
cutover initializes. Missing recovery bucket fails startup. No TTL/binding MaxBytes.

The clean break deletes `BucketEntitySuffixIndex`/`ENTITY_SUFFIX_INDEX` from constants, `graph.KVCatalog`,
framework-owned guards/inventory, provisioning, checkpoint scope, and every graph-ingest read/write/maintenance path.
Only the exact legacy detector, teardown identifier/evidence, and precondition tests retain the literal. Legacy teardown
owns deletion; normal startup never adopts or recreates the resource.

## Bounded progress and projection determinism

Progress H1 keys:

```text
v1.entity.<entityDigest>.summary
v1.entity.<entityDigest>.source.<sourceDigest>
v1.source.<sourceDigest>.malformed
v1.plan.<workDigest>.<planAttemptID>.<planDigest>.chunk.<index>
v1.plan.<workDigest>.<planAttemptID>.<planDigest>.manifest
v1.control.legacy.<planDigest>.*
```

Fixed hashes/canonical bytes; values repeat identity/schema. At consumer bind capture stream+Created incarnation in
every
queued delivery. Before each authority effect compare live Created; lookup failure Naks/unavailable; mismatch gaps old
identity, never relabels, rebuilds binding.

Per-source value owns Applied/Settled, fixed 32 exact unresolved gaps, accepted count/hash/summary receipt. Entity
summary owns one Graphable blocker and accepted count/hash. No live-source cap. Gap capacity keeps blocker/degraded.
Summary orders Graphable deliveries only, not mutation RPCs. Source postcondition precedes clear:

1. CAS entity blocker; 2. reconcile source/staged plan; 3. execute plan; 4. CAS source outcome; 5. clear blocker; 6.
   Ack/Term.

Every crash fails closed; source conflict never clears. Restart scans blockers/gaps. Accepted loss is permanent
settled-not-applied suppression. Entity rolling digest is D0=SHA256(domain), Dn+1=SHA256(Dn||gapDigest), CAS serialized;
source receipt precedes clear. Corrected semantics republish; historical retry full reseed. Source retirement stopped
only when absent config, no blocker/gap, accepted receipt included, no consumer, and no retained old delivery.

Same immutable payload/envelope produces byte-identical EntityID/triples; with same exact base/config it produces same
plan. No admitted Triples calls wall clock/random/global mutable state. Timestamps use stable payload, BaseMessage
CreatedAt, or bound JetStream stored timestamp. Generated profile/hierarchy/stub facts use same ProjectionTime;
canonical
triple ordering is fixed.

Migrate trajectory step (Step.Timestamp), loop execution (stable serialized Task/envelope time), mission command
(serialized envelope time), document/iot/weather examples, StoredMessage, every registered production/test Graphable.
Double-project/canonical compare before staging; registry census projects twice and after marshal/decode. Static test
forbids time.Now in Triples. Persisted executing plan is retry authority.

## Pure AuthorityPlan and effect-specific convergence

`PlanAuthorityGraphable` is side-effect-free: exact-read bases, pure hierarchy/container/sibling/stub discovery, pure
foreign classification, canonicalization, size checks, and plan production occur before authority mutation. Graphable
lane never invokes side-effecting GetHierarchyTriples, hidden MergeEntity hierarchy/stub behavior, or warn-only routing.

Required effect closure:

- **Planned invariant:** Graphable-owned predicate replacements and owned envelope
  **Target:** primary
  **Effect class:** strict set, with compatible canonical-stub upgrade only

- **Planned invariant:** Create-time immutable indexing profile
  **Target:** primary
  **Effect class:** compatible immutable ensure: absent→create, identical→success, different→conflict

- **Planned invariant:** Forward hierarchy membership/sibling evidence
  **Target:** primary
  **Effect class:** monotonic canonical append

- **Planned invariant:** Canonical hierarchy container existence/type
  **Target:** type/system/domain container
  **Effect class:** compatible ensure/create

- **Planned invariant:** Container membership inverse
  **Target:** container
  **Effect class:** monotonic canonical append

- **Planned invariant:** Sibling inverse
  **Target:** observed sibling
  **Effect class:** monotonic canonical append

- **Planned invariant:** Referential target existence and stub evidence
  **Target:** absent relationship target
  **Effect class:** compatible ensure/create + monotonic referenced-by append

- **Planned invariant:** Foreign-subject evidence
  **Target:** admitted foreign target
  **Effect class:** monotonic canonical append; incompatible policy becomes terminal gap

Coalesce all intended deltas for one target into one canonical target effect while retaining each subeffect class.
Suffix resolution performs the canonical read-only authority-key scan defined above; AuthorityPlan execution has no
suffix side effect, derived bucket, cache, metric, readiness, or maintenance state. Other caches/metrics remain
process-local; poison clear is revision-guarded operational. Contract census rejects every hidden `ENTITY_STATES`
writer and every runtime/catalog `ENTITY_SUFFIX_INDEX` reference except the narrow exact legacy detector,
teardown literal/evidence, and their precondition tests.

### Effect rules and canonical assertion identity

**Strict set/replacement.** Persist exact base revision/value digest plus a canonical base projection of every
Graphable-owned predicate/envelope field. If current satisfies the complete intended owned postcondition, reconcile.
Otherwise the owned base projection must still equal the persisted projection; any changed owned value is terminal
`authority_plan_conflict`. A revision advance containing only compatible canonical appends or non-owned facts is not a
rebase of the strict set: preserve those current facts, apply the persisted replacement only to its declared owned
keys, validate, and CAS the current revision with bounded retry. CAS retry never adopts a changed owned base. Mutable
Version/UpdatedAt are excluded. Thus a sibling/container/foreign append cannot make a concurrent primary plan conflict,
while mutation B changing A-owned p from a to b still does.

If strict primary expected absence but current is a valid canonical profile-less referential stub only, it may perform
the reviewed stub-upgrade path: preserve all stub/referenced-by evidence, add strict primary facts, and CAS current with
bounded retry. A real/enveloped non-stub current entity is not compatible and follows strict conflict.

**Canonical append identity.** Every structural append/ensure and every admitted foreign-evidence append uses the
existing `message.AppendIdentityKey` contract, after the same canonical object normalization as the authority writer:

```text
(Subject, Predicate, Object, Datatype, Source, Context)
```

`Timestamp`, `Confidence`, and `ExpiresAt` are assertion annotations, not identity. When the six fields match, the
assertion is already present: preserve the first assertion and all of its annotations exactly, return success, and do
not treat a different ProjectionTime/confidence/expiry as a conflict or refresh. A different Datatype is a distinct
assertion. If a producer needs to change annotations for an existing identity, it must declare a replacement mutation;
the append lane never smuggles annotation replacement through convergence.

This same identity is intentionally used for structural and foreign evidence because both route through canonical
append semantics and both must converge under duplicate delivery. “Foreign” describes target ownership/policy, not a
different triple identity. Policy can reject an otherwise well-formed foreign target before planning; after admission,
the exact six-field identity has the same deterministic first-assertion rule. Canonical ordering uses
`AppendIdentityKey` and then stable full bytes only as a deterministic tie-breaker for distinct records; it does not
promote annotations into identity.

**Monotonic canonical append.** Reread the current valid compatible entity. Every intended six-field key already
present succeeds and preserves the first assertion. Otherwise preserve every current fact, append only missing keys,
validate the whole result, CAS current revision, and retry a fixed framework-bounded CAS loop on contention. Retry
exhaustion is transient logical-attempt failure, not semantic conflict. It never rewrites/removes another plan’s
facts.

**Compatible ensure/create.** For absent target, Create canonical container/stub. On create race, reread. Container is
compatible only with valid canonical hierarchy-container envelope/type; ensure missing required invariants using the
same six-field append key while preserving first-assertion annotations. Stub invariant is “valid target resolves”: a
valid real entity satisfies it; a compatible stub gains missing stub/referenced-by facts monotonically. Malformed or
wrong envelope conflicts; annotation variation on an existing append identity does not. Combined ensure+append uses one
bounded CAS transformation over current.

Thus concurrent different-primary births sharing a container each append their distinct contains fact; sibling inverses
append independently; two stub creators converge or accept a real target; foreign canonical evidence accumulates. No
ordinary shared target terminal-gaps merely because its revision advanced. Strict A primary partial then mutation B
sets p=b still conflicts on A retry, preserves B, never Applied/ready. Unrelated B fact can coexist if A postcondition
still holds.

## Versioned, abandonable plan staging

Plan effect contains index/class/target, persisted base revision/digest where strict, intended delta/postcondition, and
compatibility invariant. Plan includes work/projection/base/listing evidence, count/bytes/digest. Canonical chunks use
fresh random `PlanAttemptID` plus PlanDigest, so different replans never collide.

Manifest phases:

```text
staging -> executing -> settled
staging -> abandoned
executing -> conflict|terminal
```

Summary blocker always contains a complete tuple `{attemptID, planDigest, phase}`. There is no digestless blocker state.
During staging, Create/idempotent-identical chunks; manifest CAS to `executing` only after all chunks/count/combined
digest and full plan verify. **No authority effect is callable unless the manifest is executing.** That CAS is the
irreversible proof effects may have begun; executing bytes are frozen and retry uses them.

Restart with a staging attempt reconstructs from immutable message and current authority. If identical, finish chunks
and CAS executing. If authority/listing/config moved so original missing bytes cannot be reconstructed, CAS staging to
abandoned; because it never reached executing, zero effects began. Atomically roll the abandoned digest into the entity
summary, delete and verify every old attempt chunk/manifest, and retain an authoritative cleanup receipt in the blocker.

While that exact old cleanup receipt remains authoritative, compute the **complete** next `AuthorityPlan`, canonical
bytes, `PlanDigest`, and fresh `PlanAttemptID` in memory. Only after all planning, canonicalization, budgets, and digest
checks pass may one CAS replace the old receipt with the complete new `{attemptID, planDigest, phase=staging}` tuple.
Only after that CAS may the owner write the new attempt’s chunks and manifest.

Crash semantics are exact:

- before the blocker CAS, restart sees and resumes the old cleanup receipt; no new namespace is authoritative;
- after the blocker CAS but before any chunk/manifest, redelivery deterministically reproduces that same plan from
  immutable input and unchanged evidence, or marks that fully identified staging attempt abandoned and repeats the
  bounded cleanup/replan protocol;
- after manifest `executing`, the attempt is never abandoned or replanned.

Abandon cleanup is bounded: summary hashes `PlanAttemptID` and PlanDigest once into
AbandonedPlanCount/RollingHash; chunks and manifest delete/verify before a next attempt is planned. At most one current
staging/executing namespace and its in-progress cleanup receipt exists. Settled Applied/accepted gap similarly permits
plan deletion after source postcondition. Unresolved conflict retains executing plan for diagnosis/checkpoint.

Logical attempt CAS starts before executing effects; crash before consumes none, after resumes same plan. Under three
completed transient failures Nak; third gap/Settled before Term. Policy/invalid/oversize/conflict gaps. Unlimited server
delivery prevents parking.

## One effective encoded-size contract

```text
serverLimit = positive connected MaxPayload
streamLimit = observed MaxMsgSize when >0, else serverLimit
localKVLimit = configured KVOptions.MaxValueSize when >0,
               else DefaultKVOptions().MaxValueSize (1 MiB)
effectiveKVLimit = min(serverLimit, streamLimit, localKVLimit)
valueBudget = effectiveKVLimit - 8192
```

Nonpositive fails pre-effect; unlimited stream sentinel falls back server. Owner seam exposes actual local option used
by
Create/CAS. Apply consistently to progress/source/plan/cutover, recovery, authority. Full plan total budget is
min(server, positive source MaxMsgSize else server)-8192; each chunk fits progress budget; each target result authority
budget. Exact one-byte boundary tests preflight/write agree.

## Reconnect and offline CLI

Async disconnect callbacks remain observers. No barrier/new ordering API. Automatic reconnect, unlimited delivery,
progress, staged plan, bound incarnation, effect-specific CAS, unavailable/unknown converge.

`semstreams authority ...` parses before server construction, uses same config/credentials, one command/report/exit,
no components/metrics/ports/HTTP. Commands: recovery
init/checkpoint/inspect/validate/restore/resume/abort/wipe/reconcile;
progress legacy-teardown/accept-loss/reseed/retire. No live control/MCP/daemon/general executor.

## Immutable checkpoint with whole-ObjectStore closure

Checkpoint is content-addressed and not globally atomic. It captures configs, exact authority values/revisions/digests,
all progress keys, source/consumer evidence, diagnostics, and catalog completeness.

Authority/progress use two full catalogs: config/Created/first-last before, sorted keyset and entry bytes/revisions,
config/first-last after. Movement triggers up to two additional full passes. Complete only when final two catalogs and
backing evidence match and every selected key reads. Final motion never complete. Select final readable exact value,
final absence, or unreadable/incomplete.

### Shape-neutral content closure

There is no universal StoredContent/Storable object shape. GS-01 deliberately does not decode envelopes, BinaryRef,
children, or future layouts. From every selected valid authority entity, resolve distinct framework `StorageInstance`
through rendered configuration. Map each logical instance to its physical NATS account/domain/ObjectStore bucket and
deduplicate aliases by physical identity. For **every referenced resolved framework physical ObjectStore, capture the
entire current store**, not selected direct objects.

Full store observation records bucket/backing stream config, Created identity, first/last stream sequence before/after,
sorted complete object-name inventory, every object’s supported metadata/headers/size/digest, and exact raw bytes. It
uses ObjectStore APIs plus backing stream evidence; envelopes, binary children, raw objects, unrelated objects, and
arbitrary future Storable layouts are copied without interpretation.

Perform two full observations per governed store, bracketed with authority catalogs. Re-read each object metadata after
bytes. Any added/deleted/replaced object, sequence/config movement, unreadable object, or unsupported nonroundtrippable
metadata is named and triggers bounded recapture under the same four-pass maximum. Complete requires final two
authority keysets, resolved physical store set, and every governed store inventory/metadata/digest to match. A new final
StorageInstance without stable whole-store capture makes catalog incomplete.

Direct referenced key absent remains found+missing diagnostic even though the rest of store is captured. Unresolved
logical instance, external/non-framework backend, or store that cannot enumerate every object is an explicit incomplete
gap. A poison authority record whose storage instance cannot be decoded is likewise `content_scope_unknown` and cannot
support a complete content-closure claim. `--accept-incomplete` retains degraded provenance; default restore refuses.
No storage-author callback, child enumerator, or object-shape knowledge is added.

### Native NATS ObjectStore links

Native links are metadata, not ordinary object bytes. Observation inspects `ObjectInfo`/`ObjectMeta.Opts.Link` before
any byte read because `ObjectStore.Get` dereferences object links and therefore cannot prove the link object itself.
The exact metadata and target identity are part of the bundle inventory.

The supported boundary follows the pinned `nats.go` native API exactly:

- only a direct same-physical-bucket object link with nonempty `Name` whose target is a captured **concrete object** may
  be preserved. Normalize an omitted/same-bucket Bucket to the current physical identity, record link metadata without
  dereferencing it as link bytes, inspect the named target’s metadata, require `Opts.Link == nil`, and require that
  target’s stable metadata and exact raw bytes in the same whole-store capture;
- if the named target is itself a link, classify the outer object as
  `native_object_link_unsupported` before restore claim. This applies even to an acyclic same-store chain because
  pinned `ObjectStore.AddLink` rejects link-to-link with `ErrNoLinkToLink`;
- a cross-bucket object link, including a logical alias resolving outside the captured physical bucket, is the same
  exact incomplete gap;
- a bucket link (`Name == ""`), missing concrete target, unreadable link/target metadata, or other non-round-trippable
  link is the same gap, with source bucket/name, target bucket/name, and reason evidence.

GS-01 does not recursively widen scope through native cross-bucket links and does not introduce raw/manual link-metadata
writes. It never calls `Get` and stores dereferenced target bytes under the link name, because that would silently
change link semantics. Default restore refuses any unsupported native link. With explicit `--accept-incomplete`, the
claim persists the exact degraded gap and the unsupported link is a declared omission; restore creates neither a
dangling link nor a materialized substitute and cannot claim content-complete provenance.

Restore creates and verifies every concrete object first. Each supported decorated direct-link item then follows this
persisted crash-resumable state machine in canonical link-name order:

```text
pending -> link_created -> metadata_updated -> verified
```

From `pending`, native `AddLink` creates only Name plus the concrete target link option. The item re-reads
`ObjectInfo`, requires the exact generated same-store target and no unexpected user metadata, then persists
`link_created`. From there, native `UpdateMeta` applies the captured Name, Description, Headers, and Metadata; the
pinned API preserves `Opts.Link`. Re-read must prove those user fields byte-for-byte and prove the generated link
option is unchanged before `metadata_updated` and `verified`.

Only one canonical-order link is current at a time. The recovery claim persists that link name, target identity,
captured metadata digest, generated-link-option digest, and subphase; completed predecessors are re-proven from their
exact postconditions on resume. This bounds control state without losing crash identity or adding another bucket.

Resume observes before acting. Absent link repeats `AddLink`; an existing exact-target link advances to or resumes
`UpdateMeta`; exact decorated metadata advances to verify. Wrong target, changed link option, unexpected metadata, or
name collision is a terminal target conflict, never overwritten. Crash after either publication resumes from observed
postcondition. Whole-store comparison distinguishes links from concrete objects and verifies the target concrete digest.
A server/client version that cannot inspect, create, update, preserve, or round-trip this direct-link metadata converts
the item to `native_object_link_unsupported` before claim.

This resolves the accepted-inventory shape conflict by widening physical scope rather than pretending to understand
logical shapes. Cost is honest: bundles may include large unrelated objects and can approach the entire referenced
store size; capture/verification time and local disk grow accordingly. Supported direct links add metadata verification
but no duplicate target bytes. Simplicity and pinned-API round-tripability are preferred over callbacks or raw link
writes.

## Isolated whole-store restore and recovery provenance

H1 `AUTHORITY_RECOVERY` key `v1.ENTITY_STATES` is account-local; one authority domain/account. Claim stores sorted full
governed authority/progress/physical ObjectStore scopes, bundle/attempt/phase, exact recovery gaps, accepted hash. Every
restore collides regardless of content selection; no TTL/takeover/runtime lease.

`semstreams authority recovery init` is a separate offline command that ensures only the recovery bucket. Restore itself
performs no target NATS mutation before the deterministic capacity plan and recovery claim. Before either claim or any
other restore mutation, canonicalize every possible provenance gap and encode the worst claim under the common
per-value budget; never truncate/page unresolved truth. Oversize is a typed report with no claim/mutation.

### Account-wide capacity preflight

Per-object sums are insufficient. Before claiming, build one canonical `AccountRestoreStoragePlan` over the complete
target account/domain and exact bundle. It records the observed account/config identity, telemetry revision/time where
available, storage class, replication multiplier, resource counts, each encoded component, safety reserve, and a
whole-plan digest. Its proof scope is every write owned by the offline authority-restore command: authority, progress,
recovery, governed ObjectStore, and graph-ingest consumer resources. It explicitly excludes post-complete writes by
derived projection owners. The plan includes conservative upper bounds for:

1. every authority KV value/message, subject and header bytes, backing-stream message overhead, and replicas;
2. every restored progress/source/plan/cutover/control KV entry with the same overhead and replicas;
3. the largest recovery claim plus all restore and current-link item subphase rewrites, conservatively counting every
   phase message that can coexist under actual H1/backing-stream behavior and all subject/header/storage overhead;
4. every ObjectStore concrete data chunk and object metadata message; for each supported direct link, both the
   `AddLink` metadata publication and the later `UpdateMeta` publication with captured Description/Headers/Metadata;
   all backing-stream message/subject/header overhead, retained crash-phase upper bounds, stream state, and replicas
   under the target client/server rules;
5. creation/config state for every governed authority/progress/ObjectStore backing stream that is absent on the empty
   target after recovery init; the already-initialized recovery bucket is current usage, not silently free;
6. fixed graph-ingest-owned durable consumer state and metadata needed by the restore; and
7. resulting JetStream stream and consumer counts, not only bytes.

Derived graph-index, embedding, clustering, rule, graph-query, fusion, lifecycle, and other projection/view stores are
not restore outputs and are not charged as planned additions. Preflight nevertheless proves every cataloged derived
store/stream absent or empty as an isolation prerequisite and reports the excluded owner scopes. Their future rebuild
capacity is intentionally not claimed by `AccountRestoreStoragePlan`.

Message, stream, consumer, and object overhead constants come from the actual pinned NATS server/client encoding or an
owner-reviewed executable sizing oracle. They are version-bound evidence in the plan, not guessed averages. For each
file- and memory-storage class, add a fixed nonconfigurable safety reserve:

```text
reserve = max(1 MiB, ceil(plannedAdditionalBytes / 20))
```

This 5%/1-MiB reserve is framework policy pending owner approval; adopters do not tune it. Counts use their exact upper
bounds rather than a percentage.

Preflight obtains authoritative JetStream account information for current file/memory usage and finite quotas, stream
and consumer counts/limits, plus the configurations and replication of every target stream. Missing, stale,
contradictory, permission-hidden, or version-unsupported telemetry yields typed `target_capacity_unknown` and refuses
before claim. An explicit server “unlimited” sentinel is known-unlimited and passes that dimension; it is not
treated as unknown. The plan compares, per dimension:

```text
currentUsage + plannedAdditionalUpperBound + reserve <= finiteQuota
currentCount + plannedAdditionalCount <= finiteCountLimit
```

Every finite dimension must pass. Object content fitting alone cannot pass account capacity. The report names storage
class, current, planned, reserve, limit, source telemetry, and enforcing layer. If target state or telemetry changes
before claim CAS, rebuild the plan; the claim stores the accepted plan digest/evidence.

Account-wide proof complements rather than replaces local limits. Preflight still checks each exact authority value
against authority KV/server/stream/local limits; each progress/control value against progress limits; each possible
recovery claim/phase value against recovery limits; and each ObjectStore data chunk, concrete-object metadata,
`AddLink` publication, and decorated `UpdateMeta` publication against connected MaxPayload, positive backing
MaxMsgSize, client/local object constraints, and positive bucket MaxBytes. Exact one-byte boundary behavior must match
both native write calls.

Capacity can race after claim due to unrelated account writers. A post-claim allocation/quota failure records the exact
scope and observed usage as a fenced `target_capacity_changed` unknown/incomplete recovery attempt; it never reports
success or silently retries beyond the plan. Resume requires new authoritative evidence and the recovery state-machine
postcondition for the current phase; any required plan replacement remains owner-governed and cannot bypass isolation.

### Restore phases, completion, and configured-startup boundary

Restore runs without runtime components. Before claim it proves authority, progress, every cataloged derived store/view,
and every governed physical target ObjectStore absent or empty. Separately, it requires **literal absence**, not
emptiness, of both the exact obsolete `ENTITY_SUFFIX_INDEX` bucket and expected backing stream. Either legacy resource
causes `legacy_ingest_cutover_required` before capacity claim or any restore mutation. The other derived checks are
isolation postconditions only: the offline CLI neither creates derived resources nor invokes their owners.

After capacity proof, claim, and isolation, restore every captured concrete object with supported metadata and bytes;
then execute each supported direct link’s `AddLink -> UpdateMeta -> verify` item; verify the full sorted inventory
contains no extras/omissions and every metadata/size/digest/link target matches. Restore authority including poison,
matching progress and recovery gaps, and fixed graph-ingest consumers at earliest retained. Validate only those
restore-owned resources plus continued derived/suffix absence, then mark authority recovery complete. No pre-complete
derived rebuild exists.

Phases remain
`claimed -> validated_empty -> content_restoring -> content_restored -> authority_restoring -> authority_restored ->
progress_and_consumers_restoring -> progress_and_consumers_restored -> validating -> complete`.
The per-link item subphases are inside `content_restoring` and their worst simultaneous native publications are in the
accepted capacity-plan digest. Prewrite abort is allowed after proven emptiness. Postwrite abort requires explicit wipe
of all recorded restore-owned scopes followed by CAS-clear. Server startup checks recovery before bind/admission and
refuses corrupt or unfinished recovery; it is allowed to attempt startup only after `complete`.

GS-01 does **not** add a general optional-component, partial-boot, or failure-domain lifecycle framework. Normal
configured composition startup remains all-or-fail: every configured component must allocate and start successfully
before the composition root binds external HTTP/query surfaces. Exact and suffix reads therefore serve only after
successful full composition startup, although their own graph-ingest initialization uses authority and progress
resources only. If any other configured graph-index, embedding, clustering, rule, fusion, lifecycle, or gateway
component cannot allocate/start, boot aborts and neither exact nor suffix external service is promised.

That post-complete boot failure is a control-plane/resource failure, distinct from localized entity poison, missing
content, or broken references after a successful start. Recovery completion remains valid: the operator fixes quota or
configuration and retries normal startup; restore is neither reopened nor retroactively failed. Once startup succeeds,
exact outcomes retain their localized classifications and suffix uses the deterministic authority scan. Derived
operations and shared readiness then follow their existing component contracts; GS-01 does not invent an independent
status for a component that prevented the process from starting.

The restore capacity proof ends at `complete` and covers restore-owned writes only. Later configured-component
allocation is explicitly outside that proof. A later startup quota failure aborts that startup attempt and is reported
as such; it does not claim per-owner service availability, change the recovery fact, or imply derived capacity was
preflighted. A future increment may design owner-specific preflight or lifecycle isolation, but GS-01 does neither.

Default restore refuses incomplete/unstable store or authority evidence. Explicit accept-incomplete persists gaps and
starts authority service degraded only after a later successful composition start. Missing direct content stays
found+diagnostic/degraded; still-observable missing cannot be accepted. Changed/lost source, evicted work,
external/unresolved store, and unsupported native links persist. Reconcile clears only proven postconditions.

## Lifecycle History clean break

H1 authority is not audit. Remove Manager.History, gateway member, `/history`, OAS/schema/replay claims; old route 404.
Preserve GetRaw/GetWithRevision/current state/transitions/restart/poison. Replacement E2E restart current
phase/revision.
Draft issue dispositions unchanged: #681 ADR-090, #821 current restart, #843 after replacement green, #888 remains.

## Decision skills

- query-pattern: exact remote HTTP/GraphQL; embedded EntityReader only; no MCP/raw/general client/KV fallback.
- kv-or-stream: authority/status/progress/recovery KV facts; Graphable JetStream; exact request/reply; no ledger.
- orchestration-check: reconnect component-local; restore/cutover offline bounded CAS commands; no workflow.
- new-payload: not triggered; envelopes/progress/plans/claims typed records.

## Adopter seam inventory

- **Adopter:** Exact caller
  **Must know:** Local revision; service starts only after full composition startup
  **If nothing:** Exact result revision after successful boot
  **Discovery:** Schema/startup errors
  **Should know:** Value/revision/outcome

- **Adopter:** Embedded
  **Must know:** EntityReader
  **If nothing:** Raw callers migrate
  **Discovery:** Compiler
  **Should know:** One operation

- **Adopter:** Suffix caller
  **Must know:** Resolution requires an end-marker-proven total key scan
  **If nothing:** Incomplete watch returns canceled/unavailable, never partial/absent
  **Discovery:** API/performance docs
  **Should know:** No index readiness

- **Adopter:** Graphable author
  **Must know:** Deterministic projection
  **If nothing:** Nondeterminism gaps pre-write
  **Discovery:** Contract/tests
  **Should know:** Stable fields

- **Adopter:** Ingest config
  **Must know:** Filters only
  **If nothing:** Many streams/shared targets converge
  **Discovery:** Boot
  **Should know:** No retries/sizes

- **Adopter:** Mutation caller
  **Must know:** Strict plan may conflict, never overwrite
  **If nothing:** ExpectedRevision unchanged
  **Discovery:** Status/errors
  **Should know:** Own intent

- **Adopter:** Recovery operator
  **Must know:** Whole stores/links copied; exact legacy suffix resources must be absent
  **If nothing:** Any literal legacy resource refuses before mutation and directs cutover
  **Discovery:** CLI totals/report
  **Should know:** Stores/bundle/attempt

- **Adopter:** Storage author
  **Must know:** Nothing about object layout
  **If nothing:** Entire resolved store closes children/future shapes
  **Discovery:** Manifest
  **Should know:** No callback

- **Adopter:** Native-link user
  **Must know:** Direct same-store links restore through AddLink then metadata UpdateMeta
  **If nothing:** Link-to-link/external/bucket links default-refuse or are explicit degraded omissions
  **Discovery:** Checkpoint/restore item report
  **Should know:** Native support status

- **Adopter:** Account operator
  **Must know:** Capacity proof covers restore-owned writes; configured startup is separately all-or-fail
  **If nothing:** Restore can complete before a later startup quota failure
  **Discovery:** Capacity/startup reports
  **Should know:** Fix quota/config and retry startup

- **Adopter:** External store owner
  **Must know:** Backend must be framework-resolved/enumerable
  **If nothing:** Incomplete gap/default refusal
  **Discovery:** Report
  **Should know:** Resolution status

- **Adopter:** Configured component
  **Must know:** Full composition startup remains all-or-fail
  **If nothing:** Any component start failure leaves external surfaces unbound
  **Discovery:** Startup error
  **Should know:** Existing composition contract

- **Adopter:** Lifecycle
  **Must know:** History gone
  **If nothing:** Compile/404
  **Discovery:** Compiler/routes
  **Should know:** Current revision

- **Adopter:** Cross-tier
  **Must know:** Ownership/conflict rule
  **If nothing:** No writer
  **Discovery:** Future
  **Should know:** No global revision

Framework observes actual stores/inventories/native-link metadata/limits/account quotas/source identity/projection/base/
postconditions/keysets/digests/gaps. Adopters do not predict Ack/retry/durable/bucket/child
layout/quiet-time/TTL/takeover/
readiness or capacity reserve.

## Exact cost and complexity delta from revision 12

Added/refined:

- `AuthorityKeyLister` is pinned exclusively to raw `jetstream.KeyValue.WatchAll` and an explicit
  `entry, ok := <-updates` receive; only `ok && entry == nil` proves completion.
- Future substitutes require cited, owner-reviewed API evidence for a distinct completion signal.

Removed/narrowed:

- the pinned `ListKeys`/`KeyLister` alternative and the vague “equivalent terminal signal” clause;
- any interpretation of a derived key-channel close as successful initial-snapshot completion.

This changes no public or adopter surface. It makes the already selected total-listing implementation mechanically
capable of observing the completion proof and retains the O(N) materialization/O(N log N) suffix cost.

All other revision-12 contracts remain unchanged: CAP, exact read/GraphQL/#851, direct reader migration, static
admission, fixed consumer, resumable cutover, exact legacy detector, catalog ownership, bounded source progress,
deterministic projection, pure effect inventory, six-field append identity, strict replacement/stub upgrade,
complete-before-CAS staging, flat unavailable/sticky poison, whole-store observation, decorated direct-link restoration,
restore-owned capacity proof, all-or-fail configured startup, canonical no-index suffix semantics, isolated recovery
claim/phases, lifecycle History removal, and rejection of optional-component lifecycle/global lease/general client/MCP/
ledger/compatibility/workflow/online restore.

## Required verification

- exact read/adapter/GraphQL/direct acquisition/#851 matrices unchanged;
- status/fusion hard-stop and sticky projection tests after successful startup;
- catalog owner Ensure/reader Open/update_kv/inventory tests;
- fixed consumer, many sources, source incarnation, progress crash/accept/retire tests;
- deterministic Graphable registry and cited migrations;
- pure effect inventory/no hidden authority writers;
- canonical append, concurrent shared-target, strict A→B preservation, and staging crash matrices unchanged;
- plan/source/progress/recovery/authority one-byte limit boundaries;
- decorated direct-link AddLink/UpdateMeta metadata, crash, capacity, and unsupported-link matrices unchanged;
- whole-store drift/alias/incomplete closure and finite account quota/unknown telemetry matrices remain;
- raw-WatchAll `AuthorityKeyLister` RED: deliver one key then cancel before nil marker; return canceled and no
  keys/result;
- raw-WatchAll RED: deliver one key then close `Updates()` before marker (`ok == false`) with live context; return
  retryable `authority_listing_incomplete` unavailable and no keys/result;
- raw-WatchAll RED collision: deliver lexical-later matching key, omit lexical-earlier match, then cancel/close before
  marker; never return later key or empty-ID success;
- marker matrix proves only `ok && entry == nil`, followed by final context check, permits sort/dedupe and lexical-first
  matching; `ok && entry != nil` continues and `!ok` never completes;
- raw adapter contract pins `WatchAll(ctx, IgnoreDeletes(), MetaOnly())`, excludes IncludeHistory/UpdatesOnly/resume,
  ignores deleted/purged keys, and still receives the initial nil marker;
- contract fixture around pinned `KeyLister.Keys()` proves its channel close is never wired into or accepted by
  AuthorityKeyLister as completion; production construction contains no `ListKeys`/`KeyLister` path;
- timeout/disconnect/setup failure/marker-race/context-at-marker tests prove explicit classification and no partial
  keys;
- completed total scans preserve full-ID, instance, type.instance, exact-key, partial-no-match, empty, poison-ID, and
  collision semantics; every request uses a fresh operation-specific listing and no suffix cache/index;
- exact-resource matrix with no old consumer/guard: empty literal `ENTITY_SUFFIX_INDEX` bucket only, exact orphan
  backing stream only, and both each make startup, recovery init, and restore refuse before mutation with
  `legacy_ingest_cutover_required`;
- confirm recovery init creates no recovery bucket/claim on those refused targets; restore creates no capacity claim or
  small write; confirmed teardown resumes through deleting each exact resource and only then permits operations;
- similarly named bucket/stream never triggers or is deleted; exact detector/teardown are the only permitted literal
  runtime references;
- fresh and migrated boot census proves exact/suffix initialization opens only authority/progress resources and no
  suffix bucket/cache/write/readiness path remains;
- restore requires literal suffix bucket/backing-stream absence, not empty, reaches complete with other derived stores
  absent/empty, and retains all-or-fail subsequent startup behavior;
- post-complete configured-component failure/retry, lifecycle History, and all retained E2E/race gates remain.

**Breaking gate.** Removing `ENTITY_SUFFIX_INDEX` is a clean-break storage/API-internal migration. Before landing the
breaking commit, run the relevant full ingest→authority→query E2E tier (at minimum `task e2e:semantic`) and the
exact/suffix integration matrix above. Grep every binary and package for both the constant and literal bucket name;
only the exact legacy detector, teardown identifier/evidence, and their tests may remain. Verify both `cmd/semstreams`
and `cmd/e2e-semstreams` refuse suffix-only legacy state, then start after teardown and resolve suffixes through the
total authority scan. Regenerate schemas and prove no uncommitted spec/schema delta. If the E2E tier does not cover
restored-authority suffix resolution and raw-WatchAll partial-list closure, record those coverage gaps before promotion.

## Revision-12 finding-resolution note

- **Revision-12 finding:** HIGH: pinned `ListKeys` cannot expose an unambiguous completion proof
  **Revision-13 resolution:** Production AuthorityKeyLister uses raw `WatchAll` only. Explicit `entry, ok` receive
                              accepts only `ok && entry == nil`; `!ok` is incomplete. ListKeys/KeyLister and vague
                              equivalent clauses are removed; future substitution requires cited distinct-signal
                              evidence

## Updated owner rulings requested

1. Accept local availability/eventual CAP envelope after successful configured startup, partial strict conflict as
   durable degraded, and no global transaction/exactly-once.
2. Accept strict replacement, canonical six-field append, compatible ensure/create, pure deterministic AuthorityPlan,
   complete-before-CAS staging, and no hidden authority writer.
3. Accept whole-store capture, decorated direct AddLink→UpdateMeta restore, direct-target-only native-link boundary,
   restore-owned capacity proof, and incomplete/default-refusal rules.
4. Accept the operation-specific total `AuthorityKeyLister`: production uses raw `jetstream.KeyValue.WatchAll`
   exclusively; only `ok && entry == nil` releases sorted keys, while `!ok`/cancel/timeout releases no result. Any
   future substitute requires cited distinct-completion-signal evidence and owner review.
5. Accept clean removal of the suffix index/cache/write/readiness surface and lexical-first total authority scan with
   O(N) keys/O(N log N) cost.
6. Accept exact legacy suffix bucket/backing-stream presence as an independent mandatory cutover condition; startup,
   recovery init, and restore refuse before mutation until confirmed exact teardown, with similarly named resources
   untouched.
7. Accept derived-empty offline isolation, recovery completion before normal startup, and existing all-or-fail
   configured startup; later boot failure does not undo recovery completion.
8. Accept the breaking E2E/census/raw-WatchAll partial-list and suffix-only legacy-resource gates before promotion.
9. Accept all revision-12 rulings for fixed consumer/progress/budgets/catalogs/exact reads/status/recovery/lifecycle.
10. Confirm no general optional-component lifecycle, general KV/client/MCP/raw app KV/compatibility/event ledger/online
    restore/global lease/general workflow or adopter retry/timing/capacity knob.
11. Confirm all named integration/race gates and coverage-gap rule before promotion.

## 6. Independent pre-owner design-review verdict

**DESIGN REVIEW PASS.** The independent reviewer reported no findings against the exact revision-13 artifact and
recorded baseline/hash above. This is reviewer clearance only, not owner acceptance.

## 7. Owner decision

**PENDING.** The owner has not accepted, rejected, or redirected revision 13.

## 8. Implementation and promotion lock

There is no GS-01 capability delta, implementation plan, runtime code, or test plan in this change. Creating or
promoting one before explicit owner acceptance violates this baton and the canonical program.
