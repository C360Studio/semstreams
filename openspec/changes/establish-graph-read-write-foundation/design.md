# Approved design — establish graph read/write foundation

> This is the mechanically adopted content of GS-01 revision 39, source SHA-256
> `4399e4f50ffcfa90c32d12ff4667e5c3797150194ed509a7d01c9a5620c16c3e`. It received independent
> `DESIGN REVIEW PASS` and owner acceptance of all sixteen rulings on 2026-08-05. The durable decision evidence is
> [approval.md](approval.md). Mechanics live in this OpenSpec change and ADR-091; this record does not broaden the
> accepted target.

## Design identity and gates

- Repository evidence baseline: `45746d98fb1c1db4ce0ae9ee431da68cbae4b398`.
- Design date: 2026-08-05.
- Accepted inventory: [inventory.md](inventory.md), promoted byte-for-byte from
  `semantic-ownership-removal-inventory-r37.md`, SHA-256
  `fb90cfa1af9789d2c767013c17554aff57d8c79b03f41e76c2ef2da13d923f32`, 406 lines, 46,211 bytes.
- Inventory review: [inventory-review.md](inventory-review.md), promoted byte-for-byte from
  `semantic-ownership-removal-inventory-r37-review.md`, SHA-256
  `c420d0ca6767da37772a4c862ffa3f72a55b67f0ecac8d799d5e9a3892b3ccca`, 28 lines, 1,524 bytes;
  verdict `INVENTORY PASS`.
- The accepted inventory is incorporated by content identity, without summary substitution. Every collision, adopter
  seam, current-spec dependency, empty `PENDING_EDGES` finding, and no-DR/no-leader/no-CQRS boundary in those exact bytes
  remains controlling evidence for this design.
- Approval gate: discharged and recorded in [approval.md](approval.md). Implementation proceeds only
  through the fresh `establish-graph-read-write-foundation` OpenSpec record, one implementation PR, its review gates,
  and the single coordinated breaking cutover. The superseded
  `openspec/changes/archive/2026-08-05-establish-authority-read-and-recovery` investigation is archive history, not a
  second implementation baton.

## Problem statement

SemStreams currently combines two different controls:

1. graph-ingest is the physical writer of current `ENTITY_STATES`; and
2. `pkg/ownership` predicts which semantic producer may write which predicate, using claims, presence, heartbeats,
   incarnation tokens, foreign-edge modes, overlap checks, quiescence, boot services, configuration, and wire fields.

The second control is incomplete and unevenly enforced, while adopters still need explicit mutation intent, lost-update
protection, exact authority reads, and typed outcomes. The design question is whether those correctness properties need
semantic ownership at all, or whether they belong in the mutation operation and its observed result.

## Binding boundary supplied for this design

This design records the following approved owner direction:

- Any component may request a graph mutation. A request is not a predicate-ownership claim.
- graph-ingest is the only physical writer to `ENTITY_STATES`; “writer” names bucket topology, not semantic authorship.
- NATS request/reply remains the command front, expressed through component ports as the canonical mutation API. No new
  KV watch, JetStream work stream, command ledger, or outbox is introduced.
- A mutation against an absent entity returns typed `entity_not_found`; the caller decides whether and when to retry.
- graph-ingest never creates an entity merely because another entity references its ID.
- An edge to an absent object is valid eventual graph state. Absence becomes visible when a reader dereferences it.
- Atomic `Create` for birth plus observed-revision KV CAS for every existing-key write, on both graph-ingest write
  lanes, prevent lost updates. Semantic owner leases and local keyed dispatch do not.
- SemStreams gains no checkpoint, backup, restore, recovery gate, leader election, CQRS split, pending-edge queue, or
  exactly-once subsystem.
- This is a pre-v1 clean break: no aliases, old subjects, dual shapes, legacy readers, token tolerance, or mixed-version
  promise.
- One graph-ingest process is the supported deployment topology. Multi-process graph-ingest is unsupported; this design
  does not invent runtime election or distributed writer fencing.
- `GRAPH_STATUS` remains the existing graph readiness/poison territory and is unchanged.
- Downstream code changes and compatibility work are outside this design artifact. Release coordination includes only
  the bounded communicate-only census in §11; its findings neither redesign nor block this increment.

## Measured premises

