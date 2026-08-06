# Post-R1b foundation inventory

Status: **inventory draft; not implementation authority**.

This is the fresh repository-first inventory required after R1a and R1b merged. It records current behavior and
collisions only. It does not approve a next slice, amend the frozen target, authorize runtime work, or modify
historical artifacts.

## 1. Identity

- repository: `C360Studio/semstreams`
- branch inspected: `codex/post-r1b-inventory`
- baseline: `313579fd8b66f0af9c62a8a05d9b9f9fffa486b4`
- baseline commit: `refactor(lifecycle): localize graph poison (#904)`
- worktree during inspection: clean
- R1a merges:
  - `f11f03b9` — catalog read-only acquisition
  - `dd02a715` — exact package-local reader narrowing
- R1b merge:
  - `313579fd` — lifecycle poison localization and ADR-092
- inspection mode: read-only
- artifact SHA-256: recorded externally after materialization

Accepted execution authority remains:

- decomposed design SHA-256:
  `e1d7c47898824b4bfdca33a4e53da75dd4d59af147315ba2871f2cbebe2c017f`
- roadmap amendment SHA-256:
  `85c837aca8ccbf38483848f322c85aba929596f24f5e517b125b6bc42a883e5b`
- decomposed design review SHA-256:
  `ed1fc0a4ae4cd87225ff8ca6d6728e07e84f56deb8e59891bebf6d0f5a8b15d2`
- accepted inventory SHA-256:
  `b5bb0fa79f584a7ec8e06965d9885b9cd87629791f0accd620d5043c2bbfc22c`

Those identities come from
`docs/proposals/post-gs01-r1-decomposed-execution-design-approval.md:3-23`.

## 2. Claimed gap after R1a and R1b

R1a acquisition narrowing and R1b lifecycle poison localization are present. The previously approved remaining R1
sequence cannot be inherited mechanically:

1. R1c production retry behavior is already present. The remaining gap is canonical contract truth and missing
   characterization.
2. R1d's accepted inventory is incomplete. Shipped configuration already contains silently ignored `kv_read` fields,
   and the port-declaration model has an accepted pre-v1 replacement issue.
3. R1e's catalog-only message-logger boundary remains a real outward-facing gap, but it is not retry work and has
   adjacent watcher/access issues that must not be absorbed accidentally.
4. The active baton, active OpenSpec task state, accepted artifact headers, and older program ledger do not all describe
   the merged repository truth.

No production code change is authorized by this inventory.

## 3. Retry and lifecycle surface inventory

### 3.1 Lifecycle transition retry

`pkg/lifecycle/manager.go:497-501` owns a five-attempt component-local retry budget.

`Transition` and `TransitionWith` enter `transitionWithOutcome` at
`pkg/lifecycle/manager.go:503-539`. On every attempt the implementation:

- exact-reads current authority and revision at `pkg/lifecycle/manager.go:553-557`;
- re-extracts phase and rechecks managed, terminal, and declared-edge conditions at
  `pkg/lifecycle/manager.go:558-570`;
- decodes and validates the transition occurrence chain at `pkg/lifecycle/manager.go:571-585`;
- reconstructs timestamp, occurrence, phase, and audit desired state at
  `pkg/lifecycle/manager.go:579-603`;
- reprojects current authority and reruns the optional caller mutator at
  `pkg/lifecycle/manager.go:604-617`;
- rebuilds predicate authority and `ExpectedRevision` at `pkg/lifecycle/manager.go:619-631`;
- retries only definite revision mismatch at `pkg/lifecycle/manager.go:643-645`; and
- returns every other error immediately and reports bounded exhaustion at
  `pkg/lifecycle/manager.go:647-650`.

This is already the owner-local full-intent retry required by the accepted R1c design.

The claim is specific to `Transition` and `TransitionWith`. `UpdateFromOperator` also has a bounded mismatch loop at
`pkg/lifecycle/manager.go:719-762`, but it precomputes the operator patch before that loop. The transition-intent claim
must not be generalized to every lifecycle operation.

