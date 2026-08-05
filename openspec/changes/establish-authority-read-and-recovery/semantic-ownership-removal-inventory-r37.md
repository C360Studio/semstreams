# GS-01 semantic-ownership removal blast-radius inventory

> **INVENTORY ONLY — UNAPPROVED.** This artifact records repository facts at the stated baseline. It contains no
> target state, option, recommendation, capability delta, migration, implementation task, or owner ruling.

## Identity and investigation boundary

- Worktree: `/private/tmp/semstreams-gs00`.
- HEAD: `45746d98fb1c1db4ce0ae9ee431da68cbae4b398` (`docs(graph): restore GS-01 foundation scope`).
- Branch: `codex/gs01-authority-recovery`.
- Baseline dirt at final verification: two untracked files. The first,
  `openspec/changes/establish-authority-read-and-recovery/authority-read-writer-safety-contract-r36.md`, identifies
  itself as an unapproved design draft and authorizes no runtime/spec/task edit (`:1-16`); it is collision evidence, not
  accepted target state. The second, `semantic-ownership-removal-inventory-r37.md`, is a prior inventory copy and was
  excluded from closing search counts to avoid self-reference. Neither untracked file was edited by this inventory pass.
- Owner-supplied investigation boundary: remove semantic `pkg/ownership` from the foundation; keep graph-ingest as the
  physical `ENTITY_STATES` writer; treat NATS request/reply as the intended mutation front; inventory typed not-found,
  caller-owned retry, no automatic stub birth, observable dangling references, and CAS/operation semantics. These are
  hypotheses/boundaries for enumeration, not rulings made by this artifact.
- Current active GS-01 truth remains inventory/design-gated: proposal says revision 36 remains unapproved and carries no
  runtime or spec impact (`proposal.md:1-16,32-55`); tasks leave design, review, owner decision, capability deltas, and
  runtime work incomplete (`tasks.md:45-69`); design says no revision-36 target exists (`design.md:57-66`).

## Mandatory surface inventory

### 1. Same word, different current surfaces

| Current spelling | Repository meaning | Evidence |
|---|---|---|
| `ENTITY_STATES` owner `graph-ingest` | Physical catalog owner and direct KV writer. Catalog owner enforcement is call-site selection/review, not runtime identity verification. | `graph/kvcatalog.go:1-24,67-74`; `processor/graph-ingest/component.go:2464-2534,2700-2707,2872,2914,2969,3007,3167,3363,3527`; `mutations.go:1042` |
| `pkg/ownership` | Semantic predicate/foreign-edge claim registry, owner presence, lease token, overlap arbitration, revival/quiesce behavior. | `pkg/ownership/doc.go`; complete file census below |
| Projection owner/contract | A component's declared entity pattern, message type, predicate groups, birth predicates, indexing profile, and mutation verbs. Current public types embed semantic ownership modes and binding. | `pkg/projection/contract.go:12-75,188-300`; `mutation_types.go:16-177` |
| Lifecycle workflow owner | Workflow registration, predicate projection, CAS transitions, owner-token stamping, heartbeat and quiesce checks. | `pkg/lifecycle/manager.go:19-74,223-308,311-410,568-735,867-1112` |
| Bucket/catalog owner | Retention/write-policy metadata for framework buckets. It is broader than semantic predicate ownership. | `graph/kvcatalog.go:1-24,46-68`; `openspec/specs/graph-retention/spec.md:205-224` |

Removing semantic ownership therefore reaches more than a package deletion, but it does not by itself erase physical
graph-ingest write paths or catalog ownership vocabulary.

### 2. Mutation wire, handlers, callers, and present semantics

The public NATS request/reply contract is explicitly consumed outside this repository (`graph/mutation_requests.go:1-10`).
Graph-ingest registers eight plain request subscribers (`processor/graph-ingest/mutations.go:22-135`); the responder uses
Core NATS `Subscribe`, not a queue subscription (`natsclient/request.go:342-405`). Multiple graph-ingest processes can
therefore receive, execute, and race replies for the same request. The active accepted inventory records the same fact
at `scope-audit-r36.md:264-270`.

| Subject / request | Current operation semantics | Absence / concurrency / result |
|---|---|---|
| `graph.mutation.entity.create` / `CreateEntityRequest` | Strict entity create. Framework may add hierarchy/referential facts; response reads stored state. | KV `Create`; duplicate is typed `entity_already_exists`; committed read-back failure is degraded success. `mutation_requests.go:17-29`; `mutations.go:581-632` |
| `...entity.create_with_triples` / `CreateEntityWithTriplesRequest` | Atomic birth; request `Triples` replaces embedded triples when non-empty; includes indexing profile and optional `OwnerToken`. | Strict create except an existing framework stub may be CAS-merged/restamped as a real birth. `mutation_requests.go:38-64`; `mutations.go:634-800` |
| `...entity.update` / `UpdateEntityRequest` | Bare must-exist entity replacement/merge lane. | Reads current revision and writes conditionally; missing is typed not-found. Evidence: `mutation_requests.go:24-29`; `mutations.go:805-910`; `component.go:2887-2975` |
| `...entity.update_with_triples` / `UpdateEntityWithTriplesRequest` | `RemoveTriples` deletes predicates first; `AddTriples` is **replace by subject+predicate**, so a multi-valued predicate requires the full desired set. | Missing is typed not-found. `ExpectedRevision>0` is exact single-pass CAS; zero performs server-side read/reapply/CAS retry. Optional `OwnerToken` is checked on this lane. `mutation_requests.go:66-127`; `mutations.go:911-1265` |
| `...entity.delete` / `DeleteEntityRequest` | Whole-entity delete. | Present handler is idempotent: absent returns success/`Deleted:false`; delete carries no expected revision and is not a conditional delete. `mutation_requests.go:31-36`; `mutations.go:69-72,1267-1325`; `pkg/lifecycle/graph_emit.go:212-240` |
| `...triple.add` / `AddTripleRequest` | Append one exact six-field triple, deduplicating an identical tuple. | Must-exist; missing is typed `entity_not_found`; CAS retry occurs inside graph-ingest; response reports the exact committing KV revision or live revision for a no-op. `mutation_requests.go:129-134`; `mutations.go:335-430` |
| `...triple.add_batch` / `AddTriplesBatchRequest` | Groups by subject; append/deduplicate, one CAS per entity. | Atomic only per subject, not across subjects; missing subjects appear in partial `FailedSubjects`; a one-subject receipt can carry exact revision. `mutation_requests.go:136-151`; `mutations.go:432-529` |
| `...triple.remove` / `RemoveTripleRequest` | Removes all matching subject+predicate facts. | Missing entity or predicate is presently an idempotent no-op success, not typed not-found; response reports `Removed:false`. `mutation_requests.go:153-159`; `mutations.go:531-560` |