| Premise | Measurement | Consequence in this design |
|---|---|---|
| The inventory gate passed on exact bytes. | Inventory/review identities above. | Design may frame options; inventory collisions cannot be silently narrowed. |
| Eight mutation subjects exist and use plain Core NATS request subscribers. | `processor/graph-ingest/mutations.go:22-135`; `natsclient/request.go:342-405`. | Keep request/reply, reduce the operation set, and accept one supported responder process. |
| The provider does not declare its mutation API as a port. | `processor/graph-ingest/component.go:501-525` declares only the `entity_stream` JetStream input and `ENTITY_STATES` KV-write output, while `setupMutationHandlers` independently hardcodes and subscribes all eight mutation subjects at `processor/graph-ingest/mutations.go:22-135`. | Move the provider family and interface into graph-ingest's input ports; handler setup must resolve and validate the declared port rather than create an invisible side channel. |
| Shipped configuration describes the same command seam three different ways. | Graph-ingest flow entries use plain `nats` with `graph.mutation.>` in eight flow/example files; graph-gateway defaults use `nats-request` with `graph.mutation.*` at `gateway/graph-gateway/component.go:172-201`; additional rule/action configs also spell `graph.mutation.*`. | Standardize the protocol-family declaration on `nats-request` plus `graph.mutation.>`; exact operation leaves come from the typed protocol, not from a misleading one-token wildcard. |
| `nats-request` interface metadata is representable but lost during port construction. | `component/port_nats.go:71-77` has `NATSRequestPort.Interface`; `PortDefinition.UnmarshalJSON` preserves typed config at `component/ports.go:99-104`; `BuildPortFromDefinition` reconstructs only Subject and Timeout at `component/ports.go:247-255`. | Preserve and validate the flat or typed interface contract so composition can match protocol identity as well as a subject family. |
| Flow-graph matching already recognizes a concrete request under a `>` provider family. | `component/flowgraph/flowgraph_validation_test.go:679-715`. | Extend static validation to the canonical interface/family and provider cardinality; do not mislabel that composition check as runtime leader election. |
| Mutation operations already differ materially. | Accepted inventory §2; `graph/mutation_requests.go:15-159`. | Do not replace them with a generic upsert or arbitrary patch. |
| Only lifecycle supplies nonzero `ExpectedRevision`. | Production search returned exactly `pkg/lifecycle/manager.go:732,972,1103`. | Reconcile/delete callers need an exact read; gated-DAG is a migration consumer, not proof CAS is already general. |
| Exact RPC and projection reads omit the KV revision, while lifecycle and one direct-KV tool can observe it. | Accepted inventory §8; `processor/graph-ingest/query.go:60-105`; `pkg/projection/mutation_client.go:956-984`. | Add one exact value-plus-same-entry-revision operation, not another general graph client. |
| Semantic ownership is a large runtime substrate. | `pkg/ownership`: 28 files, 4,599 lines (2,178 production; 2,421 tests). | A genuine removal must delete the package, not leave a compatibility shell. |
| Ownership boot/service code is material. | `service/ownership_service.go` plus its tests: 866 lines; accepted inventory §4. | Remove service/liveness wiring while preserving the independent catalog backstop and graph-state guard. |
| Owner/token/bucket spellings are cross-cutting. | 462 non-archive matches in 79 Go/JSON/Markdown files at the accepted worktree inspection. | Wire, config, schema, specs, docs, and tests change in one clean break. |
| Six shipped configurations explicitly enforce leases despite the schema default being false. | Accepted inventory §4 table; six exact config lines. | Delete the field and all six explicit settings; do not mistake schema default for shipped behavior. |
| Automatic stubs and foreign-edge routing are independent of ordinary mutation absence. | Accepted inventory §6; `component.go:1924-2010,2739-2885`; `mutations.go:634-680`. | Remove stub birth/restamp and claim-driven foreign routing separately. |
| `PENDING_EDGES` has no live bucket, reader, writer, or drain. | Accepted inventory closing search. | Add no replacement queue or durable broken-reference registry. |
| `GRAPH_STATUS` already has 160 code references in graph/runtime packages and owns readiness. | `graph/kvcatalog.go:76-81`; accepted inventory collision table. | Add no mutation/ownership status bucket and do not alter readiness semantics. |
| Current authority is History 1, not an audit or DR source. | `graph/kvcatalog.go:67-74`; ADR-090; accepted inventory. | No command log, replay claim, restore design, or CQRS event store. |
| The two authority-write lanes do not yet share one discipline. | Graphable work is locally keyed at `processor/graph-ingest/keyed_ingest.go:125-170` and its merge uses retrying CAS at `processor/graph-ingest/component.go:2464-2466,2530-2534`; RPC handlers bypass that pool (`rg -n 'ingestPool' processor/graph-ingest/mutations.go` returned zero), while `CreateEntity` upserts with `Put` at `component.go:2693-2707` and `UpdateEntity` does the same at `component.go:2913-2914`. | Make observed-revision CAS/atomic Create the storage invariant on both lanes and retire unconditional `ENTITY_STATES` Put paths. The keyed pool remains a local throughput tool, so no coordinator, lock service, or election follows. |
| Opt-in hierarchy currently writes from both lanes and has real side effects. | Graphable merge invokes `GetHierarchyTriples` at `processor/graph-ingest/component.go:2510-2526`; shared `createEntity` invokes it at `component.go:2665-2678`; hierarchy creates containers and inverse edges at `graph/inference/hierarchy.go:343-450`; the adapter currently routes container birth through upserting `CreateEntity` at `component.go:567-572`. `configs/e2e-structural.json:278-288` enables it and `Taskfile.yml:53-54` admits its E2E tier. | Retain hierarchy only on Graphable ingest, give its existing writes the same Create/CAS discipline, and prove that disposition in the existing structural tier. No derived-view subsystem is introduced. |

## Decision-skill outcomes

### `kv-or-stream`

The mutation is a request to perform a side effect, which ordinarily points to a JetStream work stream. That is not a
new design choice here: SemStreams already admits synchronous NATS request/reply as its mutation command seam, callers
need the immediate typed result, and the owner direction retains it. The result is therefore:

- keep Core NATS request/reply;
- expose it through declared `nats-request` component ports, not hidden subscriptions;
- add no KV watch (a command is not current fact);
- add no JetStream work stream or durable consumer (no queued-work/replay requirement is admitted);
- make loss of the reply an explicit `commit_unknown` client outcome instead of pretending the command was exactly once.

### `query-pattern`

Exact entity authority is one named operation, not a general embedded graph client:

- remote callers use the admitted GraphQL entity operation;
- embedded callers use one operation-specific typed adapter;
- raw KV, raw subjects, `graph/query.Client`, and MCP are not application fallbacks;
- the answer source is graph-ingest authority and returns value plus the same entry's KV revision.

## Options and costs

Costs are stated before the recommendation.

### Option A — do nothing

Keep `pkg/ownership`, owner buckets, heartbeat/revival service, tokens, foreign-edge modes, automatic stubs, all eight
subjects, and value-only admitted exact reads.