The current test fake has only an always-mismatch switch at `pkg/lifecycle/manager_test.go:20-82`.
`TestCreate_UnrelatedConcurrentUpdateIsNotADuplicateBirth` at
`pkg/lifecycle/manager_test.go:1310-1335` exercises lifecycle attach/create contention. It does not exercise
`Transition` or `TransitionWith`, does not prove transition-loop exhaustion, and does not script a first mismatch
followed by changed authority. No transition test presently proves revalidation of phase, edge, audit chain, projected
fields, and mutator against changed authority.

R1b's deletion is present: lifecycle retains only `ListKeys` and `Watch` on its catalog reader at
`pkg/lifecycle/manager.go:30-33`; no Manager-wide guard, poison latch, or lifecycle `WatchAll` remains.

### 3.2 Rule and projection retry

`pkg/projection.MutationClient.Reconcile` canonicalizes the requested group, exact-reads once, and sends one reconcile
request at `pkg/projection/mutation_client.go:176-199`. It has no automatic retry loop.

`TestReconcileReadsExactRevisionThenMakesOneMutation` proves one exact read plus one mutation on success at
`pkg/projection/mutation_client_test.go:189-226`. `TestRevisionMismatchRemainsDefinite` classifies mismatch at
`pkg/projection/mutation_client_test.go:270-287`, but does not assert that no third request was sent.

The rule action constructs desired state and request metadata from its current `ExecutionContext`, invokes
`Reconcile` once, and returns an error directly at `processor/rule/actions.go:1099-1141`. Existing rule characterization
at `processor/rule/actions_reconcile_test.go:228-258` proves one successful high-level call, not the mismatch path or
non-replay of an old `ExecutionContext`.

The canonical projection-client spec matches runtime:
`openspec/specs/projection-mutation-client/spec.md:60-73` requires one read, one mutation, and no automatic retry.

The canonical rule spec contradicts runtime:
`openspec/specs/rule-projection-mutations/spec.md:82-94` still requires one bounded retry and recomputation.

The active foundation delta already records the correct one-attempt contract at
`openspec/changes/establish-graph-read-write-foundation/specs/rule-projection-mutations/spec.md:81-93`.

The adopter concept document is also stale:
`docs/concepts/28-governed-semantic-state.md:42-51` says the rule engine permits one bounded retry.

The lifecycle spec at `openspec/specs/lifecycle/spec.md:77-87` permits bounded component reread and recomputation but
does not enumerate the full present transition-intent reconstruction on each attempt.

Bare rule add/remove remains a separate mutation lane at `processor/rule/triple_mutator.go:36-90`. Open issue #688
must not be absorbed into retry-contract truth.

## 4. Component declaration surface inventory

### 4.1 Existing KV and store-read grammar

There is no `component.KVReadPort`.

`component/port_kv.go:5-47` defines only:

- `KVWatchPort`, serialized as `kvwatch`; and
- `KVWritePort`, serialized as `kvwrite`.

`component.PortConfig` contains only `Inputs`, `Outputs`, and `KVWrite` at `component/ports.go:155-160`.

A separate existing read-port class is `StoreReadPort` at `component/port_store.go:5-27`:

- token: `store-read`;
- resource identity: `store-read:<bucket>`;
- semantics: backend-neutral streaming content federation, not exact NATS KV authority access.

`component/ports.go:297-313` constructs `StoreReadPort`.
`component/flowgraph/flowgraph.go:71-77,225-238` maps it to `PatternStore`.
Graph-embedding is its present production consumer. It must remain semantically distinct from any future exact-bucket
KV-read declaration.

### 4.2 Phantom `kv_read` configuration

Despite the absence of `PortConfig.KVRead` and `KVReadPort`, shipped configurations already contain `kv_read` arrays
whose rows use `type: "kv-read"`:

- `configs/agentic.json:412-419`;
- `configs/research-graph-e2e.json:201-203,232-234`;
- `configs/flows/deep-research.json:171-172`;
- `configs/flows/lesson-example.json:183-184`;
- `configs/flows/deep-research-test.json:193-194`;
- `configs/flows/ops-agent.json:190-191`; and
- `configs/examples/research-graph-pipeline.json:184-185,215-216`.

These top-level `kv_read` fields are ignored by the current `PortConfig` decoder. They are phantom declarations, not
runtime dependencies. Adding a `KVRead` field can activate pre-existing configuration instead of merely adding a new
token.