Current direct/typed production caller families found by request type or subject search:

- `pkg/projection`: create, replace, append (`pkg/projection/mutation_client.go:23-25,531,711,794`).
- lifecycle: create/update/delete (`pkg/lifecycle/manager.go:685,724,963,1094,1233`; `graph_emit.go:80-88`).
- rule add/remove and contract-bound replace (`processor/rule/triple_mutator.go:17-124`; projection derivation/targets below).
- agentic-loop add/batch/create (`processor/agentic-loop/graph_writer.go:25-27,97,136,183`).
- agentic tools add/batch/create and lesson birth (`processor/agentic-tools/decide.go:668-775`;
  `processor/agentic-tools/emit_lesson.go:189-212`).
- agent-run milestone add (`agentic/agentrun/nats_reader.go:7-56`).
- research-graph wrapper add/batch/create (`processor/research-graph-llmwrap/triplepub.go:52-162`).
- inference add (`graph/inference/applier.go:209-274`).
- gated-DAG unconditional replace-by-predicate claim/unclaim. Both requests omit `ExpectedRevision` (zero), so
  graph-ingest performs its server-side read/reapply/CAS retry; the client also retries the convergent request on
  transport failure (`processor/gated-dag/claim.go:14-109`; `processor/graph-ingest/mutations.go:1011-1042`).
- graph clustering optionally auto-applies high-confidence virtual relationships through inference's `triple.add`
  adapter; anomaly records themselves persist directly in graph-clustering-owned `ANOMALY_INDEX`
  (`processor/graph-clustering/anomaly.go:164-182,220-225`; `graph/inference/applier.go:226-274`).
- graph gateway declares `graph.mutation.*` ports, while the handlers remain graph-ingest-owned
  (`gateway/graph-gateway/component.go:145,192`).
- tests/e2e also construct raw create/update requests; they are part of migration verification, not production consumers.

No production caller of the bare entity `create` or bare entity `update` request type was found outside graph-ingest in
the closing request-type search; shipped callers favor create/update-with-triples. Delete's production caller is
lifecycle `Despawn`.

### 3. Complete `pkg/ownership` inventory

The directory contains 28 files: 14 production and 14 test files.

| Production file | Surface owned today |
|---|---|
| `bootstrap.go` | `EnsureBuckets`, eager catalog acquisition, inverse resolver wiring; explicitly does not create `PENDING_EDGES` (`:12-65`). |
| `buckets.go` | `OWNER_CLAIMS`, `OWNER_PRESENCE`, declared-only `PENDING_EDGES`, registry/presence key spellings (`:5-35`). |
| `claim.go` | `WriteMode`, `EdgeMode`, `OwnerClaim`, `ForeignEdgeClaim`, `CoordinationWaiver`, validation (`:21-211`). |
| `claim_reader.go` | `ClaimReader`, unclaimed-edge classification, edge-mode lookup, `OwnerOf` (`:25-156`). |
| `doc.go` | Package/runtime contract and incomplete-increment declarations. |
| `epoch.go` | Registry epoch representation and serialization/lookup helpers. |
| `errors.go` | `ErrInvalidClaim`, `ErrOwnershipOverlap`, `OverlapError` (`:12-46`). |
| `glob.go` | owner ID and entity-pattern validation (`ValidateOwnerID` at `:12`). |
| `heartbeat.go` | presence TTL/interval, `Heartbeater`, enrollment and run loop (`:41-110`). |
| `inverse_gate.go` | `InverseResolver`, `CheckInverseGate` (`:10-47`). |
| `overlap.go` | owner/foreign-edge same-cell and cross-type overlap detection. |
| `owner_token.go` | opaque `OwnerToken`, expected token wire form, registry token minting (`:25-83`). |
| `registry.go` | registry identity/bind guard, registration validation and epoch CAS, heartbeat, resign, owner and edge queries (`:22-550`). |
| `revival.go` | epoch watcher that detects a rival incarnation and quiesces local owners; metric hook (`WatchRevival` at `:77`). |

Test files cover bootstrap integration, catalog pins, claim/glob/owner-ID/token/predicate authoring, inverse gate, overlap,
claim-reader integration, registry integration, and revival unit/integration behavior. Removing production semantics also
retires or rewrites all 14 test files; they are not independent runtime adopters.

Durable/config/service/metrics/wire attachments:

- `OWNER_CLAIMS`: cataloged, owner-only, durable epoch/audit history. `OWNER_PRESENCE`: cataloged bounded-TTL heartbeat
  store. `PENDING_EDGES`: constant only and deliberately not cataloged or created because no consumer exists
  (`pkg/ownership/buckets.go:5-24`; `bootstrap.go:24-38`; `graph/kvcatalog.go:38-44,102-103`).
- `service.OwnershipService` runs static heartbeats plus revival/quiesce watching and owns shutdown joining
  (`service/ownership_service.go:20-114`). `WireOwnershipSubstrate` and `WireOwnership` eagerly construct the registry,
  attach lifecycle, bind built-ins, and require service registration (`:117-252`).
- lifecycle `AttachOwnership`, `WaitOwnership`, owner-token minting and quiesce gate are public methods/behavior
  (`pkg/lifecycle/manager.go:311-410`).
- graph-ingest self-wires a `ClaimReader` at start (`processor/graph-ingest/component.go:594-642,1296-1301`), exposes
  `enforce_owner_lease` default false (`:452-459`), and reports `owner_lease_mismatch_total`,
  `foreign_edge_unclaimed_total`, and `foreign_edge_dropped_total` (`:230-284`).
- graph request wire carries `owner_token` only on create/update-with-triples (`graph/mutation_requests.go:53-61,116-124`),
  and response taxonomy includes `owner_lease_stale` (`graph/mutation_responses.go:122-127`). Empty token, missing reader,
  unclaimed cell, and lookup failure remain fail-open; a mismatch rejects only when enforcement is enabled
  (`processor/graph-ingest/component.go:2091-2209`).

### 4. Direct and indirect semantic-ownership consumers

Production imports/references outside `pkg/ownership` close on these families:

| Family | Coupling |
|---|---|
| `pkg/projection` | Public contract modes/types, derivation, bind/heartbeat, registry/heartbeater config, token transport, error taxonomy (`contract.go:9,32,41,188-300`; `mutation_types.go:14-22`; `mutation_client.go:18-65,126-152,460-469`). |
| lifecycle | Registry/heartbeater fields, workflow claim derivation, registration error translation, token and quiesce behavior (`manager.go:19,62-74,223-410`; `ownership.go:13-94`). |
| graph-ingest | ClaimReader, foreign-edge modes, lease token comparison/enforcement and metrics (`component.go:27,594-642,1296,1936-2010,2091-2209`). |
| service/composition | Ownership service and substrate, static built-in/rule-pack binding (`ownership_service.go`; `rule_pack_bind.go:9-161`). |
| rules | Contract derivation uses `ModeReplaceOwned`; runtime target index retains modes (`processor/rule/projection_derivation.go:8,217-285`; `projection_targets.go:8,28,113`). |
| built-ins | Static projection contracts declare replace-owned groups (`internal/builtinprojection/contracts.go:7,44,73`). |
| E2E | graph roundtrip explicitly bootstraps registry/heartbeater and binds a replace-owned contract (`test/e2e/scenarios/graph_roundtrip.go:17,222-236`). |
| catalog/vocabulary | Catalog pins presence TTL without importing ownership; vocabulary provides inverse resolver (`graph/kvcatalog.go:38-44`; `vocabulary/registry.go:518-519`). |

Test-only indirect consumers additionally exist in projection, lifecycle, graph-ingest, rule, service, built-ins,
graph-index owner filtering, agentic lesson promotion, and storage-report tests (26 files returned by the test import
search). These validate behaviors above; they do not establish a separate contract owner.

#### Concrete composition, wire, configuration, and generated-schema census

This table separates the semantic-ownership coupling from adjacent behavior that shares the same composition object.
“Affected” and “mixed/preserve” are blast-radius classifications only, not target-state decisions.