Costs:

- every component author continues to learn owner IDs, claim overlap, heartbeat posture, token enforcement, target-birth
  modes, and which read can supply a CAS revision;
- six shipped configs reject stale tokens while the schema's omission path remains observe-only;
- gated-DAG remains unconditional and #851 remains unsatisfied;
- graph-ingest's mutation responder remains invisible to its port declaration, requester/provider configs keep
  disagreeing on `nats` versus `nats-request` and `*` versus `>`, and interface metadata remains discarded;
- multiple graph-ingest processes still race Core NATS replies despite semantic producer leases;
- little migration cost now, but all accepted inventory collisions remain.

### Option B — remove runtime semantic ownership; retain a local projection mutation schema

Delete the global claim/lease/liveness substrate and automatic stub behavior. Retain a smaller `projection.Contract` as
a local client-side declaration of entity shape and allowed mutation operations. Global overlaps are legal; a contract
protects only the component holding that client.

Costs:

- one breaking migration touches both binaries, graph-ingest, projection, lifecycle, rules, agentic tools, gated-DAG,
  generated schemas, six shipped configs, current specs, ADR status, docs, tests, and E2E;
- graph-ingest and every requester must expose the same typed `nats-request` family port; port construction, flow
  validation, and shipped configuration must migrate with the operation schemas;
- `replace-owned` terminology and token-aware errors disappear, requiring rule/config edits;
- components that reconcile/delete must first use the exact read and handle revision conflicts;
- no global registry rejects two components that intentionally or accidentally reconcile the same predicate; correctness
  comes from explicit CAS and each operation's semantics, not author identity;
- a local contract remains code and schema to maintain, but it preserves preflight safety and least-privilege interfaces.

### Option C — broader projection simplification

Delete semantic ownership and `projection.Contract`; expose only raw typed create/reconcile/append/delete/exact-read
operations. Lifecycle, rules, tools, and products each validate their own patterns and predicate sets.

Costs:

- greatest immediate deletion, but it creates multiple local spellings of predicate selection, full-set replacement,
  birth authorization, message type, and indexing profile;
- rule-pack preflight loses its single source of mutation target truth;
- component authors must predict removal sets and validate entity patterns themselves;
- duplicated validation increases the chance of partial multi-value replacement and out-of-pattern mutation;
- broadest external API and migration burden despite the smallest internal package count.

### Option D — extend and harden the existing ownership substrate

Make enforcement mandatory, finish pending-edge handling, and use ownership liveness to constrain graph-ingest writers.

Costs:

- retains or expands the 4,599-line ownership package and the adopter's registry/heartbeat/token bill;
- requires a pending queue or another dropped-edge policy, contrary to the admitted boundary;
- conflates semantic producer identity with graph-ingest process coordination;
- approaches election/fencing and mixed-version rollout machinery the owner excluded;
- does not by itself supply exact reads or explicit mutation/lost-reply semantics.

## Reported-approved decision

The thread handoff reports owner acceptance of **Option B**.

It deletes the predictive, globally coordinated semantic-ownership layer while preserving the part of projection that
prevents local callers from constructing malformed or overly broad mutations. Correctness is located at three explicit
seams: a declared typed component port, an exact authority read, and an observed mutation outcome. It is smaller than
hardening ownership and safer for adopters than deleting projection validation wholesale.

The review verdict and owner ruling recorded in [approval.md](approval.md) are the approval authority.

## Reported-approved target contract

### 1. Authority and topology

- `ENTITY_STATES` remains a cataloged authoritative bucket whose physical writer is graph-ingest.
- Components never write the bucket directly. Any component may send an admitted mutation command.
- Exactly one graph-ingest process is supported. The framework neither detects nor elects among multiple processes.
- `GRAPH_STATUS`, poison handling, bucket retention, and current-state History 1 remain unchanged.
- Catalog “owner” continues to mean the component responsible for a bucket's write/retention seam; it does not imply
  predicate ownership or authorize a NATS caller.
- Cross-lane invariant: every write to an existing `ENTITY_STATES` key—Graphable JetStream ingest, an RPC mutation, or
  an opt-in hierarchy inverse—commits by CAS against bytes read at a specific revision. A genuine birth uses atomic
  `Create`. No production path retains unconditional `Put`-as-upsert semantics.
- The keyed ingest pool serializes local Graphable work as a throughput optimization. RPC handlers need not enter that
  pool, because CAS is the correctness boundary across goroutines, processes, and lanes; no shared lock, coordinator,
  lease, or election is added.

### 2. Component-port mutation protocol

The component port graph is the canonical mutation API declaration. Directly subscribing or requesting a
`graph.mutation.*` string without a declared typed port is outside the contract.

One clean-break interface contract identifies the family:

```text
type:    semstreams.graph.mutation
version: v1
family:  graph.mutation.>
```

- graph-ingest declares one required **input** `nats-request` port with that interface and family.
- Every mutation requester declares a required **output** `nats-request` port with the same interface and family.
- `graph.mutation.>` is the one configuration/discovery convention on both sides. `>` truthfully denotes a protocol
  family whose operation leaves are below the prefix; `graph.mutation.*` is deleted because it falsely suggests that
  every operation is exactly one token below `mutation`.
- A canonical operation registry maps the four typed operations to exact suffixes (`entity.create`,
  `entity.reconcile`, `triple.append`, `entity.delete`). Provider setup and the typed requester both resolve exact
  subjects from their declared family plus that registry. Neither owns a second hardcoded subject table.
- Startup rejects a missing mutation port, the wrong direction/type/interface/version, a noncanonical family, or any
  resolved leaf that falls outside the declared family. `setupMutationHandlers` receives only the validated four exact
  subjects and may not silently fall back to constants or the port name.