This falsifies the accepted R1d statement that its checked-in configuration census consists exactly of four
statistical/semantic configs at
`docs/proposals/post-gs01-r1-decomposed-execution-design.md:324-342`.

### 4.3 Gated-DAG

Gated-DAG declares no inputs and says re-evaluation rides only lifecycle Watch at
`processor/gated-dag/component.go:272-274`.

Runtime also opens an optional catalog `ENTITY_STATES` watch for `UnitEntityPrefix + ".>"` at
`processor/gated-dag/executor.go:141-155,357-403`.

Failure to start the optional watcher warns and periodic evaluation remains the correctness floor. Unexpected closure
of the watcher's update channel returns silently at `processor/gated-dag/executor.go:377-380`.

Open issue #689 concerns gated-DAG claim CAS and is not a declaration-truth issue.

### 4.4 Graph clustering

Clustering currently declares one `kv-watch` input for `ENTITY_STATES` at
`processor/graph-clustering/component.go:448-492`.

Runtime actually waits for and opens three current-state readers:

- `ENTITY_STATES`;
- `OUTGOING_INDEX`; and
- `INCOMING_INDEX`.

The acquisition is at `processor/graph-clustering/component.go:1137-1199`. Package-local reader capabilities are
declared at `processor/graph-clustering/component.go:519-530`. Clustering holds no authority watcher; periodic
detection reads current state.

The declaration therefore overstates watch semantics and understates two required read dependencies.

### 4.5 Port-model collisions

Open issue #859 records duplicated port-type interpretation across the repository. A new token would add another value
to that duplicated grammar unless the existing interpretation seams are consolidated.

Open issue #862 is the owner-ruled pre-v1 breaking replacement of the component declaration model: seal
`Discoverable` so components declare ports and the framework renders them. The durable ordering at
`docs/proposals/prev1-program.md:133-138` places #859 before #862.

The suspended discovery change also records #862 as superseding the present model at
`openspec/changes/discovery-under-stream-shapes/tasks.md:105-118`.

Any future declaration slice must inventory #859, then #862, the phantom `kv_read` rows, `StoreReadPort`, flowgraph
matching, generated schemas, and present component consumers together. The old R1d boundary is not executable as
written.

## 5. Message-logger outward surface inventory

Message-logger registers arbitrary KV query and SSE watch routes at
`service/message_logger_http.go:26-45`.

Its OpenAPI accepts a caller-selected bucket name at `service/message_logger_http.go:148-229`.

The query path exposes generic `GetKeyValueBucket` acquisition at
`service/message_logger_http.go:361-369,459-490`.

The SSE path performs the same generic acquisition and creates a request-local `Watch` or `WatchAll` at
`service/message_logger_kv_watch.go:195-232`.

Comments describe these routes as development/test only, but
`service/message_logger_http.go:386-388` allows them in every environment and only emits a warning.

The framework ships zero default HTTP middleware, including zero default authorization, at
`service/middleware.go:5-23`. Product middleware is the actual authorization owner.

The old R1e catalog-only boundary therefore remains a real outward-facing gap. It is adjacent to, but not automatically
combined with:

- #587 — per-client message-logger graph-view/watch behavior; and
- #472 — entries filtering before limit.

Catalog narrowing must not silently become shared-view adoption, filtering repair, framework authentication, or a
generic diagnostics access framework.

## 6. GRAPH_STATUS inventory

`GRAPH_STATUS` is a catalog operational bucket. Its current catalog owner string names only graph-index and
graph-embedding at `graph/kvcatalog.go:69-74`.

The shared identifiers at `graph/readiness/watcher.go:39-70` define four producer keys:

- `graph-index`;
- `graph-embedding`;
- `graph-ingest`; and
- `rule`.

Production publishers exist in all four corresponding packages.

Present production consumers include:

- `graph/query.Client`, watching `graph-index` at `graph/query/client.go:421`;
- `pkg/fusion/fusionnats.Client`, lazily opening and retaining `graph-index` at
  `pkg/fusion/fusionnats/client.go:115-168`;
- graph-clustering, watching required `graph-index` and optional `graph-embedding` at
  `processor/graph-clustering/component.go:1363-1403`; and