| Concrete surface | Current wiring / do-nothing behavior | Inventory category |
|---|---|---|
| `cmd/semstreams/main.go` | Creates lifecycle Manager; obtains shutdown context; calls `WireOwnership` with both built-in contracts; injects returned `MutationClient` into built-in tools; registers `OwnershipService`; binds all rule packs before start (`:162-170,183-197,219-222,260-274`). | Affected: registry/service/rule-pack binding. Mixed/preserve: lifecycle Manager, tool capabilities, and process shutdown still have non-ownership responsibilities. |
| `cmd/e2e-semstreams/main.go` | Same substrate/static client/service/rule-pack wiring (`:143-150,167-184,200-203,237-247`); additionally constructs `LessonCurator` with that client as both `OwnedReplacer` and `AuthoritativeReader`, then injects it into the E2E promotion request handler (`:152-165`). | Affected: semantic bind/token dependency. Mixed/preserve: curator validation/read/replace behavior and E2E handler are distinct from claim registration. |
| `service.WireOwnershipSubstrate` | Runs catalog-retention backstop, creates ownership buckets/Registry with vocabulary inverse resolver, attaches lifecycle ownership, returns shared heartbeater (`service/ownership_service.go:117-205`). | Mixed: `EnsureBuckets`, Registry, attachment and heartbeater affected; `AssertOwnedBucketsClean` is catalog-retention behavior, not semantic ownership. |
| `service.WireOwnership` | Calls the substrate and binds one built-in `projection.MutationClient` under owner `agentic-loop-graph-writer` with loop-todo and lesson-lifecycle contracts (`ownership_service.go:208-240`; `internal/builtinprojection/contracts.go:12-81`). | Mixed: binding/owner/token affected; typed create/replace/append/read client behavior is separately inventoried in section 5. |
| `service.OwnershipService` | Runs static owner heartbeat and revival/quiesce watcher; joins them at stop (`ownership_service.go:20-114`). | Affected semantic liveness service. |
| `service.WireOwnershipShutdown` | Cancels the lifecycle ownership heartbeat context then calls `Manager.WaitOwnership`, which also joins the independent graph-state guard (`ownership_service.go:242-271`; `pkg/lifecycle/manager.go:350-366`). Both mains defer it. | Mixed: heartbeat join affected; graph-state guard cancellation/join is independent and shares the method. |
| `service.BindRulePackContracts` | Preflights all enabled packs; derives `rule-pack.<packID>` contracts; binds one mutation client per contract-bearing pack; injects only `OwnedReplacer`; registry/heartbeater are required according to group posture (`service/rule_pack_bind.go:49-172`). | Mixed: registry/heartbeat/owner binding affected; pack preflight, frozen contract/target validation, and replacer injection remain mutation composition behavior. |
| Built-in tool injection | Both mains place the static `MutationClient` in `executors.ToolDependencies`; built-ins require it for `write_todos` (`cmd/semstreams/main.go:183-197`; `cmd/e2e-semstreams/main.go:167-184`; `processor/agentic-tools/executors/register.go:45-47,194-197`). | Mixed: client construction affected; the tool's mutation capability remains a concrete adopter. |
| `LessonCurator` injection | Only the E2E main constructs/injects the curator in a request handler. Framework main supplies the mutation client to tools but has no `NewLessonCurator` call (`cmd/e2e-semstreams/main.go:152-165`; production search closure). | Mixed: semantic binding is transitive; curator's evidence reads and lifecycle-group replacement are direct consumers. |
| Graph mutation wire | `owner_token` exists only on create-with-triples/update-with-triples; stale token has a classified code (`graph/mutation_requests.go:53-61,116-124`; `mutation_responses.go:122-128`). | Affected wire fields/taxonomy; the eight subjects and other request fields are separate. |
| Graph-ingest generated schema | `enforce_owner_lease` is an advanced boolean with schema default `false` (`schemas/graph-ingest.v1.json:19-23`), matching the Go field tag (`processor/graph-ingest/component.go:452-459`). | Affected config/schema. **Schema omission posture:** false/observe-only. |
| Six shipped graph-ingest configs | `configs/e2e-structural.json:288`, `protocol-flow.json:299`, `semantic-8b.json:581`, `semantic-frontier.json:590`, `semantic.json:541`, and `statistical.json:504` each explicitly set `enforce_owner_lease: true`. | Affected config. **Shipped do-nothing posture:** loading any of these files retains reject enforcement; the schema default does not override an explicit true. |
| Rule generated schema | Requires `pack_id`, described as rule-pack projection owner and graph-event producer identity; exposes `projection_contracts`, group modes, foreign edge mode/predicate/target pattern, birth predicates, message type/profile, and action selectors `projection_contract`/`projection_group` (`schemas/rule-processor.v1.json:1225-1315,1343-1346,125-132`). | Mixed: owner/ownership modes affected; PackID graph-event identity and contract/selector mutation schema are independent collisions. |
| Lesson reference rule pack | `pack_id=lesson-lifecycle`; watches `ENTITY_STATES`; declares `agentic.lesson-record` with one replace-owned status/superseded/retired group; example rule performs `replace_owned` (`configs/rules/lessons/lesson-lifecycle-rulepack.json:1-60`). | Affected semantic mode/bind; preserve-category facts include watch, predicates, rule condition and replace intent. |
| Lesson rule-pack README | Tells adopters the group mirrors both mains' boot owner registry, distinguishes immutable birth predicates, directs promotion through `LessonCurator`, and documents rule-pack hard-fail wiring (`configs/rules/lessons/README.md:1-47,49-94`). | Affected adopter documentation plus separable curation/mutation behavior. |

### 5. Projection contract: separable mutation schema versus ownership substrate

Current `projection.Contract` combines two classes:

- Mutation/projection schema that remains meaningful independently: `Name`, `EntityPattern`, `MessageType`, named
  predicate groups and exact predicates, `BirthPredicates`, `ForeignEdges`, `IndexingProfile`, validation, create
  authorization, full desired-set replacement, append evidence, canonical read-back, `MutationReceipt`, and typed
  mutation errors (`pkg/projection/contract.go:12-187`; `mutation_types.go:25-177`;
  `mutation_client.go:507-838,956-984`).
- Semantic-ownership coupling: `PredicateGroup.Mode ownership.WriteMode`, `ForeignEdge.Mode ownership.EdgeMode`,
  `Derive`, `Bind`, `BindAndHeartbeat`, `Config.Registry`, `Config.Heartbeater`, `Config.Owner`, the retained owner token,
  ownership-overlap/already-bound error translation, and owner-token attachment
  (`contract.go:9,32,41,188-300`; `mutation_types.go:14-22`; `mutation_client.go:18-65,126-152,460-469`).

Current spec text binds both classes together: one-owner registration/heartbeat/token posture
(`openspec/specs/projection-mutation-client/spec.md:21-158`), narrow mutation capabilities (`:160-171`), birth-only
authorization with no graph-enforced immutability (`:190-258`), and full selected-group replacement (`:296-337`). The
separability statement above is an inventory classification; no replacement type or API is selected here.

### 6. Foreign edges, inverse gate, pending state, and automatic stubs

- The stored `ForeignEdgeClaim` fields are exactly `Owner`, exact `Predicate`, `Mode`, optional `Producer`, and optional
  `TargetPattern`. It stores neither an entity pattern nor an inverse predicate (`pkg/ownership/claim.go:136-184`). A
  `projection.Contract` derives `Producer` from its `MessageType` and copies the configured target pattern
  (`pkg/projection/contract.go:36-59,188-210`).
- Runtime classification lookup is keyed only by `(producer message type, exact predicate)`: an exact `Producer` match
  wins, then a `Producer==""` any-producer claim is the fallback; owners are scanned deterministically. `TargetPattern`
  does not participate in routing lookup (`pkg/ownership/epoch.go:110-140`; `claim_reader.go:49-109`).