- `BuildPortFromDefinition` preserves `NATSRequestPort.Interface`: typed `Config.Interface` wins when present; otherwise
  the flat `PortDefinition.Interface` constructs the v1 contract. Round-trip and flow-graph validation must retain it.
- The wire has four distinct request structs and four distinct response structs, generated schemas for each, and a
  shared closed server classification vocabulary (`applied`, `unchanged`, `entity_not_found`,
  `entity_already_exists`, `revision_mismatch`, `invalid`). The typed requester adds transport outcomes
  (`unavailable`, `deadline`, `commit_unknown`) that a server reply cannot honestly assert. The subject selects the
  operation; there is no generic patch payload or untyped `map[string]any` command.
- Static flow validation allows many compatible requester outputs but requires exactly one compatible provider input for
  this interface/family in a validated flow. Zero is an unresolved required dependency; more than one is an ambiguous
  provider topology.
- That cardinality rule validates the declared flow only. It neither detects another graph-ingest process elsewhere in
  the NATS account nor provides leases, fencing, queues, failover, or election. The deployment contract still supports
  one graph-ingest process, and operational enforcement remains outside this design.
- This port remains Core NATS request/reply. It creates no JetStream mutation stream, durable consumer, replay log, or
  queued-work guarantee.

### 3. Smallest mutation algebra

The target has four command operations. Each request carries `request_id` for correlation only; it is not an idempotency
key and creates no deduplication store.

#### `CreateEntity`

Purpose: birth one entity atomically with its metadata and complete initial primary-subject triples.

- Input: entity ID, valid semantic envelope, initial triples, optional indexing profile, trace/request correlation.
- Every supplied triple has the new entity ID as Subject. A relationship Object may name an absent entity.
- Precondition: entity ID is absent. KV `Create` supplies the race break.
- Existing entity returns typed `entity_already_exists`; invalid shape returns typed invalid.
- No hierarchy target, inverse edge, foreign-subject edge, or referential stub is created as a hidden side effect.
- Success returns the exact committed entity and nonzero committing KV revision.

This RPC operation is the only public birth seam. It never invokes hierarchy inference. The old non-strict
`CreateEntity` upsert and whole-entity `UpdateEntity` `Put` paths are retired rather than retained as internal bypasses.

#### `ReconcilePredicates`

Purpose: make a declared set of predicates equal a complete desired set on one existing entity.

- Input: entity ID, **required nonzero** `expected_revision`, the exact predicate set being reconciled, complete desired
  triples for those predicates, and correlation.
- Omitted desired values for a named predicate delete that predicate. Predicates outside the declared set are untouched.
- The desired set may be empty; this is the sole predicate-removal operation.
- Missing entity returns typed `entity_not_found`.
- Revision mismatch returns typed `revision_mismatch`; graph-ingest does not silently switch to unconditional retry.
- If the entity exists, the revision matches, and the desired set already holds, success reports `changed=false` and the
  current nonzero revision without claiming a write occurred.
- Otherwise one CAS commit returns `changed=true`, the committed entity, and exact committing revision.

#### `AppendTriples`

Purpose: append exact evidence tuples to existing subjects without replacing sibling values.

- Input: one or more canonical triples plus correlation. Identical six-field tuples are deduplicated.
- The request may group multiple subjects; graph-ingest CAS-retries independently per subject because exact-tuple append
  is convergent. No caller revision is required.
- Every absent subject returns typed `entity_not_found`; no entity or stub is born.
- The response contains one result per distinct subject: `applied`, `deduplicated`, or typed failure, plus the exact
  committing revision for applied subjects. It never represents a cross-subject transaction.
- Partial success is explicit. A caller retries only selected failed/unknown subjects with the same canonical tuples.

#### `DeleteEntity`

Purpose: remove exactly one existing entity without racing a newer update.

- Input: entity ID, **required nonzero** `expected_revision`, and correlation.
- Missing entity returns typed `entity_not_found`; delete is no longer absent-success.
- Revision mismatch returns typed `revision_mismatch`.
- Matching revision performs conditional delete and returns the deleted entity ID, `applied`, and the matched
  `expected_revision`. NATS KV acknowledges the conditional delete but does not return a delete-marker revision, so the
  response claims no such value. A lost/ambiguous acknowledgement becomes `commit_unknown`. No relationship cascade,
  stub, inverse cleanup, or reachability repair is implied.

#### Opt-in hierarchy on the Graphable lane

Hierarchy remains an opt-in semantic projection of a six-part Graphable entity ID, not an RPC-create behavior. On the
Graphable ingest lane only, first birth may derive membership triples and real hierarchy-container entities. Containers
are inferred entities with explicit `entity.type.class=hierarchy.container`; they are not referential stubs. A missing
container is born with atomic `Create`, and concurrent existence is an ordinary create race. Container inverse and
sibling inverse edges target must-exist entities and use the same CAS append path as other existing-key writes.

Hierarchy may leave a relationship whose object is absent if a companion write fails; that is valid eventual graph
state and is surfaced on dereference. It does not trigger a stub, repair queue, rollback protocol, or global failure.
This increment neither moves hierarchy to a derived-view subsystem nor broadens it to RPC requests.

### 4. Disposition of all eight current subjects

This is a clean replacement, not an alias set.