- graph-gateway readiness rows configured from explicit keys.

E2E also consumes configured sets but is not a production owner.

Fusion treats a transport lacking the narrow readiness bucket source as a wiring error. It spends one bounded wait for
the first status delivery, then uses the retained local reading. Unknown remains fail-closed.

Open #868 asks for generalized `GRAPH_STATUS`; the approved later R3 target instead requires an atomic role-typed
cutover. Open #820 asks for clustering status evidence. Neither belongs to R1 retry truth.

## 7. Deferred derived-owner inventory

`pkg/graphview` is an existing shared `WatchAll` current-state projection with poison, restart, and fan-out semantics at
`pkg/graphview/doc.go:1-60`.

Its current production adoption is agentic-dispatch over `AGENT_LOOPS`. It has no production `ENTITY_STATES` consumer.

`pkg/revlag` is an existing sparse-revision caught-up watermark at `pkg/revlag/watermark.go:1-16`. Current production
consumers are graph-index and graph-embedding only.

No three-owner same-semantics/reduced-code proof exists for a broader shared derived-view runtime. Lifecycle,
gated-DAG, clustering, message-logger, and GRAPH_STATUS retain different scopes, recovery rules, and failure semantics.
Derived-owner extraction remains deferred.

## 8. Issue disposition and durable-ledger drift

Live GitHub state inspected on 2026-08-06:

- #861: closed manually;
- #870: closed manually;
- #869: closed manually at `2026-08-06T18:53:20Z`;
- #871: closed manually at `2026-08-06T18:53:30Z`;
- #688: open;
- #689: open;
- #820: open;
- #859: open;
- #862: open;
- #868: open;
- #472: open;
- #571: open; and
- #587: open.

PRs #901, #902, and #904 have no automatic closing-issue references. Closure of #861, #869, #870, and #871 must not be
attributed to those PRs without an owner record.

`docs/proposals/prev1-program.md:83-105` still promotes #869 as an open request-scoped idempotency primitive and
describes #870/#871 as dependent work. That ledger conflicts with live issue state. #689 remains open even though its
older blocker language depends on the now-closed #869.

Issue closure does not itself prove implementation, rejection, or supersession. The owner must record the actual
disposition rather than inferring it from the GitHub state.

## 9. Program-control truth drift

The active baton still says no runtime slice is implemented and R1-R9 are unimplemented at
`docs/proposals/post-gs01-graph-read-derived-foundation-baton.md:33-41`. That is false after #901, #902, and #904.

The active `establish-graph-read-write-foundation` change still leaves final reviewer/merge task 8.8 unchecked at
`openspec/changes/establish-graph-read-write-foundation/tasks.md:81-96`, despite the merged foundation cutover. Task 7.3
was later amended for R1b, so the change mixes completed historical cutover truth with later successor truth.

The accepted decomposed execution design still labels itself “pre-owner review draft; not approved” at
`docs/proposals/post-gs01-r1-decomposed-execution-design.md:1-4`.

The accepted roadmap amendment still labels itself “proposed…not approved” at
`docs/proposals/post-gs01-r1-roadmap-amendment.md:1-4`.

Their separate approval record says both are accepted at
`docs/proposals/post-gs01-r1-decomposed-execution-design-approval.md:3-17`.

Those frozen artifacts must not be silently edited. A successor baton/control record must state their accepted
content identities and current execution status without rewriting historical bytes.

The other active OpenSpec changes, `discovery-under-stream-shapes` and `semantic-tier-split`, explicitly mark themselves
SUSPENDED AND FROZEN in their proposals/tasks. They are not competing executable work.

## 10. Same-class collision table

