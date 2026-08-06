# R1 design — catalog acquisition, lifecycle poison localization, and retry truth

Status: **replacement architect draft for independent pre-owner review; not approved**.

## 0. Replacement status

This artifact completely supersedes the rejected 480-line / 27,821-byte design at SHA-256
`34d68d6a2f2eb48a5585568b3955d0d49e9c378b30b2e70ba38720c589b5da6c`, which received
`DESIGN CHANGES REQUIRED`. The rejected hash remains historical evidence only and cannot authorize implementation.

This replacement:

1. rules explicitly on the existing `component.StoreReadPort` owner before recommending `KVReadPort`;
2. removes the "where practical" loophole and binds exact local reader method sets; and
3. classifies R1 as unconditionally BREAKING with exact lifecycle, message-logger, and clustering E2E plus an owned
   gated-DAG coverage gap.

## 1. Authority

This design binds without modifying the accepted inventory:

- artifact: `docs/proposals/post-gs01-r1-acquisition-lifecycle-retry-inventory.md`
- lines/bytes: 487 / 35,930
- SHA-256: `b5bb0fa79f584a7ec8e06965d9885b9cd87629791f0accd620d5043c2bbfc22c`
- review: `docs/proposals/post-gs01-r1-acquisition-lifecycle-retry-inventory-review.md`
- verdict: `INVENTORY PASS`

Other authority:

- approved foundation design SHA-256:
  `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`
- approved roadmap SHA-256:
  `0f16d7de739ea70c09312a897089ca01b79c28c9e43fbf0b78bf596bdc1504a2`
- repository HEAD/main: `6ce137009fe6cf019dcb0a9a2a5122e81c2f9d27`
- runtime baseline: `d1570ef81b23096021af0d7bf3321b4c08c7e54b`

The ten-point identity packet remains unchanged. `ENTITY_STATES` remains current authority; graph-ingest remains its
sole physical writer; mutations remain typed request/reply with CAS; eventual consistency remains explicit. R1 adds
no recovery system, CQRS runtime, semantic-ownership system, general graph client, graph-source injector,
compatibility path, universal derived-state runtime, bucket, stream, service, status key, or operation family.

The `kv-or-stream` decision skill applies because `KVReadPort` declares an inter-component current-state dependency.
All four tests select KV facts rather than a JetStream work stream: restart re-reads current facts; readers fan out;
observations are idempotent; and entity/index rows are facts, not requests. `KVReadPort` is metadata for point,
enumeration, snapshot, or poll access. It adds no watcher, acknowledgment, replay, durable consumer, cadence, or
acquisition runtime. `new-payload`, `orchestration-check`, and `query-pattern` do not trigger.

## 2. Problem statement

R1 closes three current-truth gaps:

1. Framework catalog buckets already have owner-Ensure and reader-Open contracts, but surviving readers still bind
   through raw JetStream or generic acquisition.
2. Lifecycle currently makes one malformed authority value a Manager-wide admission failure through a process-lifetime
   `WatchAll` guard and sticky poison latch.
3. Rule runtime and the projection client already perform one exact read and one mutation attempt, while the canonical
   rule spec still requires automatic conflict retry. Lifecycle transition retry is legitimately different because
   every attempt rereads authority and reconstructs the complete transition intent.

R1 also makes two outward declarations truthful: gated-DAG declares its existing optional unit-prefix authority watch,
and graph-clustering declares periodic KV reads rather than claiming an active watch.

## 3. Measured premises