| Current subject | Target disposition |
|---|---|
| `graph.mutation.entity.create` | Retain the subject name for unified `CreateEntity`, but replace its request/response shape with the complete atomic birth contract. This is breaking. |
| `graph.mutation.entity.create_with_triples` | Delete. All callers move to unified create; no compatibility subscriber. |
| `graph.mutation.entity.update` | Delete. Bare whole-entity update has no explicit predicate operation and is not retained. |
| `graph.mutation.entity.update_with_triples` | Replace with `graph.mutation.entity.reconcile`; required revision and complete predicate-set semantics remove the zero-revision mode. |
| `graph.mutation.entity.delete` | Retain the subject name with a breaking fenced, must-exist shape; absent now returns typed not-found. |
| `graph.mutation.triple.add` | Delete. Single append is a one-triple `AppendTriples` request. |
| `graph.mutation.triple.add_batch` | Replace with `graph.mutation.triple.append`; response is explicitly per subject and partial across subjects. |
| `graph.mutation.triple.remove` | Delete. Predicate removal is `ReconcilePredicates` with an empty desired set; absent predicate on an existing matching-revision entity is `changed=false`, while absent entity is typed not-found. |

Old subjects and old JSON fields receive no compatibility handlers. A stale client fails at transport/compile time during
the coordinated pre-v1 cutover rather than silently selecting old behavior.

### 5. Mutation outcomes and lost replies

The wire proves only what graph-ingest can causally report:

- successful reply: `applied` or `unchanged`, with the operation's exact entity revision evidence;
- classified reply: definitely not applied for invalid, not-found, exists, or revision-mismatch precondition failures;
- no responder before delivery: unavailable/not-applied according to the NATS primitive's classified guarantee;
- timeout, disconnect, malformed/lost reply after possible delivery: `commit_unknown` at the typed-client boundary.

There is no exactly-once claim, request-ID ledger, authorship inference from matching content, or automatic retry after an
ambiguous possible delivery. The client may observe exact authority and report “desired state currently observed,” but
that observation does not prove this request committed it. The caller chooses whether to stop, reconcile from the newly
observed revision, or retry an operation whose semantics make that acceptable.

`graph_mutation_outcomes_total{operation,outcome}` is the one bounded command signal; `revision_mismatch` is an outcome.
A structured mismatch log names operation, entity ID, and expected revision, never entity ID as a metric label. A high
mismatch rate is the fighting-writer diagnostic; no detector, registry, ownership substitute, or alerting service is
introduced.

### 6. Exact authority read

One result type carries canonical value and same-entry revision:

```go
type ExactEntity struct {
    Entity     *graph.EntityState
    KVRevision uint64
}
```

Contract:

- one authority bucket `Get` yields both fields from the same KV entry;
- success always has a non-nil validated entity and nonzero `KVRevision`;
- absence is typed `entity_not_found`, never `(nil, nil)` and never revision zero;
- poison, unavailable, deadline, and invalid-ID outcomes retain classified errors;
- `EntityState.Version` is logical metadata and is never accepted as `KVRevision`;
- reads do not mutate, repair, create stubs, or change `GRAPH_STATUS`.

Remote placement: the admitted GraphQL entity operation returns `{entity, kvRevision}`. It does not bless literal-colon
HTTP routes, raw provider JSON, MCP, or `graph/query.Client` as an adopter contract.

Embedded placement: one narrow operation-specific `ExactEntityReader` adapter sends the exact query and returns
`ExactEntity`. `pkg/projection`'s current value-only `AuthoritativeReader` migrates to this result; lifecycle may consume
the adapter where it needs raw authority, while its workflow projection remains lifecycle-owned. There is no general
embedded graph client and no raw-KV fallback.

### 7. Referenced-object absence and dereference

- Mutation validates the syntax of relationship facts, not whether the Object currently exists.
- No relationship-target stub, foreign-edge mode, inverse gate, pending record, delayed drain, or auto-repair is created.
- Exact dereference of the Object ID returns the ordinary typed `entity_not_found` when absent.
- Batch hydration and traversal preserve the source edge and explicitly list unresolved IDs/reasons in their existing
  missing/unknown result shapes. They do not omit the edge or fabricate a target.
- A later real `CreateEntity` makes the next dereference resolve. Nothing is replayed because the source edge already is
  current graph state.
- A component that wants to mutate the referenced entity sends an explicit command to that entity; absence is its typed
  not-found result, and that component chooses retry.

This is observation rather than prediction: the writer does not guess target birth order, and the reader reports the
real target state at the moment dereference is requested.

### 8. Local projection contract after ownership removal

Retain `projection.Contract` as local mutation schema, not global authority:

- retain `Name`, `EntityPattern`, optional `MessageType`, `BirthPredicates`, named predicate groups,
  `IndexingProfile`, validation, immutable copied configuration, narrow creator/reconciler/appender/reader interfaces,
  and canonical post-operation verification;
- replace `ownership.WriteMode` with local operation values `reconcile` and `append`;
- collapse `replace-owned` and `cas-transition` into revision-required `reconcile`;
- rename `append-evidence` to `append`;
- delete `ForeignEdges` and every `ownership.EdgeMode`; cross-subject work is an explicit operation against that subject;
- delete `Derive`, `Bind`, `BindAndHeartbeat`, owner ID, Registry, Heartbeater, token, presence posture, overlap checking,
  quiescence, revival, and ownership error translation;
- overlapping contracts in different components are valid. A contract does not grant or deny global permission;
- rule-pack `PackID` remains required for graph-event producer identity and duplicate event identity, but no longer derives
  `rule-pack.<PackID>` ownership or triggers projection binding;
- rule-pack preflight still freezes contracts and validates every `reconcile` action against an exact contract/group/
  predicate target before start or hot reload;
- a rule reconcile that receives `revision_mismatch` performs one fresh exact read and one retry; a second mismatch is a
  visible action failure. This fixed bound is not a configuration knob and does not apply after `commit_unknown`;
- `LessonCurator`, `write_todos`, and rule actions keep their narrow mutation interfaces over a locally constructed client.