| Proposed/current fact | Existing same or adjacent class | Present owner/consumer | Inventory disposition |
|---|---|---|---|
| Rule CAS retry | Projection client's one-attempt exact-read/reconcile | Rule action | No shared retry; stale `ExecutionContext` is not replayable |
| Lifecycle CAS retry | Owner-local transition reconstruction | Lifecycle Manager | Preserve locally; characterize changed-authority attempts |
| Exact KV-read declaration | `KVWatchPort`, `KVWritePort`, phantom `kv_read`, `StoreReadPort` | Flowgraph and components | Old R1d boundary is incomplete; remap with #859/#862 |
| Gated-DAG optional prefix watch | Lifecycle Watch plus periodic evaluation | Gated-DAG | Declaration gap only; #689 remains separate |
| Message-logger KV access | Generic lookup, per-request watcher, product middleware | HTTP operators/products | Catalog boundary remains separate from #587/#472/auth |
| GRAPH_STATUS | Four producers, query/fusion/clustering/gateway consumers | Graph operational plane | Preserve for later atomic role-typed work |
| Shared current-state view | `pkg/graphview` | Agentic-dispatch only | No broader extraction without three-owner proof |
| Revision watermark | `pkg/revlag` | Graph-index and embedding | No broader extraction without third owner |

No new bucket, stream, service, status key, metric, retry coordinator, graph-source injector, or universal view is needed
to correct retry truth.

## 11. Adopter seam inventory

| Specific adopter | What they must know today | If they do nothing | Where they discover it | What they should have to know |
|---|---|---|---|---|
| External rule author | Rule reconcile makes one exact read and one mutation attempt | A racing writer returns visible `revision_mismatch`; old context is not replayed | Runtime errors, projection spec; canonical rule spec and concept doc currently mislead | No retry count or authority prediction |
| Lifecycle caller | Transition may internally retry definite mismatch | It succeeds from observed current authority or returns a typed terminal/exhaustion error | Lifecycle API/spec | No retry knob or per-attempt mechanics |
| External component author | Declared ports do not currently tell the truth for clustering/gated-DAG; `kv_read` config is ignored | Flowgraph/dependency metadata can be incomplete or false without changing runtime | Component declarations/config schema | Required facts and optionality, not raw NATS acquisition |
| Message-logger operator | Arbitrary bucket query/watch is reachable when routes are served; framework auth is absent | Product/application KV may be observable to callers admitted by deployment routing | OpenAPI, handler comments, product middleware docs | A truthful access boundary, not an “operator-only” claim the framework does not enforce |
| GRAPH_STATUS consumer | It must name only producer keys it actually depends on and treat absent/aged readings as unknown | Standalone/partial deployments remain fail-closed or degrade according to local policy | Readiness package and consumer config | No global producer-set prediction |
| Derived-owner implementer | `graphview` and `revlag` solve narrow existing classes | Nothing automatically adopts them | Package docs and present imports | No universal convergence framework |

The generative rule is observation over prediction:

- lifecycle observes fresh authority and reconstructs intent after a definite conflict;
- rule refuses to predict that old `ExecutionContext` remains safe;
- callers receive real typed outcomes rather than a retry knob;
- a future declaration should describe present dependencies, not imply runtime watch semantics that do not exist; and
- message-logger policy must describe actual middleware/catalog enforcement rather than an operator label.

## 12. Verification performed

The focused current-state suite passed:

```text
go test ./pkg/projection ./processor/rule ./pkg/lifecycle \
  -run 'Test(ReconcileReadsExactRevisionThenMakesOneMutation|RevisionMismatchRemainsDefinite|Action|Transition)' \
  -count=1
```

Independent reviewer verification also passed:

```text
go test -race ./pkg/lifecycle ./pkg/projection ./processor/rule
```

The worktree remained clean at `313579fd`.

## 13. Inventory exclusions

This inventory does not authorize or combine:

- #688 bare rule append/remove changes;
- #689 gated-DAG claim changes;
- #472 message-log filtering changes;
- #587 shared message-logger view adoption;
- #571 graph/query watcher deletion;
- #820 or #868 GRAPH_STATUS changes;
- #859 or #862 implementation;
- `COMPONENT_STATUS` deletion;
- graphview/revlag generalization;
- query result, pagination, absence, or external API changes;
- metrics, configuration knobs, buckets, streams, services, status keys, or coordination primitives;
- edits to frozen historical artifacts; or
- runtime implementation of R1d or R1e.

## 14. Inventory gate

This artifact must be materialized unchanged, measured, hashed, and reviewed independently.

No target-state option, next-slice recommendation, implementation authorization, or binding ruling follows until the
reviewer returns `INVENTORY PASS` for this exact content identity. The owner retains every binding decision.