| Premise | Measurement |
|---|---|
| R0 changed no runtime | R0 source commit `e4ce3576` and merge `6ce13700` contain documentation only. |
| Owner/read acquisition exists | `graph/kvcatalog.go:214-241`; `natsclient/kvspec.go:224-319`; `graph-retention/spec.md:200-241`. |
| Graph-ingest is the sole physical writer | Accepted inventory names every graph-ingest write seam and found none elsewhere. |
| Named R1 readers bypass Open | Accepted inventory enumerates graph-index, spatial, temporal, embedding, clustering, lifecycle, rule watch, and message logger. |
| Mandatory narrowing is frozen | `post-gs01-graph-read-derived-foundation-design.md:850-853` forbids a full reader handle escaping acquisition. |
| Adjacent read declaration exists | `component.StoreReadPort` at `component/port_store.go:5-26`, resource `store-read:<bucket>`. |
| Store-read owns content federation | `component/flowgraph/flowgraph.go:235-238,313-316,598-637`; ADR-063 `:377-386,398-405`. |
| Store federation differs from KV state | It fans every provider into every reader and resolves a producer-selected backend/content reference per fetch. |
| Lifecycle guard/latch is removable | 60 production `graphStateGuard|graphStatePoison` references; `pkg/lifecycle/manager.go:64-102`; `manager_query.go:208-479`. |
| Guard has no production shutdown owner | `graphStateGuardCancel` has only its field and assignment in production. |
| Rule is already one-attempt | `processor/rule/actions.go:1108-1153`; `pkg/projection/mutation_client.go:176-200`; `processor/rule/triple_mutator.go:67-90`. |
| Canonical rule truth conflicts | `rule-projection-mutations/spec.md:82-94` requires retry; `projection-mutation-client/spec.md:60-73` and ADR-091 prohibit it. |
| Lifecycle transition rebuilds intent | `pkg/lifecycle/manager.go:623-716` rereads, revalidates, recomputes, and reruns the mutator. |
| Gated-DAG owns but does not declare its watch | `processor/gated-dag/executor.go:355-386`; `component.go:272-274`. |
| Clustering polls but declares `kv-watch` | `processor/graph-clustering/component.go:448-505,1124-1186,1302-1347,1869-1879`. |
| No KV read/snapshot port exists | `KVReadPort`, `KVSnapshotPort`, `kv-read`, and `kv-snapshot` searches found no production definition; this does not erase adjacent `StoreReadPort`. |
| Port interpretation is centralized | `component/port_kv.go`; `component/port.go:105-165`; `component/ports.go:250-310`; `component/flowgraph/flowgraph.go:197-300`. |
| Message logger has no authorization boundary | Routes register at `service/message_logger_http.go:26-47`; no default middleware at `service/middleware.go:17-23`; arbitrary buckets at `message_logger_http.go:148-196,361-490`. |
| Lifecycle has no dedicated metric | Production metric-definition search under `pkg/lifecycle` returned zero. |
| WARN already has a bounded consumer | `semstreams_log_entries_total{component,level}`; `docs/operations/01-local-monitoring.md:126-146`; `cmd/semstreams/logging.go:33+`. |
| Lifecycle has a production-wire E2E | `Taskfile.yml:69-70`; `taskfiles/e2e/lifecycle.yml`; `test/e2e/scenarios/lifecycle/scenario.go:1-19`. |
| Graph roundtrip runs in core and every tiered variant | `test/e2e/scenarios/tiered.go:245-263`; `test/e2e/scenarios/graph_roundtrip_scenario.go:10-47`. |
| Core has a deterministic absent owner | `configs/protocol-flow.json` omits graph-embedding, owner of `EMBEDDING_INDEX`. |
| No gated-DAG E2E/fault seam exists | Targeted configs/docker/e2e searches found no deployment or isolated watch-loss mechanism. |
| Statistical E2E covers clustering | `test/e2e/scenarios/tiered.go:341-355`. |
| E2E uses message-logger catalog reads | `test/e2e/scenarios/graph_roundtrip.go:174,576`; `test/e2e/client/messagelogger.go:228`. |

## 4. Program options

### Option 0 — do nothing or defer R1

Keep raw acquisition, Manager-wide poison, false clustering watch metadata, undeclared gated-DAG dependency, and
conflicting rule specs. This leaves the accepted target unimplemented and makes later slices build on a known-bad
foundation.

### Option 1 — acquisition-only partial R1

Normalize bucket acquisition but defer lifecycle poison, declaration truth, diagnostics, and rule spec repair. This is
a smaller diff but violates R1's atomic outcome, leaves the worst behavioral defect, and forces later slices to reopen
the same surfaces.

### Option 2 — smallest coherent R1

Normalize only surviving R1 reader acquisitions, delete lifecycle's global guard/latch, localize watch poison, make
gated-DAG and clustering declarations truthful, narrow message-logger KV diagnostics to catalog buckets, and repair
rule/lifecycle retry truth.

Cost: one exported metadata type and one serialized port/pattern token, with graph-clustering as consumer at birth; a
clean clustering declaration break; a clean message-logger scope break; focused proof; and relevant core, lifecycle,
and statistical E2E.

### Option 3 — shared graph source/view or retry runtime

Introduce injection, `pkg/graphview` authority singleton, generalized watcher coordination, or shared CAS retry. This
conflicts with the approved target, imports owner-specific lifecycle and recovery semantics, and lacks the three-owner
reduced-code proof.

Architect recommendation: **Option 2**.

## 5. Read-port collision and pre-owner rulings

These are recommendations, not binding decisions.

### 5.0 Same-class read-declaration collision