### 9. Complete semantic-ownership and stub retirement

#### Runtime and durable state

- Delete all 28 `pkg/ownership` files and their 4,599 lines.
- Remove `OWNER_CLAIMS` and `OWNER_PRESENCE` constants and catalog descriptors; existing buckets are removed during the
  clean-break wipe/reseed procedure. Delete the declaration-only `PENDING_EDGES` constant.
- Delete ClaimReader wiring, owner lookup, token comparison, foreign-edge classification/modes, revival/quiesce, presence
  heartbeat, overlap/waiver/inverse gates, and their tests.
- Delete `OwnershipService` and its tests.
- Split `WireOwnershipSubstrate`: retain `AssertOwnedBucketsClean` in a neutrally named boot/catalog function; delete
  Registry creation, lifecycle attachment, and heartbeater return.
- Delete `WireOwnership`; both mains construct the local projection mutation client directly from NATS and copied built-in
  contracts.
- Replace `WireOwnershipShutdown`/`WaitOwnership` with graph-state-guard-specific cancel/join only; no heartbeat remains.
- Replace `BindRulePackContracts` with local preflight/client construction and `OwnedReconciler` injection, with no owner,
  registry, heartbeat, token, or cross-pack claim overlap.

#### Config, generated schema, wire, metrics, and docs

- Delete graph-ingest `enforce_owner_lease`, its generated schema property/default, and explicit `true` from all six
  shipped configs.
- Add graph-ingest's typed `nats-request` provider input; convert every mutation requester output from plain `nats` or
  `graph.mutation.*` to the canonical typed `nats-request` `graph.mutation.>` family. Delete hidden subject fallbacks.
- Preserve `NATSRequestPort.Interface` through definition construction/JSON round trips and require the mutation
  interface/version in flow validation and generated component configuration.
- Delete both `OwnerToken` request fields, token JSON tests, `owner_lease_stale`, ownership-mismatch metrics/logs, and
  lease rollout documentation. Add only the bounded mutation-outcome counter and structured revision-mismatch log from
  §5.
- Delete `foreign_edge_unclaimed_total` and claim-mode drop/defer metrics. Ordinary append not-found and dereference
  missing outcomes remain observable through operation/query metrics.
- Remove Registry/Heartbeater/Owner fields from projection client configuration and remove semantic modes/foreign-edge
  declarations from the rule generated schema.
- Update the lesson reference rule pack/README from `replace_owned`/boot owner registry to local `reconcile` semantics;
  evidence-existence validation and lifecycle predicates remain.

#### Automatic stub behavior

- Delete `graph/stub.go`, stub predicates/type, `IsStub`, relationship target walker, `ensureReferencedEntityExists`,
  stub restamp exception/counter, claim-driven no-birth-stub route, and their tests.
- Strict create now conflicts with every existing real entity and has no stub exception.
- gated-DAG and lesson promotion delete stub filtering; they handle ordinary exact-read not-found instead.
- lifecycle `ReferenceStub` is renamed/reframed as an unresolved relationship reference derived from the source entity;
  it does not claim a target entity exists.

### 10. Current-spec and ADR disposition

Current specs, not ADR prose, become target mechanics through the deltas in
`establish-graph-read-write-foundation`.

| Artifact | Approved disposition |
|---|---|
| `projection-mutation-client` | Rewrite around local contracts, four operations, exact read, CAS, partial append, and `commit_unknown`; delete Registry/presence/heartbeat/token/foreign-edge posture/enforcement requirements. |
| `rule-projection-mutations` | Replace owner binding and `replace-owned` with local preflight plus `reconcile`; preserve exact static target selection, receipts, hot-reload immutability, and narrow injection. |
| `lifecycle` | Delete ownership-overlap, owner-token, lease, incarnation/quiesce requirements; preserve workflow registration, exact revision reads, CAS transitions, operator error propagation, and independent graph-state guard. |
| `graph-ingest` | Delete stub first-arrival/restamp and claim-driven foreign-edge behavior; specify no target birth, explicit four-operation semantics, typed absence, CAS, exact read, and the required typed provider input port whose declaration controls handler subjects. |
| `graph-events` | Delete `rule-pack.<PackID>` owner/binding rationale; retain required PackID, grammar, duplicate identity failure, and event lineage. |
| `graph-retention` | Remove `OWNER_CLAIMS`/`OWNER_PRESENCE`; preserve catalog owner-only acquisition and all remaining bucket policies. |
| `predicate-contract` | Delete registry-derived owner map and semantic authorization language; preserve canonical predicate validation and separation from authentication. |
| `entity-id-contract` | Remove semantic-ownership configuration references; preserve shared entity/pattern validation. |
| `graph-index` | Replace ambiguous “semantic ownership” wording with row provenance/retraction responsibility; preserve owner-key/filter/reconciliation mechanics. |
| `graph-state-contract` | Preserve projection poison and physical/catalog graph-bucket ownership; clarify it is not predicate ownership. |
| `framework-composition` and component-port schema | Require one compatible mutation provider input per validated flow, allow many compatible requester outputs, preserve request interface metadata, and standardize `graph.mutation.>`; state explicitly that this is static topology validation, not leader election. |
| `graph-view-subscription`, `nats-kv-keys`, `storage-observability`, `stream-provisioning` | Preserve their distinct package, view, catalog, migration, attribution, and provisioning uses of “owner”; edit wording only where needed to prevent semantic-claim implication. |

ADR disposition follows the project's history rule: semantic rulings cease to govern, but historical files are not erased.

- ADR-056: mark superseded in full by the accepted replacement decision; predicate claims, leases, foreign-edge modes,
  inverse/pending/stub decisions are retired.