- `TargetPattern` participates separately in OwnerClaim×ForeignEdgeClaim cross-type overlap: empty becomes conservative
  match-any, it intersects the OwnerClaim's `Pattern`, and the exact predicate must also match. ForeignEdgeClaim×
  ForeignEdgeClaim is allowed (`pkg/ownership/overlap.go:8-25,71-105`).
- The vocabulary inverse gate is another separate registration-time check, not stored claim identity. Only
  Conditional/Backfill modes require `vocabulary.GetInversePredicate` through the injected resolver; Strict and
  NoBirthStub do not (`pkg/ownership/inverse_gate.go:5-37`; `registry.go:459-465`; `bootstrap.go:40-64`;
  `vocabulary/registry.go:490-522`).
- A present target always receives the edge. With no claim reader, lookup error, unclaimed cell, or unknown mode, routing
  fails open to the legacy append path (`processor/graph-ingest/component.go:1924-1970,2004-2010`). Because append is
  must-exist, an absent target then produces a warned partial failure rather than a durable pending item.
- `EdgeNoBirthStub` creates an envelope-bearing stub and then appends; `EdgeStrict` drops with metric/WARN;
  `EdgeConditional`/`EdgeBackfill` are labelled deferred but no pending store exists, so they metric/WARN and attempt the
  must-exist append (`component.go:1972-2003`).
- Independently of `ForeignEdgeClaim`, create/fact-arrival relationship handling walks relationship targets and
  auto-creates best-effort stubs (`component.go:2739-2793`). The stub is a public graph shape with message type and marker,
  referenced-by, and owner predicates (`graph/stub.go:5-49`; `component.go:2795-2858`). It is atomically create-if-absent
  so it cannot overwrite a concurrent real birth (`component.go:2865-2877`).
- A later real `create_with_triples` collision can restamp/merge the stub rather than return conflict
  (`processor/graph-ingest/mutations.go:634-680`).
- Readers rely on stub identity: gated-DAG excludes stubs from dispatch (`processor/gated-dag/reader.go:137-172`),
  lesson promotion rejects stubs (`processor/agentic-tools/lesson_promotion.go:77-97`), and lifecycle exposes reference
  triples as `ReferenceStub` records without reading a target (`pkg/lifecycle/manager_query.go:680-724`;
  `gateway/lifecycle-gateway/component.go:152`).

Thus `ForeignEdgeClaim` removal and automatic-stub removal are overlapping but not identical blast radii. The
relationship walker can birth stubs without claim lookup; claim routing also controls strict/drop/deferred behavior.

### 7. Lifecycle, rule, agentic, and other downstream seams

- Lifecycle directly opens/reads `ENTITY_STATES` and returns the same-entry revision through `GetWithRevision`
  (`pkg/lifecycle/manager.go:413-456,492-523`). It creates, transitions, operator-updates, and deletes through the mutation
  front, using expected revisions on update lanes (`:568-735,867-1112,1224-1284`). Semantic ownership additionally
  supplies registration, heartbeat, quiesce, and token behavior (`:223-410`). Those are distinct dependencies.
- Lifecycle NATS create retries no-responder transport and, after an ambiguous/lost reply, current higher-level logic
  re-reads to distinguish its committed birth from a conflicting birth (`graph_emit.go:162-203`;
  `manager.go:568-735`; tests at `manager_test.go:1004-1160`). Update propagates `revision_mismatch` for an outer reread
  loop and maps typed absence to `ErrEntityNotFound` (`graph_emit.go:108-159`). Delete is currently retried as idempotent
  (`:212-240`). Caller-owned retry after typed absence therefore depends on a same-entry value+revision read if the
  subsequent operation is conditional.
- Rule packs bind a public projection client fail-closed and derive replace-owned targets from rule definitions; raw
  add/remove remains separately available (`openspec/specs/rule-projection-mutations/spec.md:21-42,158-207,247-319`;
  `processor/rule/projection_derivation.go:217-285`; `triple_mutator.go:17-124`).
- Agentic, inference, research-graph, and anomaly writers predominantly use append or strict birth and carry no owner
  token. They depend on graph-ingest must-exist/typed error and request retry behavior rather than claim registration;
  caller list is in section 2.
- Gated-DAG is **not** a CAS caller today. Claim and Unclaim omit `ExpectedRevision`, so it is zero/unconditional
  last-write-wins replace-by-predicate; graph-ingest applies the delta inside server-side `UpdateWithRetry`, and the
  client retries the convergent request on transport failure. OwnerToken is also empty
  (`processor/gated-dag/claim.go:14-109`; `processor/graph-ingest/mutations.go:1011-1048`). The production
  `ExpectedRevision:` assignment search closes on lifecycle only: create/update path at `manager.go:732`, transition at
  `:972`, and operator update at `:1103`. Issue #689 is the deferred CAS/request-scoped outcome work, not present gated-DAG
  behavior (`scope-audit-r36.md:193-203`; `projection-mutation-client/spec.md:571`).

### 8. Exact-read dependency for caller-owned retry

Current read seams are not equivalent:

| Reader | Value | Same-entry revision | Relevance |
|---|---|---|---|
| graph-ingest exact RPC `graph.ingest.query.entity` | canonical `EntityState` | Handler obtains entry revision for validation but response omits it. | Admitted mutation-adjacent exact read cannot feed public `ExpectedRevision`. `processor/graph-ingest/query.go:60-105`; `scope-audit-r36.md:127,173-176` |
| projection `ReadAuthoritative` | validated `*EntityState` | omitted | Used for create/replace/append ambiguity verification, not revision-fenced retry. `pkg/projection/mutation_client.go:956-984`; `scope-audit-r36.md:173-176` |
| lifecycle `GetWithRevision` | projected participant | yes | Private workflow-shaped direct-KV read; does not provide the general raw admitted operation. `pkg/lifecycle/manager.go:492-523` |
| agentic `query_entity` | content plus metadata | yes | Direct-KV model-tool exception, not the admitted remote/embedded graph contract. `processor/agentic-tools/executors/graph_query.go:151-221`; `scope-audit-r36.md:114` |
| graph/query, GraphQL, fusion, configurable HTTP | value/projection varies | omitted | Current external exact callers cannot perform a read-modify-CAS loop through one admitted contract. `scope-audit-r36.md:179-184` |

The exact-read issue is independently live as #851; logical `EntityState.Version` cannot substitute for KV revision, as
Issue #892 demonstrates (`scope-audit-r36.md:193-203`). This artifact records the dependency only; it does not select a public
front door or response type.

### 9. Same-class collision inventories

#### Mutation/authority collision table

| Required class | Existing occupant/collision |
|---|---|
| Semantic class | Strict birth, stub-restamping birth, must-exist replace, predicate-set replacement, evidence append, predicate removal, whole delete, and read-back verification are separate operations; “mutation” is not one upsert. |
| Owners | Graph-ingest owns physical state and handlers; projection/lifecycle/rule/gated-DAG/agentic/inference/research callers own operation intent and retry decisions. |
| Catalog | `ENTITY_STATES` is authoritative, owner-only, History 1; catalog identity does not authenticate a request caller. `graph/kvcatalog.go:67-74` |
| Status | `GRAPH_STATUS` is already graph readiness territory; mutation receipts separately carry commit/degraded/revision evidence. `kvcatalog.go:76-81`; `mutation_responses.go:13-56` |
| Lifecycle | Lifecycle has its own direct read+revision, CAS loops, lost-reply recovery, and idempotent despawn assumptions. |
| Ownership | Predicate claims/token enforcement overlap only create/update-with-triples; add/remove/bare update/delete remain outside token fields. |
| Readers | Admitted exact RPC/projection omit revision; lifecycle and one model tool expose it through private/direct paths. |
| Writers | Eight plain responders can execute concurrently across processes; per-entity CAS protects individual writes but does not make every higher-level operation retry-safe. |
| Recovery | Poison repair/read-back/retry exists; no SemStreams checkpoint, backup, restore, or orchestration is authorized. |

#### Semantic-ownership removal collision table

| Required class | Existing occupant/collision |
|---|---|
| Semantic class | Predicate author exclusivity, foreign-subject edge legitimacy, process incarnation liveness, and catalog write ownership currently share “ownership” language but are different facts. |
| Owners | Lifecycle workflows, built-in static projections, rule packs, and the E2E graph-roundtrip owner register claims. Graph-ingest is a claim reader/enforcer, not the owner of those predicates. |
| Catalog | Removing `OWNER_CLAIMS`/`OWNER_PRESENCE` affects catalog descriptors and rule generic-KV guards; `PENDING_EDGES` is not cataloged/live. |
| Status | Presence heartbeat and revival/quiesce are liveness state; they are not `GRAPH_STATUS` readiness. |
| Lifecycle | Manager attachment/heartbeat/token/quiesce is semantic-ownership coupling; direct state+revision/CAS projection is separate. |
| Ownership | Complete package, service, config, metrics, wire fields, specs, tests, and ADR-056 are enumerated above. |
| Readers | ClaimReader feeds foreign-edge routing and lease enforcement; stub consumers read graph stub identity independently. |
| Writers | Projection and lifecycle stamp tokens; most append/birth callers do not. Default enforcement is off and several paths fail open. |
| Recovery | Registry epoch compaction and revival quiesce recover semantic-owner liveness; they are not operational graph backup/restore. |

### 10. Specs, ADRs, active change, issues, and task collisions

The exhaustive current-spec search found 296 matching lines in 15 of 33 current specs. The census distinguishes direct
semantic-ownership dependencies from terminology collisions:

| Current spec | Collision/dependency |
|---|---|
| `projection-mutation-client` | Direct: Registry identity, owner presence/heartbeat, tokens, stale-token errors, foreign-edge postures, rollout enforcement; also the separable create/replace/append/read and ambiguity contracts (`:7-171,190-449,505-576`). |
| `rule-projection-mutations` | Direct: `rule-pack.<packID>` binding, claim/heartbeat/token/overlap behavior, replace-owned derivation and built-in lesson collision; also separable target/receipt/preflight behavior (`:21-43,116-207,247-362,488-536`). |
| `lifecycle` | Direct: registration records but ownership rejects cross-workflow overlap; owner token participates in writes; lease cannot enforce out-of-pattern birth; ownership-quiesce/superseded-incarnation must reach operator routes intact (`:15-46,141-146,239-263,309-317`). |
| `graph-ingest` | Stub/foreign-edge collision: a profile-less referential stub's first real arrival may set indexing profile; internal relationship-target/foreign writes remain enumerated authoritative lanes (`:67-87,118,301,486,710-725`). The current spec has no literal `restamp` requirement for the runtime `create_with_triples` stub-collision merge at `processor/graph-ingest/mutations.go:634-680`. It does not itself specify Registry binding. |
| `graph-events` | Mixed/direct: `PackID` is rule-trigger lineage and the source of `rule-pack.<PackID>` owner identity; it is required even with graph integration disabled, duplicates fail before projection binding, and schema bounds are normative (`:14,57-80,133-160`). PackID event identity remains a distinct use if semantic claims disappear. |
| `graph-index` | Terminology collision: “semantic row owner,” owner key/filter, source owner, and reconciliation capability describe derived-index row provenance/retraction, not `pkg/ownership` claims. It explicitly assigns owner-lease enforcement elsewhere (`:22-37,91,150-182,208-237,444-499`). |
| `graph-retention` | Catalog collision/direct bucket names: owner-only acquisition/retention, `OWNER_CLAIMS`, and bounded-TTL `OWNER_PRESENCE`; catalog owner is call-site enforced, not request identity (`:24-46,150-224,247-271`). |
| `graph-state-contract` | Terminology collision: projection owner poison scope and generic graph-bucket ownership; generic KV writers are directed to the mutation API (`:19-56,152-181`). |
| `graph-view-subscription` | Terminology collision: trusted owner decode and projection-view lifecycle/ownership, not claim Registry (`:21,171,221` and requirement body). |
| `predicate-contract` | Mixed: registry-derived per-predicate owner map and ordinary graph ownership are referenced, while namespace/alias and endpoint authentication are explicitly separate (`:42-101,211,265`). |
| `entity-id-contract` | Cross-cutting load/validation references to ownership configuration; explicitly makes no reclamation/ownership decision (`:185,242,265,354`). |
| `framework-composition` | Terminology collision: package/product ownership basis and service quiescence, not semantic claims (`:6-11,124-158`). |
| `nats-kv-keys` | Terminology collision: migration-ledger semantic owner and fixed-position owner reconciliation (`:191-211`). |
| `storage-observability` | Terminology collision: logical KV owner attribution derives from catalog; it explicitly has no general stream/ObjectStore owner Registry (`:9,52-101,254-303`). |
| `stream-provisioning` | Terminology collision: resource provisioner/declared stream owner and contested configuration ownership, not predicate claims (`:39-81,128-187,216-417`). |

- ADR-055 supplies strict create/must-exist graph mutation intent; ADR-056 is the semantic ownership, foreign-edge,
  pending-edge, and stub foundation; ADR-058 embeds ownership infrastructure into boot/service lifecycle; ADR-060 defines
  classified RPC errors; ADR-068/073 preserve graph retention; ADR-090 says authority is current state and separates
  physical authority from materialized views. All collide with some portion of a removal, but only ADR-056/058 directly
  establish the semantic substrate.
- Active revision-36 design truth assigns GS-02 mutation outcomes/write seams and keeps lifecycle, view, suffix, and
  front-door work in later increments (`design.md:38-55`). Its untracked draft proposes extending `pkg/ownership` for
  graph-ingest writer liveness (`authority-read-writer-safety-contract-r36.md:35-45` and later design sections), which
  conflicts with the owner-supplied removal/no-leader investigation boundary and has no approval.
- Accepted revision-36 inventory/review is content-addressed at SHA-256
  `eca90d2eaafec75f02fa3a0ae243a95e8614daaa9dde385a1247fdd345a3ef02`, 440 lines/63,402 bytes, and has
  `INVENTORY PASS` (`scope-audit-r36-review.md:1-18`). It records open issues #681 (lifecycle H1 history), #843 (masked
  lifecycle E2E failure), #689 (deferred gated-DAG CAS/outcome work), #851 (authority value+revision), and #892 (logical version drift), with
  their consumers and increment boundaries (`scope-audit-r36.md:193-219`).
- Active tasks prohibit runtime/spec work until design review and explicit owner acceptance (`tasks.md:45-69`). This
  inventory adds no task truth.

### 11. Explicit non-DR, cluster, leader, and CQRS boundary

- The active proposal rules out SemStreams checkpoint, backup, restore, attestation, recovery gates/orchestration, a
  single-node restriction, and a NATS CLI requirement (`proposal.md:32-43`).
- Clustered NATS remains supported; edge/offline backups are operator responsibility (`design.md:7-23`).
- No leader-election or CQRS target is authorized by this inventory. Current facts are plain fan-out request responders,
  process-local keyed ordering, KV CAS, and semantic-owner liveness; none is relabelled as leader election or CQRS.

### 12. Closing searches at `45746d98`

All commands ran with working directory `/private/tmp/semstreams-gs00`.

```text
rg -n 'pkg/ownership|ownership\.|OWNER_CLAIMS|OWNER_PRESENCE|PENDING_EDGES|owner_token|enforce_owner_lease|owner_lease' ...
=> 678 matching lines in 123 files

rg -n 'graph\.mutation\.|SubjectTriple(Add|AddBatch|Remove)|SubjectEntity(Create|CreateWithTriples|Update|UpdateWithTriples|Delete)' ...
=> 289 matching lines in 100 files

rg -n 'StubMessageType|IsStub\(|stub_owner|ForeignEdgeClaim|Edge(NoBirthStub|Strict|Conditional|Backfill)|PENDING_EDGES' ...
=> 346 matching lines in 54 files

rg -n 'ReadAuthoritative|getEntity\(|getWithRevision|graph\.ingest\.query\.entity|query_entity|KVRevision|ExpectedRevision' ...
=> 536 matching lines in 133 files

rg -n 'ExpectedRevision\s*:' --glob '*.go' --glob '!**/*_test.go' .
=> exactly 3 production assignments, all in pkg/lifecycle/manager.go:732,972,1103

find openspec/specs -mindepth 2 -maxdepth 2 -name spec.md
=> 33 current specs

rg -n -i '\bowners?\b|\bownership\b|owner[_ -]?(token|lease|claim)|replace-owned|foreign.?edge|quiesc|stub|restamp|PackID|pack_id|OWNER_CLAIMS|OWNER_PRESENCE' openspec/specs --glob 'spec.md'
=> 296 matching lines in 15 current specs; all 15 classified in section 10
```