| Dimension | `StoreReadPort` | KV watch/write | Candidate `KVReadPort` |
|---|---|---|---|
| Semantic job | Fetch referenced large content from backend-neutral storage. | Publish current KV state or bootstrap/watch facts. | Point/list/snapshot/poll current KV facts without claiming a watch. |
| Present owner/consumer | Store registry/federation; graph-embedding content readers. | Current KV owners and watchers. | No owner yet; clustering is consumer at birth. |
| Grammar | `store-read` | `kvwrite`/`kv-write`/`kv`; `kvwatch`/`kv-watch` | Exactly `kv-read`; no alias. |
| Resource ID | `store-read:<bucket>` | `kvwrite:<bucket>` / `kvwatch:<bucket>` | `kvread:<bucket>` |
| Flowgraph pattern | `store` | `watch` | Serialized `kv-read`; internal constant need not be exported. |
| Consumer connection | `store-federation` | Exact bucket | Exact bucket |
| Producer connection | `store:<instance>` | Exact bucket | Exact bucket from `KVWritePort` |
| Matching | Every store provider fans into every store reader. | Same-bucket writer/watch. | Same-bucket writer/read; different buckets and store ports never match. |
| Runtime | Resolve producer-selected storage instance per content fetch. | State write or bootstrap/live subscription. | Existing local read/poll code; metadata adds no runtime. |
| Lifecycle | Advisory federation. | Component owns watcher lifetime. | Existing Start/lazy/request reader lifetime. |
| Adopter knowledge | Content references and storage instances. | Write versus live watch and bucket. | Read current values without subscribing, and bucket. |
| Exported cost | Existing. | Existing. | One Go type, one port token, one pattern value. |

Options:

- Reuse `StoreReadPort`: rejected because federation fan-in would claim clustering can read arbitrary content stores.
- Add a mode to `StoreReadPort`: rejected because resource identity, matching, substrate, and instructions become
  conditional, recreating the erased distinction.
- Remove clustering's watch with no replacement: rejected because it hides three required storage inputs.
- Add distinct `KVReadPort`: recommended because exact-bucket KV state, content federation, and reactive watch are
  stable separate jobs with no cross-match.

`StoreReadPort` semantics and matching remain unchanged. The extra outward concept is admitted only for clustering's
three current inputs and must be counted in the complexity ledger.

### 5.1 Graph-index reservation

- Defer graph-index acquisition to R3: preserves wording but violates the explicit R1 surface and outcome.
- Edit it informally in R1: preserves semantics but defeats shared-file control.
- Correct the reservation explicitly: changes program metadata, not target semantics, and requires a reviewed roadmap
  identity.

Recommendation: reserve `processor/graph-index/component.go` R1 → R3 → R4 → R5a. Keep query/watermark R3 → R4 → R5a.
R1 owns only authority acquisition, local handle narrowing, and coupled tests.

### 5.2 Gated-DAG owning truth

- Code/ADR only leaves current capability truth silent.
- Framework-composition only loses the gated-DAG prefix and correctness distinction.
- Gated-DAG OpenSpec plus generic framework-composition truth records each fact at its owner.

Recommendation: update `openspec/specs/gated-dag-dispatch/spec.md`; use framework-composition only for generic
required/optional port truth. Preserve ADR-046.

### 5.3 Gated-DAG failure posture

- Keep an undeclared best-effort watch: runtime remains correct but inspection stays false.
- Fail Start: turns a latency path into a correctness dependency.
- Declare the watch optional and degrade visibly: preserves periodic correctness and makes dependency loss observable.

Recommendation: declare optional input `unit_entity_watch`, `KVWatchPort`, bucket `ENTITY_STATES`, keys exactly
`UnitEntityPrefix + ".>"`. Open/watch failure logs once and Start continues. Unexpected closure logs once and stops only
that watcher. Periodic reevaluation continues. Add no status or metric.

### 5.4 Clustering port vocabulary

The collision ruling recommends one distinct metadata concept for point/enumeration/snapshot/periodic reads without
runtime acquisition semantics:

```go
type KVReadPort struct {
    Bucket    string
    Interface *InterfaceContract
}
```

Contract:

- canonical serialized type `kv-read`; reject `kvread`, `kv-snapshot`, `store-read`, and every compatibility spelling;
- resource identity `kvread:<bucket>`; non-exclusive;
- flowgraph pattern serializes as `kv-read`; connection identity is the exact bucket;
- only a same-bucket `KVWritePort` matches; different buckets and all store ports do not match;
- represents point, enumeration, snapshot, or periodic read access;
- implies no Watch, replay, injection, acquisition, cadence, or shared runtime;
- flowgraph connects KV-write producers to KV-read consumers by bucket.

Clustering declares three required inputs: `entity_snapshot` → `ENTITY_STATES`, `outgoing_snapshot` →
`OUTGOING_INDEX`, and `incoming_snapshot` → `INCOMING_INDEX`. Existing `kv-watch` configuration is rejected, not aliased.

### 5.5 Message-logger “operator-only”

- Documentation-only “operator-only” makes an unenforced security claim.
- Framework authentication imports product identity/policy into SemStreams.
- Capability-scope restriction to catalog diagnostics removes the accidental application-bucket API while leaving
  authorization with product middleware.

Recommendation: the route is an operational diagnostic surface, accepts only framework catalog names, opens through
`graph.OpenCatalogBucket`, and never creates/writes. It is not an authorization boundary; product middleware remains
responsible for access control.