- ADR-055: supersede its eight-lane mechanics with the four-operation algebra while retaining the fact/request distinction
  and graph-ingest physical-write boundary.
- ADR-058: supersede ownership-specific boot phases/service/shutdown sections; retain the general lifecycle composition
  lessons that still apply.
- ADR-060: remove `owner_lease_stale`; keep classified RPC errors. `commit_unknown` is a typed-client transport outcome,
  not a server claim that a mutation failed.
- ADR-068/073: remove retired ownership buckets from catalog evidence; live graph no-TTL policy remains.
- ADR-090: unchanged; authority remains current value, not audit or DR.
- A replacement ADR is warranted only for the irreversible external mutation/read contract and semantic-ownership
  retirement. Operation mechanics live in specs.

### 11. External clean break

- Announce one breaking SemStreams release and update every in-repo caller, generated artifact, configuration, example,
  and test in the same cutover.
- Remove old subjects/types/fields; do not ship aliases, compatibility subscribers, dual JSON, legacy token acceptance,
  bucket readers, or mixed-version guarantees.
- Wipe retired ownership buckets and reseed graph authority/indexes as required by the pre-v1 policy; this is release
  cutover, not operational backup/restore capability.
- Remote callers migrate the GraphQL entity result to include `kvRevision`; embedded callers migrate to the one exact
  adapter; stale direct-NATS callers fail rather than silently changing semantics.
- Before the breaking cutover, perform a communicate-only grep census of the eight mutation subjects and retired wire
  fields across semdev, semmachina, semsource, semboids, semdragon, semstreams-ui, semteams, semconnect, semlink, and
  semops. Publish the old-to-new subject/shape migration notice with the release.
- That census makes no sister-repository edits, adds no bridge or compatibility period, and cannot block or redesign the
  accepted foundation. Findings are migration communication, not new SemStreams requirements.

## Adopter seam in the proposed target

The specific adopter is a developer outside this repository writing a component.

| Surface | What they must know | If they do nothing | Discovery rank | What they should have to know |
|---|---|---|---|---|
| Component port | Declare one output `nats-request` port with interface `semstreams.graph.mutation` v1 and family `graph.mutation.>`; call typed operations, not literal subjects. | Flow validation reports a missing/incompatible provider before start; no hidden runtime fallback makes an undeclared client appear connected. | Generated component schema and static flow validation, then typed client construction. | Mutation capability and typed operation only; not handler subjects, transport wildcard arithmetic, or provider internals. |
| Create | Entity ID/envelope and complete birth facts; create is strict. | Duplicate returns typed exists; relationship objects may remain unresolved. | Compile-time typed request, then typed runtime result. | Their intended entity and birth facts only. |
| Reconcile | Complete desired set for named predicates and exact-read revision. | Missing returns not-found; stale revision returns conflict; no silent lost update. | Local contract compile/preflight plus typed result. | Desired state; client supplies/reuses observed revision rather than making them predict storage state. |
| Append | Canonical tuples; batch is partial by subject. | Existing subjects append/dedup; absent subjects return per-subject not-found. | Typed per-subject result. | Evidence to append and which failed subjects, not CAS mechanics. |
| Delete | Exact-read revision; delete is must-exist and fenced. | Missing/conflicting target returns typed result; newer state is not deleted. | Typed result. | Intent to delete current observed entity, not bucket primitives. |
| Lost reply | A timeout can mean committed or not committed. | Client returns `commit_unknown`; it never retries into a false success/conflict claim. | Typed client outcome. | Choose business retry policy after observing current authority; no exactly-once fiction. |
| Relationship | Object may not yet exist. | Source edge persists; dereference reports the real missing target until birth. | Exact/batch/traversal typed missing result. | The relationship fact only; no target-birth prediction, edge mode, or stub identity. |
| Projection client | Local contract names allowed birth/reconcile/append facts. | Invalid/out-of-pattern operation fails before transport; overlaps with another component are not globally rejected. | Boot/preflight or compile-time interface. | Local fact shape and desired operation, not owner IDs, leases, tokens, buckets, or heartbeats. |
| Remote exact read | GraphQL entity operation returns entity and KV revision. | Not-found is typed; no direct-KV workaround is admitted. | GraphQL schema/result. | Entity ID and returned result. |
| Embedded exact read | One narrow adapter, no general graph client. | Missing adapter is a compile-time dependency failure; raw KV is not fallback. | Compile time. | The named read operation only. |
| Operator | Run one graph-ingest process and monitor existing graph readiness. Static flow validation admits one declared provider in that flow only. | A second process elsewhere is unsupported and no election protects it. | Deployment contract and health docs; process-wide/account-wide cardinality cannot be inferred from the local flow graph. | Supported cardinality and existing readiness, not semantic registry internals. |

The remaining adopter debt is explicit: a reconciler must understand “complete desired predicate set,” and an operator must
enforce one graph-ingest process. Both are owner-ruling points; neither is hidden behind documentation as if already
accepted.

## Complexity and deletion budget

Hard design budget for independent review:

- four mutation command subjects, down from eight;
- one mutation protocol interface, one provider family port, and one requester family port per consuming component;
- one exact result and one embedded operation-specific adapter;
- zero new KV buckets, streams, durable consumers, services, status keys, config fields, leader/election primitives,
  pending queues, compatibility paths, or MCP surfaces;
- delete all 4,599 `pkg/ownership` lines and the 866-line OwnershipService cohort;
- delete stub/lease/foreign-mode production paths and the directly coupled test cohorts measured in the inventory;
- retain one local projection schema and existing classified RPC mechanism;
- production code must be net-negative after generated artifacts are excluded. A net-positive implementation returns to
  design review with a line-by-line justification rather than treating the budget as optional.