Scope checks also used complete production import, test import, mutation request type/subject, direct `entityBucket`
write, stub consumer, active-spec, ADR, active-change, and issue-reference searches. Empty live category confirmed:
`PENDING_EDGES` has no catalog descriptor, bucket construction, reader, writer, drain, or recovery loop; only constants,
comments, ADR/spec/design text, and tests describing the not-yet-landed increment exist.

## Mandatory adopter seam inventory

The outward-facing mutation request structs explicitly name external consumers (`graph/mutation_requests.go:1-5`), and
`pkg/projection`/lifecycle/rule contracts are framework APIs. The specific adopter is a developer outside this repository
writing a SemStreams component and never opening graph-ingest, ownership, or lifecycle implementation files.

| Reached surface | What must the adopter know today? | If they do nothing | Where they find out today | What should they have to know |
|---|---|---|---|---|
| Entity create | Strict create versus stub collision/restamp; canonical triple source; degraded committed response; duplicate classification. | A retry after ambiguous commit can look like conflict; a stub ID may become real instead of conflicting. | Request comments, projection spec/client, classified error. | One birth operation and an explicit committed/not-committed/unknown result; no KV race prediction. |
| Entity update/replace | AddTriples means replace-by-predicate/full desired set; missing is typed; zero/nonzero expected revision differs; token may be enforced. | Partial multi-value sends erase siblings; unconditional updates can overwrite interleavings; stale tokens may reject only in configured deployments. | Request comments, projection contract/spec, runtime errors/config. | The desired state and optional explicit precondition; no owner-token composition or server revision prediction. |
| Evidence append/remove | Append is must-exist, exact-tuple deduplicated, batch partial by subject; remove-missing is currently success. | Append to absent returns typed failure; broad retry of partial batch may duplicate only non-identical facts; remove cannot distinguish absent entity from absent predicate. | Request/response types, handler behavior, projection appender. | Operation-specific typed outcomes and failed subset; no bucket or CAS mechanics. |
| Delete | Current delete is absent-success and has no condition. | Retrying is safe under current handler, but a delete racing a newer write has no caller-supplied fence. | Request/response comments and lifecycle emitter. | Explicit delete semantics and optional operation precondition, not direct KV access. |
| Caller-owned not-found retry | Which read returns current value plus the same-entry revision, which errors are stable absence, and whether the intended later write is conditional. | Current admitted read omits revision; caller can only retry unconditionally, use a private/direct reader, or stop. | #851, scattered code/specs; no single admitted typed path. | One operation-specific exact read result with classified absence and its usable revision; no provenance calculation. |
| Projection component | Contract/group/mode names, registry/heartbeater wiring, owner ID, liveness, token opacity, full group replacement, read-back verification. | Boot can fail on overlap; missing heartbeater blocks owning contract; absent substrate/token may silently change enforcement posture. | `pkg/projection` docs/types/spec, service wiring. | Entity pattern, message type, allowed facts, desired set/append intent; ideally no semantic registry, heartbeat, lease, or token knowledge. |
| Lifecycle adopter | Workflow registration, direct authority dependency, watch cancellation, revision/CAS retry, H1 history limit, ownership attach/quiesce/token behavior. | Transition handles CAS internally, but history can expose only one retained revision; ownership-disabled paths fail open. | lifecycle package docs, ADR/spec, #681/#843, runtime errors. | Workflow phases/fields and typed operation results; no bucket, history-depth, registry, or incarnation knowledge. |
| Foreign relationship producer | Primary versus foreign subject; claim message type/predicate/inverse/mode; target birth ordering; strict/drop/deferred/stub behavior. | Unclaimed/failed lookups fail open to append, but absent target then drops through must-exist partial failure; no durable pending retry exists. | projection contract, ADR-056, graph-ingest metrics/WARNs. | Emit a relationship fact and observe committed/pending/broken-reference outcome; no prediction of target existence or claim mode. |
| Consumer of referenced target | Stub message type and marker, possibility of later restamp, and whether its component must exclude stubs. | Treating a stub as real can dispatch/promote incomplete entities; removing auto-stubs makes previously resolvable IDs absent/dangling. | `graph/stub.go`; gated-DAG/lesson code; lifecycle reference API. | A typed “real / unresolved reference / absent” result at the read seam; no implementation marker knowledge. |
| External NATS caller | Exact subjects, JSON shapes, classified reply headers/codes, request timeout/retry, and the fact multiple responders can execute. | Transport retry can replay a non-idempotent semantic operation; direct subscription topology remains invisible. | graph request types, natsclient RPC docs, component-specific client wrappers. | A supported typed client and operation outcome; no subject, responder-count, or backoff prediction. |
| Operator | Ownership buckets/service, enforcement flag, heartbeat TTL, revival quiesce, mutation and foreign-edge metrics. | Default lease enforcement is off; semantic-owner state may age/compact; broken edges surface in WARN/counters, not a pending queue. | config schema, service/component logs and metrics, ADR-056. | Health and named actionable failures; no registry internals. |

### Consumer-at-birth check

No new surface is proposed, so there is no new external consumer to place at birth. Any later accepted removal or mutation
contract must enumerate the external component developer above and the concrete shipped consumers in sections 2, 4, 6,
7, and 8 before changing artifacts.

### Owner-ruling boundary

This inventory does not decide which semantic-ownership types survive, whether projection modes are renamed or retained,
how dangling-reference observation is represented, whether delete/remove absence semantics change, which exact-read front
door is admitted, or how cross-process graph-ingest execution is constrained. Those are binding rulings for the owner
after independent review.