- off-catalog bucket: HTTP 400 in the current invalid-input envelope;
- catalog bucket absent: HTTP 503 with current `index_not_ready` and owner-bearing message;
- existing catalog bucket: current read/watch behavior;
- watch validates and opens before SSE headers;
- unexpected closure uses the current SSE error form and closes;
- no allowlist override, alternate route, parameter, or compatibility path.

R1 does not change the service's default-enabled posture.

### 5.6 Message-logger owning truth

- Source comments only leave the HTTP boundary non-binding.
- Graph-retention overloads acquisition truth with HTTP/operator semantics.
- A focused diagnostic capability spec plus operator runbook owns the actual changed surface.

Recommendation: add `openspec/specs/message-logger-diagnostics/spec.md` for catalog-only read/watch, no-create,
owner-not-ready, off-catalog rejection, operator capability classification, and product middleware boundary. Update
`docs/operations/debugging-data-flow.md` beside the first KV example.

### 5.7 Lifecycle logs and metrics

- Per-entity metric has no consumer and creates unbounded cardinality.
- Unstructured log cannot identify scope.
- One terminal structured WARN per affected subscription gives exact drill-down while the existing WARN counter gives
  a bounded aggregate.

Recommendation: add no lifecycle metric. On malformed matching watch value, emit one terminal WARN with
`component="lifecycle"`, `operation`, `workflow`, `entity_id`, `revision`,
`code="graph_state_reset_required"`, and `reason`, then close that subscription. The existing
`semstreams_log_entries_total{component,level}` observes the count without entity/revision labels. Exact/List continue
returning typed errors without extra status or metric.

### 5.8 TDD truth for already-correct rule runtime

- Claiming a red test falsifies the record.
- Breaking runtime to manufacture red-green introduces a defect.
- Characterization-first tests, recorded green on baseline, honestly prove runtime before correcting the canonical
  spec.

Recommendation: strengthen mismatch request counting to exactly one exact read plus one mutation; prove the old
`ExecutionContext` is not replayed; record baseline green; then correct the canonical spec. Make no rule runtime change
unless proof exposes a contradiction.

## 6. Target contract

### 6.1 Catalog acquisition

- Graph-ingest remains the only `EnsureCatalogBucket` caller for `ENTITY_STATES`.
- Every R1 reader binds through `OpenCatalogBucket`; Open never creates or reconciles.
- Existing component-specific startup wait lifecycles remain. Each attempt uses Open. R1 adds no shared wait helper and
  does not redesign existing startup budgets.
- A successful handle is immediately stored behind the exact package-local capability below. This is mandatory, not
  discretionary.
- Separate local handles remain allowed where measured nats.go concurrency requires them, including graph-index watcher
  versus Status/LastSeq.
- No source provider, registry, injection mechanism, or singleton is introduced.

| Consumer / local interface | Exact method set | Acquisition and lifetime |
|---|---|---|
| graph-index `entityStateWatchGet` | `WatchAll`, `Get` | One successful Open retained in the current Start wait. |
| graph-index `entityStateStatus` | `Status` | Second independent Open, preserving measured Status/Get concurrency separation. |
| spatial `entityStateWatch` | `WatchAll` | Open during Start; watcher owns cancellation. |
| temporal `entityStateWatch` | `WatchAll` | Open during Start; watcher owns cancellation. |
| embedding `entityStateRead` | `WatchAll`, `Get`, `Status` | Open during Start for watch, repair Get, and last-sequence status. |
| clustering `entitySnapshot` | `Keys`, `Get` | Open in current wait; periodic reads only. |
| clustering `outgoingSnapshot` | `Get` | Open in current wait; periodic reads only. |
| clustering `incomingSnapshot` | `ListKeysFiltered` | Open in current wait; prefix enumeration only. |
| rule `entityPatternWatch` | `Watch` | Zero patterns: no Open. Otherwise retained for required configured patterns. |
| lifecycle `entityListWatch` | `ListKeys`, `Watch` | One successful lazy Open cached for Manager lifetime; failures are not cached. |
| gated-DAG `unitEntityWatch` | `Watch` | One optional Start Open; owned until cancellation. |
| message logger `diagnosticKVQuery` | `Keys`, `Get` | One request-local Open after catalog validation. |
| message logger `diagnosticKVWatch` | `Watch`, `WatchAll` | One request-local Open before SSE headers. |

Only these reader methods are admitted: `Get`, `Watch`, `WatchAll`, `Keys`, `ListKeys`, `ListKeysFiltered`, and
`Status`, with their existing nats.go signatures. No reader interface includes write, delete, purge, history,
revision-fetch, bucket-introspection, or another method.

Lifecycle exact/Get/History stays behind `graph.ExactEntityReader`. Rule's exact point read remains request-local and
narrows to `Get`. `natsclient.BucketLastSeq` narrows its input to anonymous `Status`; `natsclient.FilteredKeys` narrows
to anonymous `ListKeysFiltered`. These add no named exported interface.