## TDD and E2E proof intent

Proof is required in this order during implementation:

1. Contract tests fail first for the four operation shapes, exact result, retired subjects/fields, typed port interface,
   canonical `graph.mutation.>` family, and generated schemas.
2. Port/composition tests prove graph-ingest declares the required provider input, requesters declare compatible outputs,
   `BuildPortFromDefinition` preserves flat and typed interface metadata, all four leaves resolve inside the declared
   family, missing/wrong/multiple provider declarations fail static validation, and the port creates no JetStream
   stream/consumer. These tests make no process-wide election claim.
3. Handler unit/integration tests prove:
   - an ingest merge racing an RPC reconcile cannot overwrite the acknowledged reconcile; every existing-key write uses
     CAS, while strict birth uses atomic Create;
   - concurrent create has one winner;
   - two reconciles from one revision have one winner and one revision mismatch;
   - append preserves sibling values, deduplicates exact tuples, and reports per-subject partials;
   - delete cannot erase an entity advanced after the caller's read;
   - every non-create mutation against an absent entity is typed not-found;
   - empty reconcile on an existing entity distinguishes unchanged predicate absence from entity absence;
   - lost replies become `commit_unknown` without automatic ambiguous retry or exactly-once claims.
4. Exact-read tests prove value/revision come from one entry, revision is nonzero, logical Version is ignored, poison remains
   classified, and GraphQL plus embedded adapters agree.
5. Relationship tests prove absent objects do not create KV keys, edges remain visible, dereference reports missing, and a
   later real birth resolves without replay/pending state.
6. Composition/schema tests prove both mains have no ownership service/buckets/tokens, six configs and generated schemas
   contain no lease field, rule preflight remains local, and `GRAPH_STATUS` behavior is unchanged.
7. `go test -race ./...`, strict OpenSpec validation, schema generation with clean diff, and contract tests are green.
8. Because this is BREAKING, relevant E2E tiers must be green before landing:
   - `task e2e:semantic` for ingest → authority → exact query;
   - `task e2e:structural` for retained Graphable-lane hierarchy, atomic container birth/CAS inverse writes, RPC-create
     hierarchy prohibition, and relationship absence/no-stub behavior;
   - `task e2e:lifecycle` for read/CAS/delete migration;
   - `task e2e:agentic` for todos, lesson birth/curation, and tool mutation callers.
   A missing assertion is filed as a coverage gap before the breaking commit.

## Approved implementation slices

These are sequencing constraints translated into the accompanying OpenSpec tasks:

1. Pin target specs/ADR supersession and add failing algebra/exact-read contract tests.
2. Make the port contract real: preserve `nats-request` interface metadata, add graph-ingest's provider input, migrate
   requester outputs/configs to the typed `graph.mutation.>` family, validate exactly one provider per flow, and resolve
   all handler/client leaves from the declared port.
3. Implement exact result at graph-ingest, GraphQL, and the one embedded adapter.
4. Implement the shared Create/CAS storage discipline, four handlers, and typed client outcomes beside failing tests,
   without compatibility handlers or unconditional `ENTITY_STATES` Put paths.
5. Convert local projection Contract/client, built-ins, rule preflight/action names, and generated rule schema.
6. Migrate lifecycle, gated-DAG, agentic-loop/tools, inference/research writers, both mains, hierarchy's Graphable-only
   adapters, and E2E harnesses.
7. Remove automatic stubs/foreign modes and migrate dereference consumers.
8. Delete ownership package/buckets/service/config/wire/metrics/tests/docs and relocate the catalog backstop/graph guard.
9. Regenerate schemas, wipe/reseed test stacks, run race/contract/OpenSpec/E2E gates, then land as one coordinated breaking
   cutover. No slice may merge to main with sister binaries half-migrated.

## Approved owner rulings

Independent review passed and the owner accepted all sixteen rulings:

1. Option B is the target.
2. The four-operation algebra and exact subject dispositions are binding.
3. Nonzero `expected_revision` is mandatory for every reconcile and delete.
4. Delete and every absent non-create mutation target return typed not-found.
5. Append responses are partial per subject and retry selection belongs to the caller.
6. `commit_unknown` carries no exactly-once, request-ID deduplication, or authorship inference claim.
7. Relationship-object absence is valid state, visible when a reader dereferences, hydrates, or traverses.
8. Automatic stubs, restamp, foreign-edge modes, inverse gate, and the unused pending-edge spelling are deleted.
9. The local projection schema remains; every global claim, lease, and owner binding is deleted.
10. One graph-ingest process is the supported topology, with no runtime detection, election, or fencing.
11. Component ports are the canonical mutation API: typed provider input and requester outputs, one
    `graph.mutation.>` family convention, subjects resolved from the declaration, and no JetStream mutation stream.
12. Exactly-one-provider flow validation is a static composition rule only, not runtime leader election or
    account-wide multiple-process detection.
13. The complete package, bucket, service, config, schema, wire, spec, and ADR disposition is binding.
14. The external pre-v1 cutover has no compatibility bridge and includes only the communicate-only downstream wire
    census/notice that performs no sister-repository edits and cannot block the design.
15. Atomic Create plus observed-revision CAS is the invariant for both authority-write lanes, with the keyed pool
    retained only as a local throughput optimization.
16. Opt-in hierarchy remains only on Graphable ingest, with real derived-container Create/CAS writes, no RPC-create side
    effects, no referential stubs, and no new derived-view subsystem.

These rulings bind the mechanical translation according to the handoff. Changing one requires a new owner ruling;
implementation review may correct mechanics but may not redesign the accepted foundation.