For every R1 reader, the concrete `OpenCatalogBucket` result exists only inside acquisition; no reader field,
parameter, or return type is `jetstream.KeyValue`; no generic provider survives; and fakes implement only their local
method set. Broad handles remain only for components that own the bucket and demonstrably call write methods. Every
surviving broad occurrence must be recorded with field/function, catalog owner, and observed write method.

### 6.2 Lifecycle affected-scope poison

Exact validates only its entity. Contract poison returns the existing typed reset-required result, writes no Manager
latch, affects no unrelated operation, and attempts no mutation.

List resolves the workflow, enumerates keys, filters to its workflow pattern, and exact-reads only matches. Malformed
matching state fails typed; malformed nonmatching state is never read. Existing disappearance/non-managed skip behavior
remains.

Watch and WatchEvents open only their workflow pattern. There is no `WatchAll`, complete-authority bootstrap, global
revision barrier, or poison latch. Each matching value is decoded before projection. Malformed matching values emit no
participant/event or mutation, log once with entity/revision, and close only that subscription. Other workflows
continue. Transport closure remains transient and local; normal cancellation is quiet. No status is added.

Lifecycle deliberately stops claiming unrelated authority is globally clean. Owner and derived-view validation remain
independent.

### 6.3 Retry boundary

Rule performs one exact read and one mutation request. Definite mismatch returns visibly. There is no second read,
mutation, helper, or knob; `commit_unknown` is never retried. A future retry requires a separate contract that
reevaluates predicate and intent.

Every lifecycle `Transition`/`TransitionWith` conflict retry exact-reads current authority, re-extracts phase,
revalidates absence/terminality/edge, validates the transition-record chain, recomputes timestamp/records/phase/audit,
reprojects and reruns the optional mutator, then reconciles at the fresh revision. Only definite mismatch retries;
commit-unknown returns. Other lifecycle operations retain their operation-specific policies and are not described as
full transition-intent retries.

## 7. OpenSpec target deltas

### 7.1 Lifecycle

Add requirements that lifecycle MUST NOT open whole-authority admission, complete `ENTITY_STATES` preflight, or retain
a Manager-wide poison latch. Exact/List validate only the requested scope. Touched poison returns
`graph_state_reset_required` and causes no mutation; unrelated poison cannot block the operation.

Watch/WatchEvents decode each match before projection. A malformed match emits no event/mutation, produces one bounded
structured diagnostic naming workflow/entity/revision/code/reason, and closes only that subscription. Other work
continues. No status or per-entity metric is created.

Add transition language requiring every conflict attempt to reread authority and reconstruct/revalidate complete
phase, edge, audit-chain, projection, triples, and optional-mutator intent. A prior delta cannot be replayed at a newer
revision.

Required scenarios: unrelated A does not block B; touched A is typed/non-mutating; matching poison closes only its
subscription; fresh retry invalidation stops stale intent.

### 7.2 Rule projection

Replace “one bounded conflict retry” with one authority read and one mutation attempt. Definite mismatch is visible
without a second request; commit-unknown is not retried; successful receipt retains the commit revision. A newer
revision alone never replays an action from old `ExecutionContext`.

### 7.3 Gated-DAG

Declare the direct `ENTITY_STATES` prefix watch as optional, exact pattern `UnitEntityPrefix + ".>"`, catalog-opened,
and owned until cancellation. It is latency only; periodic reevaluation is correctness. Open/closure warns once and
does not fail Start, add status, or stop periodic reevaluation.

### 7.4 Framework composition and clustering

Add `KVReadPort`, serialized only as `kv-read`, for read/enumeration/snapshot/poll access. It implies no watcher,
replay, injected acquisition, cadence, or shared runtime. Flow composition connects KV-write to KV-read by bucket.

Clustering declares required reads for `ENTITY_STATES`, `OUTGOING_INDEX`, and `INCOMING_INDEX`, opens them through the
catalog, and keeps only local read capabilities. It remains periodic and whole-result based; it claims no watcher and
creates no missing input.

### 7.5 Message-logger diagnostics

Create a focused capability spec: KV query/watch accepts only catalog names and uses `OpenCatalogBucket`; never
ensures, creates, reconciles, writes, or accepts application buckets. Off-catalog is invalid. Owner absence reports
`index_not_ready` and names the owner. Routes are operational diagnostics, not application graph APIs. SemStreams
provides no default mux authorization; products apply middleware.

## 8. Exact file ownership

R1 reserves until merge:

- shared vocabulary: `component/port_kv.go`, `component/port.go`, `component/ports.go`, `component/doc.go`, port tests,
  `component/flowgraph/flowgraph.go`, and coupled flowgraph tests/docs;
- acquisition: graph-index `component.go` and tests; spatial, temporal, embedding, clustering component/tests; rule
  config/watcher/tests;
- clean-break configs: `configs/statistical.json`, `configs/semantic.json`, `configs/semantic-8b.json`, and
  `configs/semantic-frontier.json`, plus a contract test enumerating every checked-in graph-clustering deployment;
- lifecycle: `pkg/lifecycle/manager.go`, `manager_query.go`, `doc.go`, test helper, contract/watch tests, focused
  integration;
- gated-DAG: component, executor, only necessary config, component/executor/integration tests;
- rule proof: `processor/rule/actions_reconcile_test.go`, `pkg/projection/mutation_client_test.go`; runtime files are
  proof surfaces with no expected edit;
- diagnostics: `service/message_logger_http.go`, `message_logger_kv_watch.go`, and focused tests;
- narrow helpers: only the `natsclient.BucketLastSeq` and `natsclient.FilteredKeys` signatures/tests required by
  section 6.1;
- E2E: lifecycle scenario and test-only corruption helper; shared graph-roundtrip probe/client/tests; statistical
  clustering config/declaration assertions; existing tagged clustering entity-watch-scope integration test;
- specs/docs: lifecycle, rule-projection, gated-DAG, framework-composition, graph-clustering, new message-logger
  diagnostics, debugging-data-flow, framework bucket catalog, port/flowgraph docs,
  `docs/concepts/28-governed-semantic-state.md`, `docs/adr/081-graph-view-subscription.md`,
  `processor/graph-ingest/README.md`, `docs/basics/06-configuration.md`, `taskfiles/dev.yml`, and generated schema
  artifacts.

R1 explicitly amends/supersedes ADR-081's lifecycle sticky-reset/`WatchAll` ruling while preserving the rest of that
accepted record. Current rule-retry, clustering-watch, and arbitrary-diagnostic guidance is corrected at the owning
files above rather than layered with contradictory prose.

After R1, graph-index component ownership passes to R3. Query/watermark remain reserved for R3 throughout.

## 9. Atomic contract

Add:

- `component.KVReadPort` / `kv-read`;
- three clustering read declarations;
- one optional gated-DAG prefix-watch declaration;
- message-logger diagnostic capability truth;
- lifecycle affected-scope poison truth;
- structured watch-poison fields.

Replace:

- raw/generic reader acquisition → catalog Open;
- every broad reader handle/signature → the exact local capability in section 6.1;
- clustering watch claim → read claim;
- arbitrary message-logger bucket provider → catalog-only opener;
- lifecycle full-authority admission → touched-scope validation;
- canonical rule retry prose → one-attempt truth.

Delete every lifecycle field/type/function/gate/test fixture listed in the accepted inventory, all R1 raw/generic
acquisition expressions, clustering's false entity-watch declaration, message-logger generic bucket provider and
off-catalog behavior, the canonical “one bounded conflict retry” requirement, every full `jetstream.KeyValue` reader
field/signature, and any `StoreReadPort` mode/KV matching introduced during implementation.

Do not delete owner validation, derived-owner watchers/readiness, rule-local watch latches, touched-entity typed error,
lifecycle transition retry, gated-DAG periodic reevaluation, R6 query surfaces, E2E storage probes, or the existing WARN
counter, legitimate owner/write handles, or existing `StoreReadPort` federation behavior.

Runtime, tests, specs, generated schema, and operator documentation land together. No dual port, bucket scope, poison
policy, retry policy, raw/catalog acquisition, shim, deprecated alias, compatibility flag, or migration helper survives.

## 10. TDD sequence

1. Pin existing `StoreReadPort` federation, then add failing `kv-read` canonical/no-alias, ResourceID, pattern,
   same-bucket match, different-bucket/store nonmatch, orphan, and three-clustering-input tests. Convert all four
   checked-in graph-clustering configs to unsupported `kv-read` and record RED before parser/runtime support. A
   contract test must fail if another checked-in clustering deployment retains `entity_watch`/`kv-watch`.
2. Add failing catalog Open, absent-owner/no-create, owner-name, zero-pattern rule, exact interface, no broad reader
   field/signature, owner/write allowlist, and graph-index dual-Open tests. Then replace raw/generic acquisition and
   broad reader types.
3. Replace inverse lifecycle tests with failing A/B continuity, touched-poison mutation count, two-subscription local
   closure, structured-log, and zero-`WatchAll` tests. Add the production-wire lifecycle stage and record RED before
   deleting the guard/latch.
4. Add failing gated-DAG port, exact-prefix, warning/periodic-continuity, and cancellation tests. Implement only the
   declaration and local observability.
5. Add lifecycle fresh-authority retry characterization for phase, edge, audit chain, projection, and per-attempt
   mutator. Failure blocks the spec claim.
6. Add rule request-count and stale-context characterization; record baseline green if current runtime passes; make no
   runtime edit unless the premise is falsified.
7. Add failing service and graph-roundtrip assertions for catalog allow, off-catalog 400/no-create, absent-owner
   503/owner/no-create, and Open-before-SSE. Record core RED before implementation.
8. Apply accepted specs/docs, generate schemas, inspect changes, run exact deletion/scope searches, and measure the
   production delta.

## 11. Breaking E2E and verification budget

R1 is **BREAKING unconditionally**: clustering configuration changes from `kv-watch` to `kv-read`, and off-catalog
message-logger callers are rejected with no compatibility spelling or route.

### 11.1 Lifecycle production-wire stage

Extend the existing lifecycle scenario after its transition/history proof. Open and bootstrap the production mission
WebSocket; use a test-only NATS corruption helper to put malformed bytes under nonmatching canonical key
`c360.test.poison.gcs.device.p001` in the existing `ENTITY_STATES` bucket without ensuring/creating storage; then use
the production gateway to list mission, GET valid mission B, and apply a unique operator patch to B.

Assert list/exact/mutation success, WebSocket continuity and receipt of B's patch before its bounded deadline, and no
event for A. Record assertion count and duration. This stage is RED today because the hidden lifecycle `WatchAll`
latches poison from unrelated A. Focused tests still own matching-poison closure and exact log fields.

### 11.2 Shared graph-roundtrip diagnostics

Extend the shared `GraphRoundTripProbe`, which runs in core and every tiered variant:

1. Query `ENTITY_STATES` for its entity; assert HTTP 200 and exact evidence.
2. For fixed off-catalog name `R1_E2E_NOT_CATALOG`, prove direct NATS absence, HTTP 400, then continued absence.
3. In core only, prove `EMBEDDING_INDEX` is absent because graph-embedding is undeployed; assert HTTP 503,
   `index_not_ready`, message naming `graph-embedding`, and absence after the call.
4. In statistical, repeat catalog allow and off-catalog/no-create, but do not expect embedding absence.

The client must expose negative status/body rather than collapsing every non-200 into a generic error. Record core RED
on current generic/404 behavior before implementation.

### 11.3 Clustering and gated-DAG

Statistical E2E proves the deployment accepts `kv-read`, clustering boots with three visible same-bucket producers,
flowgraph exposes the three read declarations/edges, community detection retains its assertion-counted result, and
graph-roundtrip diagnostics pass. No-authority-watcher runtime proof belongs to the existing build-tagged
`processor/graph-clustering/entity_watch_scope_integration_test.go`, where the observation is attributable to
clustering rather than other stack watchers.

No gated-DAG E2E deployment or isolated watch-loss seam exists. Before the breaking commit, file and link a coverage
gap titled `test(e2e): prove gated-DAG periodic continuity after optional unit-watch loss`, owned by the gated-DAG
capability with target `task e2e:structural`. Acceptance must deploy a fixture, induce only Open/closure loss without a
production fault hook, observe one warning, prove Start, change eligibility, and prove dispatch within two periodic
intervals using polling/deadlines. The issue records a gap; it does not weaken focused tests.

### 11.4 Required runs

Final focused proof:

```bash
go test -race ./component ./component/flowgraph
go test -race ./graph ./natsclient
go test -race ./processor/graph-index
go test -race ./processor/graph-index-spatial
go test -race ./processor/graph-index-temporal
go test -race ./processor/graph-embedding
go test -race ./processor/graph-clustering
go test -race -tags=integration ./processor/graph-clustering -run TestIntegration_ClusteringHoldsNoEntityStatesWatcher
go test -race ./processor/rule ./pkg/projection
go test -race ./pkg/lifecycle
go test -race ./processor/gated-dag
go test -race ./service
go test -race ./test/e2e/scenarios/...
go test ./test/contract/...
task schema:generate
git diff --check
git diff -- schemas/ specs/
task check:push
task e2e:core
task e2e:lifecycle
task e2e:statistical
```

Do not run structural, semantic, agentic, research, or the full ladder unless a focused failure proves a direct R1
dependency. Before implementation, the baton records lifecycle RED, core diagnostic RED, statistical `kv-read` RED,
rule baseline characterization, and the gated-DAG issue identity. All three listed E2E tiers must be green before the
breaking commit lands.

### 11.5 Broad-handle census

Run a targeted `jetstream.KeyValue` census across every R1 reader package. Classify each result as approved owner/write
handle with exact owner/write method, test fixture, acquisition-local temporary with no field/signature escape, or
blocking reader escape. The last category must be zero. Add an AST/contract test rejecting broad fields and function
parameter/return types on exact R1 reader seams; grep remains the review ledger.

## 12. Falsifiable completion

R1 completes only when:

1. exactly one production Ensure of `ENTITY_STATES` remains: graph-ingest;
2. every R1 availability attempt uses catalog Open and no raw/generic acquisition remains;
3. no full `jetstream.KeyValue` reader field or reader function signature survives;
4. every broad surviving handle is proved owner/write or test-only;
5. graph-index retains separate `WatchAll/Get` and `Status` capabilities from separate Opens;
6. spatial/temporal retain only `WatchAll`; embedding only `WatchAll/Get/Status`;
7. clustering retains exact `Keys/Get`, `Get`, and `ListKeysFiltered` capabilities for its three inputs;
8. rule/lifecycle/gated-DAG/message-logger retain only the exact section 6.1 method sets;
9. absent input remains absent and reports not-ready naming its owner;
10. `StoreReadPort` semantics/matching remain unchanged, and store/KV ports never cross-match;
11. `kv-read` has one canonical spelling and same-bucket writer/read matching only;
12. all four checked-in clustering configs declare three required `kv-read` inputs with no old watch declaration;
13. tagged clustering integration proves no authority watcher, while clustering remains periodic;
14. zero-pattern rule opens no authority/watch;
15. gated-DAG declares one optional exact prefix watch, owns it to cancellation, and continues periodic work on loss;
16. lifecycle contains no `WatchAll`, global preflight/latch, or accepted guard identifier;
17. malformed A outside B cannot block B exact/List/Watch/WatchEvents/transition;
18. touched A is typed and sends no mutation;
19. matching watch poison emits no participant/mutation, warns once with entity/revision, and closes only its subscription;
20. no lifecycle status or metric is added; existing WARN counter observes without entity/revision labels;
21. rule mismatch is one exact read and one mutation, and old `ExecutionContext` is not replayed;
22. lifecycle retry rereads/revalidates phase, edge, audit chain, projection, and mutator;
23. no shared CAS helper/knob exists;
24. message logger allows catalog buckets, rejects off-catalog 400/no-create, and reports absent owner 503/no-create;
25. watch Open precedes SSE headers, and docs do not claim framework authentication;
26. CI-equivalent `task check:push` passes;
27. lifecycle, core, and statistical RED evidence is recorded before implementation;
28. lifecycle, core, and statistical E2E are green before the breaking commit;
29. the gated-DAG structural coverage-gap issue is filed, owned, and linked;
30. retired identifiers have zero current references outside immutable history;
31. no source injector, shared authority view, recovery/CQRS runtime, compatibility alias, or dual path appears;
32. authored production code is net-negative excluding generated artifacts.

## 13. Adopter outcome

Component authors see truthful `kv-read` versus `kv-watch` metadata and typed owner-not-ready errors; they need no raw
bucket mechanics or creation decisions. Lifecycle adopters see errors only for touched state; watch poison is local and
observable, unrelated work continues, and there is no global reset status. Rule authors get one stable one-attempt
contract with visible mismatch and no retry knobs. Message-logger operators get read-only catalog diagnostics, honest
owner absence, immediate off-catalog rejection, and an explicit product-middleware boundary.

## 14. Complexity and rollback

- new buckets/streams/services/status/metrics/query or mutation operations/shared runtime: 0;
- new exported metadata symbols: 1 (`component.KVReadPort`);
- new serialized port tokens: 1 (`kv-read`);
- new serialized flowgraph pattern values: 1 (`kv-read`), with no new exported constant required;
- removed coordination mechanisms: 1 Manager-wide guard/latch;
- route count: unchanged; message-logger scope narrows;
- `StoreReadPort` semantics and matching: unchanged;
- clustering vocabulary cleanly replaces `kv-watch` with `kv-read`;
- authored production code must be measured net-negative.

Risks: lifecycle no longer reports unrelated poison it never touches; off-catalog diagnostic users break; `KVReadPort`
is a new outward concept; startup ordering remains component-local; gated-DAG watcher loss raises latency; warning
emission must terminate after one record.

Before merge, revert the complete R1 change. After merge, forward-fix; do not restore raw acquisition, global poison,
false watch metadata, arbitrary diagnostic buckets, or retry prose through compatibility. NATS graph-state and
mutation data formats do not change; configuration/schema and flowgraph vocabulary do change. Binary/config rollout is
coordinated; no dual schema support is added.

## 15. Amendment status

Semantic target amendment: **none**.

Program-record corrections requiring owner acceptance:

1. Reserve graph-index `component.go` to R1 before R3.
2. Add gated-DAG, framework-composition, graph-clustering, message-logger diagnostics, all four clustering configs,
   ADR-081 disposition, current rule/clustering/diagnostic docs, and exact E2E files to R1 owning truth.
3. Record the reviewed StoreRead/KVRead collision ruling and distinct `component.KVReadPort` resolution.
4. Record mandatory reader method sets and the no-broad-handle census.
5. Record “operator-only” as capability scope, not framework authentication.
6. Record R1 as unconditionally BREAKING with core, lifecycle, and statistical as relevant green tiers.
7. Record the gated-DAG coverage-gap issue as a pre-commit requirement.
8. Record characterization-first TDD for already-correct rule runtime.

Because roadmap approval is content-addressed, these reservation/ownership corrections require a new reviewed roadmap
identity and explicit owner acceptance. They cannot be edited implicitly during implementation.
