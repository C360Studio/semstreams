# GS-01 revision-36 inventory-only scope audit

> **INVENTORY ONLY.** This artifact records current repository facts, collisions, adopter costs, issue evidence, and
> scope conflicts. It contains no options, recommendation, future contract, specification delta, task plan, or
> implementation direction. Binding rulings remain with the owner.

## Audit identity

- Worktree inspected: `/private/tmp/semstreams-gs00`
- Branch: `codex/gs01-authority-recovery`
- Evidence baseline: `cb09133e0154296664343c5a5d0723b294cbfd5f`
- Audit date: 2026-08-05
- Binding session ruling: owner correction supplied 2026-08-05
- Prior reviewed evidence: `reviewed-fifth-pass-inventory.md` (`INVENTORY PASS`),
  `reviewed-recovery-contract-r35.md` (`DESIGN REVIEW PASS`, later owner-rejected),
  `suffix-inventory-addendum.md`, and `suffix-inventory-review.md`

The shared worktree contained parent-session modifications and an untracked earlier r36 draft while this audit was
prepared. This artifact is read-only with respect to that worktree and treats the commit above as its evidence
baseline.

## Surface inventory

### 1. Claimed gap and corrected problem boundary

The durable program describes an offline-first graph foundation whose priorities are predictable results, local edge
operation, one easy default, and easy comprehension. `ENTITY_STATES` is current shared authority, not an event ledger;
the read model separates admitted front door from answer-source semantics.

Evidence: `docs/proposals/graph-state-read-write-program.md:29-53,126-152`.

The binding owner correction for this audit states:

- SemStreams does not own an operational disaster-recovery or checkpoint product.
- NATS clusters remain supported; there is no single-node constraint.
- Edge/offline operators maintain ordinary infrastructure backups/checkpoints; SemStreams documents that responsibility.
- “Recovery” in this program means architectural recovery from drift toward a predictable graph foundation.

That ruling is not yet expressed consistently in durable repository authority. Four current claim sets conflict:

| Claim source | Current claim | Conflict exposed by the owner correction |
|---|---|---|
| ADR-032 | Production durability is JetStream cluster replication, R>=3; snapshots are optional compliance PITR, not primary durability; the cluster/runbook work is documentation, not framework code. | This agrees with no SemStreams DR product and rejects revision 35's single-node premise. |
| ADR-090 | Authority recovery uses snapshot/restore of `ENTITY_STATES`, referenced ObjectStore content, and coordinated ingest-guard state; authority restore is a distinct runbook. | This still assigns physical authority recovery to SemStreams. |
| Program | The authority recovery unit is current state plus declared content/guard state, and GS-01 requires a coordinated snapshot/restore runbook. | The physical recovery gate is stale under the binding owner boundary. |
| Owner ruling | Infrastructure operators own backup/checkpoint; program recovery is recovery from architectural drift. | This is binding for the audit but has no canonical repository citation yet. |

Evidence: `docs/adr/032-policy-tenancy-cluster.md:117-123,183-200,333-367,389-416`;
`docs/adr/090-authoritative-current-state-and-materialized-views.md:14-27,34-48,57-68`;
`docs/proposals/graph-state-read-write-program.md:47-53,469-477,507-529`.

The exact phrase `recovery from drift` has no baseline repository match. The exact search is recorded under Closing
searches. The durable absence is a documentation conflict, not evidence against the binding owner ruling.

Revision 35 displaced the graph foundation with an offline physical checkpoint product: recovery binary/package,
control KV, maintenance-provider seam, closed single-node maintenance NATS, checkpoint identities, native snapshot
protocol, signed evidence, and startup fences. It also restricted the value-plus-revision reader to recovery inspection
and moved graph-ingest exclusivity to GS-02.

Evidence: `openspec/changes/establish-authority-read-and-recovery/reviewed-recovery-contract-r35.md:31-56,112-159,
249-454,773-792,1214-1326,1431-1580,1800-1990,2036-2233`.

Under the binding owner correction, those r35 mechanisms are correction evidence rather than an operational product
surface.

### 2. Current cluster, replicas, and operator guidance

Cluster operation is already a valid topology:

- The NATS client accepts comma-separated server URLs explicitly for clustering and defaults to infinite reconnects
  with a two-second reconnect wait: `natsclient/client.go:67-92,151-190,361-377`.
- Config accepts multiple `nats.urls`, reconnect controls, TLS, and environment URL lists:
  `config/config.go:169-179,485-498,742-745`; `config/README.md:45-65`.
- README promises edge-to-cluster operation: `README.md:11-16`.
- ADR-032 accepts single node for development/demo and says production durability uses a 3+ node JetStream cluster
  with R=3: `docs/adr/032-policy-tenancy-cluster.md:333-346`.

Current replica declarations are not one coherent production policy:

- `BucketSpec` carries `Replicas`; the graph catalog helper defaults every owned bucket, including `ENTITY_STATES`, to
  1: `natsclient/kvspec.go:105-120,200-212`; `graph/kvcatalog.go:50-74`.
- `EnsureFrameworkBucket` uses the descriptor for create, then reconciles retention and History; its documented
  acquisition contract does not say it reconciles replicas: `natsclient/kvspec.go:215-220,286-294`.
- Ordinary stream config exposes replicas but defaults non-positive values to 1; built-in LOGS, HEALTH, METRICS,
  FLOWS, and GOVERNANCE_VERDICT_AUDIT declarations are 1:
  `config/streams.go:28-45,108-181,238-251,495-510`.
- Drift repair deliberately preserves an operator's observed replica count:
  `config/stream_drift.go:184-194`; `config/stream_drift_test.go:139-151`.
- `JetStreamConfig.ReplicationFactor` is reserved and not honored:
  `config/config.go:189-219`.
- The agent-stream guide is one present operator example that says production replicas 3 and development 1:
  `docs/advanced/08-agentic-components.md:557-578`.
- The promised general JetStream cluster/reconnect/replication runbook remains listed as pending in migration docs:
  `docs/operations/migration-beta31-to-beta32.md:166-178` and
  `docs/operations/migration-beta32-to-beta33.md:103-113`.

No current code fact imposes the r35 single-node topology as the framework boundary. Current defaults do, however,
make a first creator use replicas 1 unless the operator pre-provisions or another declaration governs the resource.

### 3. Authority identity and revision spellings

`ENTITY_STATES` is cataloged as authoritative, graph-ingest-owned, owner-created, owner-written, History 1, and
Replicas 1. Readers are supposed to bind must-exist and never create or reconcile it.

Evidence: `graph/kvcatalog.go:50-74,247-274`; `natsclient/kvspec.go:297-320`.

`graph.EntityState.Version` is logical entity metadata. NATS KV entry revision is separate transport/storage metadata;
the stored `EntityState` has no KV-revision field.

Evidence: `graph/types.go:24-47`; `processor/graph-ingest/query.go:87-105`.

The broad claim that no value-plus-revision authority read exists is false. The agentic `query_entity` tool reads one
KV entry, canonically validates its value, returns that value as content, and returns the same entry's revision in tool
metadata. It is a model tool backed by direct KV, not the admitted remote/embedded graph operation sought by #851.

Evidence: `processor/agentic-tools/executors/register_graph_query.go:49-104`;
`processor/agentic-tools/executors/graph_query.go:14-24,170-221`.

### 4. Repository-first authority reader census

The following table starts from production bucket acquisition and then follows every direct `Get`, key enumeration,
`Watch`, `WatchAll`, and `History` site. Mediated public/embedded reads and diagnostic harnesses are included after the
direct owners because they are adopter-visible result contracts even though they do not acquire the bucket themselves.

| Consumer and read sites | Result | Revision semantics | Validation and absence | Lifecycle/status behavior | Recovery-from-drift behavior |
|---|---|---|---|---|---|
| **graph-ingest owner** — creates/opens at `component.go:1230-1243`; boot `WatchAll` at `:1306-1345`; owner-local reads at `component.go:557,577,1907,2517,2832,3517`, `poison_inventory.go:161,185,248`, `mutations.go:422,571,1307`, and `query.go:87,279,287,554,661`. | Exact, batch, prefix, suffix/query results plus internal existence, poison, merge, CAS, and ingest checks. Exact RPC returns raw `EntityState`. | Exact handler receives entry revision for poison evidence but returns only value. Mutation/RMW paths use revisions internally. | Canonical stored-value decode; exact absence is typed `entity_not_found`; poison is inventoried per entity. | Boot sweep gates reads fail-closed while writer remains available; no steady self-watch after sweep. `GRAPH_STATUS` reports read readiness. | Current owner-local recovery is poison repair/re-read, mutation retry, and reseed/cutover procedure; no SemStreams physical DR product. |
| **`graph/query.Client`** — catalog bind `graph/query/client.go:147-177`; `WatchAll` `:179-253`; `Get` `:281-327`; `Keys` `:357-372`; higher queries call `GetEntity`/`ListEntities` throughout `:602-1021`. | Cached raw `*EntityState`, batches, lists, and aggregate graph queries. | Watch revisions invalidate cache and track observation; returned entities omit revision. | Canonical decode; KV absence becomes generic get failure; poison is whole-client sticky. | Requires unrelated spatial and incoming buckets. Watch loss is classified transient but permanent for that client instance. | Caller must construct a new client after watch loss; poison requires reset/reingest. |
| **agentic graph-query tools** — lazy catalog bind and `Get` adapter `register_graph_query.go:49-104`; tool reads `graph_query.go:170-221,224-311,314-390,393-488`. | `query_entity`, batch, relationships, neighbors, by-type tool results. | `query_entity` alone exposes same-entry revision in metadata; other tool results omit per-entity revisions. | Canonical authority decode; exact/batch not-found are tool outcomes; some neighbor fetch failures are skipped. | Registers before bucket exists, retries lazy must-exist bind, never creates. No graph readiness contract. | Tool failure is not a projection repair contract; later execution retries bucket bind/read. |
| **lifecycle Manager** — lazy direct bind `pkg/lifecycle/manager.go:413-456`; `Get`, `GetWithRevision`, `GetRaw` `:483-552`; List/key scan `manager_query.go:25-95`; pattern `Watch` and guard `WatchAll` `:134-160,210-325`; `History` `:515-605`. | Raw entity, projected `Participant`, lists/events, and projected transition history. | `GetWithRevision` returns current authority revision with a projection; `GetRaw` omits it; watches use revisions as a validation barrier; History exposes entry time, not a current raw value+revision result. | Canonical decode; distinguishes missing entity from present non-lifecycle entity. Poison latches Manager-wide. | Pattern watch plus full-authority guard; callers must cancel watches. `History` claims audit reconstruction over History-1 authority. | Watch/guard failure blocks lifecycle access; poison requires reset/reingest. The H1 audit claim is contradicted by ADR-090 and live issues. |
| **rule processor** — bounded variable-name bind `processor/rule/entity_watcher.go:86-139`; pattern Watch `:225-237`; decode/dispatch `:663-704`; exact fetch `:990-1027`. | Rule snapshots with state, CRUD action, and revision. | Watch/fetch exposes entry revision; current absence synthesizes `DELETED` revision 0, not an observed tombstone. | Canonical decode; pattern validation is ENTITY_STATES-specific. | Dynamic watcher generations; guard degradation/poison blocks rule evaluation. Rule also publishes a separate readiness key. | Rebinding patterns is local lifecycle; poison/reset-required is graph recovery evidence. |
| **gated-DAG executor** — catalog bind and raw prefix Watch `processor/gated-dag/executor.go:139-153,361-392`. | No authority value result; any unit-key write nudges reevaluation. | Revision ignored. | Values are not decoded; watcher-start failure degrades to polling backstop. | Best-effort latency accelerator beside lifecycle Watch. | Claim recovery is reset-driven; #689 records missing CAS/ambiguity semantics. |
| **agent-run triple reader** — direct bucket bind/Get `agentic/agentrun/nats_reader.go:77-138`. | One string predicate (`run ID` or parent ID). | Omitted. | Canonical decode; missing entity and missing predicate both become `(empty,false,nil)`; wrong type errors. | Lazy read per call; no watch/readiness surface. | Retry is caller-owned; no authority repair contract. |
| **graph-index core** — direct wait/open and double handle `processor/graph-index/component.go:944-981`; `WatchAll` `:848-940`; reconcile `Get` `:1181-1251`; predicate-value hydration `processor/graph-index/query.go:438-480`. | Derived index maintenance and predicate-filter query IDs. | Watch revisions feed watermark/readiness; reconcile Get revision is not returned by predicate query. | Canonical decode; identity mismatch/reset-required is sticky; authoritative absence deletes derived entries. | Bootstrap target, watermark, failed-key repair, and `GRAPH_STATUS`. | Reconciliation re-reads authority inside ordered lanes and converges derived indexes; this is architectural drift recovery. |
| **spatial index** — wait/open `processor/graph-index-spatial/component.go:404-440`; `WatchAll` and apply `:579-725`. | Spatial materialization; no direct authority result. | Entry revision is not part of the spatial record/result contract shown here. | Canonical bootstrap/live decode; tombstones delete; poison latches reset-required. | Query fails closed on poison, watcher loss, or incomplete bootstrap. | Replayed current snapshot rebuilds/converges the spatial view. |
| **temporal index** — wait/open `processor/graph-index-temporal/component.go:414-449`; `WatchAll` and apply `:599-745`. | Temporal plus reverse materialization; no direct authority result. | Entry revision is not exposed by the temporal query result here. | Canonical decode; tombstones delete; poison latches reset-required. | Query fails closed on poison, watcher loss, or incomplete bootstrap. | Current snapshot/live watch converges temporal and reverse state. |
| **graph embedding** — wait/open `processor/graph-embedding/component.go:1200-1235`; `WatchAll` `:1423-1541`; reconcile Get/requeue `:1594-1662`. | Embedding work and derived records. | Watch/Get revision is carried as source revision into watermark/work; not an authority read result. | Canonical decode; absence deletes derived vector; read failure strands work and withholds readiness. | Bootstrap/watcher/readiness gates plus repair/coalescing. | Reconcile reads authority inside the hop-1 seam and converges or deletes the derived record. |
| **graph clustering** — generic must-exist waits for authority/outgoing/incoming `processor/graph-clustering/component.go:1110-1171`; entity IDs via `Keys` `:1854-1864`; hydration Gets `:2386-2445`. | Entity ID census and hydrated `*EntityState` batch for clustering. | Revisions omitted. | Missing entities are skipped; canonical poison latches and fails the batch; no live authority watcher. | Polls on detection cycles and depends on three buckets. | A later cycle re-enumerates/rehydrates; poison is reset-required, not silently skipped. |
| **operator triple HTTP endpoint** — catalog bind, Keys/Get scan `service/graph_triples_http.go:103-221`. | Filtered `[]message.Triple`; low-throughput operator/e2e/dashboard surface. | Omitted. | Canonical decode; key-disappeared race is skipped; nil NATS returns empty; backend errors become HTTP 500. | Per-request full scan; no readiness or cache contract. | Subsequent request re-scans current authority; it does not repair state. |
| **message-logger raw diagnostics** — caller-selected must-exist bucket Get/Keys `service/message_logger_http.go:459-556`; generic Watch/WatchAll `service/message_logger_kv_watch.go:195-290`. | Raw/JSON values with key, revision, creation time; SSE events with operation/revision/value. | Same-entry revision is exposed diagnostically. | No authority canonical validation; non-JSON becomes string; per-key Get failures are skipped. | Operator diagnostic HTTP/SSE; caller chooses bucket name and pattern. | Observation only; reconnect/retry behavior belongs to the diagnostic client/session. |
| **E2E validation client (diagnostic/test only)** — direct Count/Get/sample/list/provenance reads `test/e2e/client/nats.go:216-258,389-454,960-1010`; message-logger SSE usages `:1097,1154,1398`. | Counts, raw test `EntityState`, samples/IDs, bounded predicate evidence, watch events. | Direct helpers omit revision; message-logger SSE includes it. | Plain JSON decode, with several missing/error cases degraded or skipped for test diagnostics. | Harness-only, not an adopter contract. | Used to measure current state and convergence; does not own repair. |

Direct production bucket acquisitions are therefore not confined to graph-ingest. The direct-reader set includes
`graph/query`, lifecycle, agent-run, agentic tools, graph-index, spatial, temporal, embedding, clustering, rule,
gated-DAG, and service/operator paths. Message logger adds a generic raw diagnostic path. The separate mediated census
below follows every production match for the four internal entity-query subjects, the four named public query
subjects, and `ReadAuthoritative` through its consumer result contract.

### 5. Complete mediated authority-reader census

This census begins at the two responder layers and follows every statically named production caller returned by the
closing exact-subject and `ReadAuthoritative` searches. A row is a distinct consumer/result contract; shared subject
constants and routing tables are cited with the concrete call sites that consume them.

| Mediated consumer and call sites | Subject/provider path | Result | Revision semantics | Validation and absence | Lifecycle/status behavior | Architectural recovery from drift |
|---|---|---|---|---|---|---|
| **graph-ingest exact provider** — `processor/graph-ingest/query.go:27,60-105`. | Responds on `graph.ingest.query.entity` from the authority bucket. | Raw canonical `EntityState` JSON. | Entry revision is used only to inventory/clear poison; reply omits it. | Empty/malformed ID is invalid; missing is typed `entity_not_found`; backend failure is transient/internal; stored bytes are canonically validated. | Every request fails closed while bootstrap is incomplete or the authority watch is lost; five-second handler timeout. | A later request re-reads current authority; a valid out-of-band repair clears stale poison evidence on read. |
| **graph-ingest batch provider** — `processor/graph-ingest/query.go:34,107-159,575-691`; wire contract `graph/query_batch_types.go:3-55`. | Responds on `graph.ingest.query.batch`. | `EntityBatchResponse{entities,missing}`; ordering is not promised. | Revisions are used internally to validate/cache each value and omitted from the reply. | Empty input returns empty; invalid and not-found IDs are reported in the closed `missing.reason` set; a non-absence backend or poison failure fails the batch. Missing is explicitly not proof of authoritative absence. | Ten-second handler timeout; same readiness gate as exact; bounded concurrent fetch. | A later batch re-reads uncached current entries; returned accounting lets callers notice omissions, but the provider does not repair a caller projection. |
| **graph-ingest prefix provider** — `processor/graph-ingest/query.go:41,243-370`; wire contract `graph/query_prefix_types.go:7-79`. | Responds on `graph.ingest.query.prefix`. | Sorted page of full `EntityState` values plus opaque keyset cursor. | Entry revisions validate/cache values but are omitted. | Canonical prefix grammar; disappearing keys during page hydration are honestly omitted; poison/backend error fails; byte-budget and caller limit can truncate with `next_cursor`. | Ten-second handler timeout and readiness gate; default/max page limit 1000. | Each page re-enumerates/reads current authority. Cursor paging is bounded observation, not snapshot isolation or repair. |
| **graph-ingest suffix provider** — subscription `processor/graph-ingest/query.go:48`; handler/index/scan `:420-572`. | Responds on `graph.ingest.query.suffix`; TTL cache, then suffix KV index, then full key scan. | `{"id":"<match>"}` or `{"id":""}`; first matching ID only. | Omitted; neither the suffix index nor fallback key scan returns an authority entry revision. | No authority entity-value validation occurs on an index hit or key scan; absence is an empty ID; malformed/empty suffix is invalid; index/scan errors are transient/internal. | Readiness gate and bounded handler context; cache/index state is component-local/derived. | A scan miss/hit can lazily refill cache/index, but trusted stale index hits and first-match ambiguity remain suffix-slice drift evidence. |
| **graph-query exact and alias providers** — subscriptions `processor/graph-query/query.go:31-32`; handlers `:68-171`; router `processor/graph-query/router.go:17-22`. | `graph.query.entity` and `graph.query.entityByAlias` route to internal exact; alias lookup is best effort before ID fallback. | Canonically validated raw `EntityState`. | Omitted by the internal exact reply and therefore by both public replies. | Exact requires nonempty ID. Alias failure falls back to treating the input as an ID. Internal classified absence/failure propagates; response is canonically validated again. | Public responder exists only while graph-query is started; each request has the component query timeout. | A later request repeats alias resolution and current authority read; no stored projection is repaired. |
| **graph-query batch provider** — `processor/graph-query/query.go:33,174-226`; router `processor/graph-query/router.go:17-22`. | `graph.query.batch` forwards to `graph.ingest.query.batch`. | Validated `EntityBatchResponse`, including partial `missing` accounting. | Omitted for every returned entity. | Returned entities are canonically validated; internal errors propagate. Comment records that large batches may exceed NATS 1 MiB and callers should chunk. | Per-request mediation; no independent readiness state beyond downstream response. | A later call rehydrates current authority; partial/missing evidence is preserved for caller reconciliation. |
| **graph-query prefix provider** — `processor/graph-query/query.go:37,229-300`; router `processor/graph-query/router.go:17-22`. | `graph.query.prefix` forwards one typed page to `graph.ingest.query.prefix`. | Validated `PrefixQueryResponse` with values and cursor. | Omitted for every returned entity; the cursor is not a revision fence. | Prefix/request and returned canonical entities are validated; classified downstream failures propagate. | Per-request mediation under the component query timeout; no aggregate paging lifecycle. | The caller owns cursor continuation; each page observes current authority and supplies no snapshot/revision fence. |
| **graph-query summary provider** — `processor/graph-query/query.go:45`; `processor/graph-query/summary.go:40-124`; contract `graph/query_summary_types.go:20-100`. | `graph.query.summary` composes internal prefix sampling and predicate-list query. | `QueryResponse[SummaryData]`: totals by type, examples, optional predicates, and truncation flag. | Omitted; neither sample entries nor aggregate response carry authority revisions. | Prefix failure is hard; predicate failure yields an omitted/partial predicate facet; `entity_sample_truncated` conservatively marks a sample that reaches its limit. | One request fans into two responders; default entity sample limit is 2000. | A later summary resamples current authority/index state; the truncation marker exposes bounded approximation but does not reconcile it. |
| **graph-query suffix resolver** — `processor/graph-query/entity_resolver.go:11-115`; used by GraphRAG at `processor/graph-query/graphrag.go:355,880`. | Full IDs pass through; otherwise alias is attempted, then `graph.ingest.query.suffix`. | Resolved canonical ID string or empty/no match. | Omitted. | Any suffix request error, malformed reply, or empty result degrades to no match; suffix provider does not validate the matched entity value. | Two-second suffix request; invoked during each GraphRAG resolution. | Later GraphRAG calls retry resolution, but transient authority/index failure is indistinguishable from absence at this seam. |
| **graph-query PathRAG existence check** — `processor/graph-query/pathrag.go:110,205-249`. | Direct `graph.ingest.query.entity` request before path expansion. | Boolean existence decision; response value is discarded. | Omitted with the discarded value. | Invalid/not-found means absent; transport/transient/fatal errors remain errors. Canonical validation is delegated to graph-ingest. | Per search; bounded by PathRAG timeout; no cached result. | Each invocation rechecks current authority; the check prevents expansion from a presently absent start node but performs no repair. |
| **gated-DAG unit-set reader** — `processor/gated-dag/reader.go:13-20,31-123`; caller `executor.go:224`. | Paginates direct `graph.ingest.query.prefix` for the configured DAG unit prefix. | Fresh `[]EntityState`; at `maxUnits`, returns a partial slice with nil error and emits truncation warning. | Omitted for every unit; cursor does not fence revisions across pages. | Every page is JSON-decoded and canonically validated; downstream classified failures propagate; no absence item exists for enumeration. | Cold first page uses readiness probes up to a budget and calls `onNeverReady` if exhausted; later pages/reads are steady classified requests. `warmed` is set only after all pages validate. | Every evaluation re-reads the whole bounded unit set; the raw watcher merely nudges reevaluation. Page-cap warning exposes drift/coverage loss but does not repair claims. |
| **agentic-loop todo reader** — `processor/agentic-loop/todos.go:19-29,47-108`. | Direct `graph.ingest.query.entity` for the loop entity. | Ordered reconstructed `[]TodoState` projected from five predicate groups. | Omitted. | Canonical entity decode; classified invalid/not-found becomes an empty list; transient/transport error propagates from the reader and the enclosing iteration may omit the todo block. | Two-second per-iteration request; no watch/cache; the next iteration retries. | A later iteration reconstructs from current authority; one-iteration omission is intentional availability behavior, not projection repair. |
| **agentic-loop lesson reader** — `processor/agentic-loop/lessons.go:17-31,33-118`. | Paginates direct `graph.ingest.query.prefix` for lesson records. | Matcher projection for every returned lesson, including status. | Omitted for every lesson. | Empty graph is empty success; request/decode failures error; canonical validation is delegated to graph-ingest; zero/missing predicates remain zero values. | Three-second request per page, no query retry, maximum 16 pages; a remaining cursor logs warning and returns partial success. | Every dispatch re-lists current lessons. The cap makes incomplete brief coverage observable but leaves recovery to a later dispatch/configuration. |
| **agentic-loop origin verification** — `processor/agentic-loop/graph_writer.go:227-288`. | After `create_with_triples` reports `entity_exists`, direct `graph.ingest.query.entity` reads the existing entity. | Idempotent-birth decision plus divergent-task warning evidence. | Omitted; verification compares semantic value only. | Canonical decode and exact `MessageType` comparison; unreadable/missing/mismatched origin fails birth. Same type succeeds; differing task ID warns and preserves first identity. | Runs only on create ambiguity/entity-exists path; bounded classified request. | Read-back resolves whether an apparent duplicate is a safe idempotent birth; it deliberately does not rewrite divergent existing identity. |
| **agentic-tools lesson-status reader** — `processor/agentic-tools/emit_lesson.go:188-208,239-267`. | Direct `graph.ingest.query.entity`. | `(status,found,error)`; a present entity without status returns `("",true,nil)`. | Omitted. | Canonical decode; typed `entity_not_found` is stable absence; other handler/transport failures propagate. | Five-second, non-retrying query used for lesson state checks. | A later tool action can re-read current status; this seam detects state but does not reconcile it. |
| **agentic-tools graph summary** — registration `processor/agentic-tools/executors/register_summarize_graph.go:11-26`; execution `summarize_graph.go:41-90,149-203`. | Calls public `graph.query.summary`. | LLM-formatted summary plus metadata `total_entities`, `entity_sample_truncated`, and `predicate_total`. | Omitted by the provider and tool metadata. | Success envelope is decoded; classified handler failure becomes external tool error, transport becomes network tool error. Approximation/partial predicate semantics come from summary provider. | Ten-second read-only tool execution; no graph identity or cache. | A later tool call resamples current state; metadata lets the model see sample truncation but does not make totals exact. |
| **projection `ReadAuthoritative` adapter** — declaration `pkg/projection/mutation_types.go:158-160`; implementation `mutation_client.go:954-984`. | Calls direct `graph.ingest.query.entity`. | Canonically ID-checked `*EntityState` with mutation-shaped errors. | Omitted by graph-ingest and the adapter. | Requested ID and returned canonical ID must match; classified absence/failure maps into mutation taxonomy. | Per-operation embedded adapter; no bucket bind, watch, or distinct readiness surface. | Provides current-value read-back but cannot supply `ExpectedRevision`; callers use semantic comparison to resolve write ambiguity. |
| **projection create ambiguity/read-back callers** — `pkg/projection/mutation_client.go:587,655,1235`. | `CreateOwnedEntity`/create-with-triples ambiguity and committed-create verification call `ReadAuthoritative`. | Existing/committed entity used to decide collision, equivalence, or success. | Omitted; callers cannot use the read for revision-fenced CAS. | Adapter validation above; caller compares canonical owned state rather than treating every `entity_exists`/ambiguous result alike. | Only on entity-exists, ambiguous, or verification paths inside mutation lifecycle. | Authoritative re-read recovers the operation decision after uncertain write outcome; no authority bytes are repaired. |
| **projection replace post-commit verifier** — `pkg/projection/mutation_client.go:772`. | `ReplaceOwned` calls `ReadAuthoritative` after mutation commit. | Current entity used to verify the intended owned projection. | Omitted. | Adapter validation plus operation-specific owned-triple comparison; absence/read failure fails verification. | Runs after successful replacement response. | Detects post-commit divergence so the caller does not predict success from an ack alone; it does not expose revision. |
| **projection append-evidence ambiguity/anomaly verifier** — `pkg/projection/mutation_client.go:928,1038,1063,1080`. | `AppendEvidence` ambiguity, anomaly, committed, and requested-failure paths call `ReadAuthoritative`. | Current subject entity used to classify committed/not-committed/unknown evidence outcomes. | Omitted. | Adapter validation plus evidence triple comparison; absence and transient reads retain mutation error distinctions. | Conditional read-back only when the write response cannot alone establish the result or verification is required. | Observing authority resolves architectural write-outcome drift without guessing; unresolved reads remain explicit unknown/error rather than silent success. |
| **lesson curator through public `AuthoritativeReader`** — `processor/agentic-tools/lesson_promotion.go:31-56,59-115`. | Reads lesson and every cited evidence entity via injected `ReadAuthoritative`. | Promotion eligibility decision over complete lesson/evidence entities. | Omitted by the interface. | Missing lesson/evidence and stubs refuse promotion; transient errors abort; no-evidence also refuses. | Runs synchronously before the separate owned replacement; product/operator curation seam. | Fresh reads prevent promotion from predicting evidence existence; refusal preserves proposed state until later retry, without repairing evidence. |
| **public `graph/query.Client` prefix helper** — interface `graph/query/interface.go:46-61`; implementation `graph/query/prefix.go:14-110`. | Calls public `graph.query.prefix`; `QueryPrefixAll` iterates it from cursor zero. | One typed page, or bounded aggregate plus explicit `truncated` boolean. | Omitted for every entity; cursor/truncation is not a revision fence. | Prefix and every returned entity are validated; handler/transport errors surface. A defensive empty-page-with-cursor case stops without spinning. | Thirty-second per page; aggregate requires positive caller-owned `maxEntities`; no unbounded mode. | Later calls rescan current authority. The explicit cap/truncation result exposes bounded coverage; comment at `prefix.go:24-33` is stale against the current classified provider and is documentation drift. |
| **public embedded `fusionnats.Client` prefix** — stable package surface `pkg/fusion/fusionnats/doc.go:12-33`; lifecycle `client.go:46-233`; call `:280-304`. | Calls `graph.query.prefix` for one bounded page. | `[]fusion.Seed` IDs only; `NextCursor` is not followed or exposed. | Omitted with the projected seed values. | Canonically validates returned entities; request/decode failure errors; zero results are empty. | Lazily watches `GRAPH_STATUS` for graph-index readiness; one bounded first wait; configured unknown-readiness fail-closed or degraded behavior; `Close` stops the watcher. | Each resolution re-reads a current page; no revision/snapshot fence, and prefix truncation is not observable through the seed result. |
| **public embedded `fusionnats.Client` exact** — `pkg/fusion/fusionnats/client.go:370-390`; engine uses `pkg/fusion/engine_lens.go:155,162,509`. | Calls `graph.query.entity`. | `*fusion.Entity` (ID and triples) or nil. | Omitted. | Canonical entity validation; stable not-found becomes `(nil,nil)`; other classified/transport errors propagate. | Shares the client readiness watcher and close lifecycle above; read is per invocation. | Later engine evaluation refetches current authority; nil conflates only the provider's stable absence, not transient failure. |
| **public embedded `fusionnats.Client` batch** — `pkg/fusion/fusionnats/client.go:392-426,445-485`; engine uses `pkg/fusion/engine_graph.go:231`, `engine_facets.go:199,207`. | Calls `graph.query.batch`. | `fusion.Hydration`, restored to requested order with found/missing/unknown accounting. | Omitted for all hydrated entities. | Canonical validation is inherited from public provider; explicit missing reasons are preserved; underreported requested IDs become unknown rather than guessed absent. | Shares readiness watcher/close lifecycle; one request per hydration batch. | Reconciliation repairs response-order/accounting drift at the adapter boundary; a later hydration retries unknown/current values. |
| **research graph-execute batch adapter** — `processor/research-graph-execute/adapters.go:20-40,55-100,322-338`. | Calls public `graph.query.batch`. | `[]fusion.Evidence` for found entities. | Omitted for every evidence projection. | Canonical provider validation; reported missing IDs are logged and omitted, not fatal; handler/transport failure fails the call. | Per research execution request with configured timeout; no cache/watch. | A later execution rehydrates authority; missing evidence remains visible in logs but the returned slice is partial. |
| **configurable HTTP gateway exact routes** — registration `gateway/http/http.go:155-167`; relay `:170-281`; route contract `gateway/types.go:10-21`; shipped instances `configs/http-gateway-semantic-search.json:105`, `configs/examples/pathrag-graph-traversal.json:143`, `configs/examples/bm25-semantic-search.json:157`. | All three configure `GET /entity/:id` for public `graph.query.entity`, but registration concatenates that string unchanged into `net/http.ServeMux.HandleFunc`. ServeMux wildcard syntax is `{id}`, so `:id` is a literal segment: ordinary `/entity/<id>` does not reach this handler. | Ordinary adopter-shaped request returns mux 404 before NATS. Literal `/entity/:id` can reach the handler and returns raw provider JSON only if its body is a valid exact-query request. | Omitted on the reachable successful path. | No `PathValue`, colon conversion, path extraction, or request shaping exists. The handler forwards the body verbatim; a normal bodyless GET to the literal route reaches graph-ingest with empty bytes and becomes sanitized HTTP 400 invalid request. A caller-supplied `{"id":"..."}` body can succeed; typed provider not-found maps to HTTP 404. | Routing happens before gateway request metrics/lifecycle for ordinary `/entity/<id>`; a literal match runs one request with the configured two-second timeout. | Retrying the advertised dynamic path repeats mux 404; retrying the literal bodyless route repeats 400. The relay has no architectural self-repair for the colon/ServeMux and path/body contract drift; only a non-advertised literal path plus shaped body reaches current authority. |
| **GraphQL exact/prefix/summary facade** — route/shape mapping `gateway/graph-gateway/component.go:843-1050`; request/error path `:1781-1886`; prefix unwrap `:1719-1771`. | Routes root fields to `graph.query.entity`, `graph.query.prefix`, or `graph.query.summary`. | HTTP GraphQL `data.entity`, raw entity array for prefix, or unwrapped summary data. | Omitted from all three GraphQL shapes. | Prefix input and returned entities are validated; default prefix limit 100. Classified handler failures become GraphQL HTTP 200 errors, timeout 504, transport 500. Prefix cursor is discarded when the envelope is unwrapped. | One bounded HTTP request with configured query timeout; no client-visible graph readiness or cursor lifecycle for prefix. | A later HTTP request refetches/resamples current state; value-only exact and cursorless prefix cannot support revision-fenced or complete reconciliation. |

Every production match from the exact-subject and `ReadAuthoritative` closing searches is assigned above. The four
internal providers return current authority-derived results but no KV revision; the only value-plus-same-entry-revision
exception remains the direct-KV agentic `query_entity` tool in the direct census. Recovery across the mediated paths is
architectural: fresh observation, explicit partial/unknown outcomes, bounded retry, ambiguity read-back, and derived
re-evaluation. None is a SemStreams physical backup/checkpoint product, and none changes the owner ruling that cluster
operation is valid while infrastructure operators own backup/checkpoint.

### 6. Adjacent live issue claims

All five issues were queried live on 2026-08-05 and were OPEN.

| Issue | Present consumer and claim | Collision exposed |
|---|---|---|
| #681 `graph-ingest: retain enough ENTITY_STATES revisions for lifecycle history` | Lifecycle Manager and lifecycle gateway expect multi-transition audit reconstruction from authority History. | Conflicts with catalog History 1 and ADR-090's statement that H1 is not lifecycle audit history. |
| #843 `e2e(lifecycle): tier has been failing while reporting green — history returns 1 event, expected >=3` | `task e2e:lifecycle`; expected create + rule + operator events, observed only one. | Live end-to-end evidence for the #681/H1 contract collision; `ignore_error` had masked failure. |
| #689 `gated-dag: replace raw claim writes with a contract-bound CAS primitive` | Gated-DAG claim/unclaim needs authoritative revision, owner token, committed/not-committed/unknown outcomes, read-back, and conditional unclaim. | Existing gated-DAG watch ignores revisions and its private writes leave ExpectedRevision/OwnerToken empty. |
| #851 `graph-ingest: expose authoritative entity revision so public ExpectedRevision CAS is usable` | SemMachina and general external read-modify-write callers need value plus same-entry nonzero revision, passed unchanged to `ExpectedRevision`. | Agentic `query_entity` proves the primitive exists in one direct-KV tool, but admitted GraphQL/projection/graph-ingest exact results omit it. |
| #892 `graph-ingest: update_with_triples bare deltas do not advance EntityState revision metadata` | SemMachina observed changing triples while logical `Version` stayed old and `UpdatedAt` became zero. | Confirms logical entity metadata cannot substitute for KV revision. |

Live evidence commands and URLs are in Closing searches. Issue order does not schedule program work:
`docs/proposals/graph-state-read-write-program.md:196-213`.

### 7. Adjacent ownership boundaries

- Lifecycle declaration and the H1 audit contradiction belong to the lifecycle/documentation slices, while the current
  authority-revision fact remains an exact-read collision: program `:399-414,480-487,517-529`; ADR-090 `:63-68`.
- Graph-index, spatial, temporal, embedding, clustering, and other derived owners already carry distinct bootstrap,
  poison, readiness, repair, and rebuild semantics. They are not one generic recovery class:
  program `:391-414,488-505,520-526`.
- `ENTITY_SUFFIX_INDEX` is currently graph-ingest-owned, best-effort updated, and lazily backfilled; it has LWW
  collision, trusted-stale-hit, and blind-delete findings. Its disposition remains GS-05 and no future owner was selected:
  `suffix-inventory-addendum.md:22-82,185-219,250-270`; `suffix-inventory-review.md:1-7`;
  program `:399-414,521`.
- Revision 35's future graph-index suffix ownership and physical capture of the suffix bucket have no accepted owner
  ruling under the corrected boundary.

### 8. Consumer at birth

This inventory introduces no exported symbol, port, subject, bucket, config field, CLI, or operator protocol. There is
therefore no new-surface consumer-at-birth row. Revision 35's proposed surfaces have no birth authorization and remain
rejected correction evidence.

## Adopter seam inventory

| Specific adopter | What they must know now | If they do nothing | Where they find out | What they should have to know |
|---|---|---|---|---|
| External application developer doing CAS (#851) | Which front door returns the exact KV revision; that `EntityState.Version` is different; which revision is accepted by `ExpectedRevision`; refetch behavior. | GraphQL/projection reads give no revision; direct KV bypass violates ownership; unconditional writes can lose an interleaving. | Issue/doc or runtime mismatch today; no admitted typed compile-time path. | The admitted authority read and conditional mutation contract, not bucket identity or revision provenance. |
| External HTTP developer using a shipped `/entity/:id` route | That `:id` is registered as a literal ServeMux segment, not a parameter; that even the literal route ignores the path and requires a JSON `{"id":"..."}` GET body. | `/entity/<actual-id>` returns mux 404 without reaching the gateway. `/entity/:id` with an ordinary empty GET body reaches NATS but returns sanitized 400 invalid request. | The shipped configs and `gateway.RouteMapping` comment claim colon parameters, while `RegisterHTTPHandlers` performs no conversion and runtime reveals only 404/400. | Only the entity ID in one documented dynamic URL; no duplicate JSON body value or router-syntax knowledge. |
| Component author reading current entity state | Whether to use graph-ingest RPC, `graph/query.Client`, lifecycle, agent tool, or raw KV; each surface's poison, absence, readiness, and revision semantics. | A locally convenient reader silently chooses different dependencies and failure scope; direct readers may bind unrelated buckets or synthesize absence. | Package docs, code, runtime errors, and scattered specs. | One operation-specific seam and the meaning of its result; no storage topology prediction. |
| Lifecycle adopter | H1 authority cannot supply retained transition audit; raw/projected/history surfaces differ; watch cancellation and poison are Manager-wide. | `/history` can return one event while appearing successful, as #843 records. | E2E assertion/issue; current lifecycle docs still claim revision replay. | Lifecycle audit availability/retention as a lifecycle-owned contract, independent of authority internals. |
| Rule or gated-DAG author | Rule absence may be synthetic revision 0; gated-DAG watch is a nudge only; claims are not CAS-safe across workers. | Rule semantics may treat synthetic deletion as observed; concurrent claimers can guess after ambiguity. | Source comments and #689; gated-DAG watch failure is a warning/backstop. | Typed state/claim outcomes, not KV watch mechanics or owner-token wiring. |
| Model/tool author using `query_entity` | Revision is metadata beside JSON content; other graph-query tools omit per-entity revisions; tool access is direct KV, not the admitted graph API. | The model can ignore metadata or assume all query tools have the same contract. | Tool schema/result at runtime and executor code. | A tool-level semantic result whose revision meaning is explicit without knowing `ENTITY_STATES`. |
| Graph operator | Cluster URLs/reconnects exist; production replication guidance says R=3; many defaults/catalog declarations say 1; general cluster runbook is pending; backups/checkpoints are infrastructure-owned. | First-created resources can be replicas 1; availability/durability may not match production intent; stale docs may suggest SemStreams snapshot/restore owns recovery. | ADR-032 and scattered config/docs; no completed general runbook. | Their infrastructure durability/backup policy and observable NATS state, not a SemStreams checkpoint protocol. |
| Operator using raw diagnostics | Message logger accepts arbitrary bucket/pattern and does not canonical-validate authority; triple endpoint returns triples only and skips fetch races. | Diagnostic output can be partial or structurally different from admitted reads without an explicit unsoundness marker. | HTTP behavior/logs/source comments. | That the endpoint is diagnostic and what omissions/partial-result rules apply; no inference that it is an application contract. |

The present seams repeatedly ask adopters to predict facts the framework or NATS owns: which reader is strongest, which
revision belongs to a value, whether a projection has caught up, whether a first creator stamped replicas 1, and whether
history exists. Those are measured adopter debts in the current surface, not documentation requests resolved by this
inventory.

## Same-class collision table: exact authority read

| Required dimension | Current spellings and collision | Evidence |
|---|---|---|
| Semantic class | Raw authority value, lifecycle projection, triple subset, predicate scalar, rule event snapshot, tool content+metadata, raw diagnostics, and derived-query hydration are all called “read” but prove different facts. | Reader census above. |
| Owners | Graph-ingest owns authority; lifecycle, graph/query, tools, rules, graph-index family, clustering, services, and gateways each own separate read behavior. | `graph/kvcatalog.go:67-74`; acquisition census. |
| Catalogs | Catalog says owner creates/readers bind must-exist, yet legacy direct `GetKeyValueBucket` and raw `js.KeyValue` readers coexist with `OpenCatalogBucket`. Generic diagnostics bypass semantic catalog typing. | `graph/kvcatalog.go:247-274`; reader census. |
| Status | Graph-ingest, graph-index, embedding, rule, spatial, temporal, lifecycle guard, and graph/query client use different readiness/poison/watch-loss states. | `processor/graph-ingest/readiness.go:205-343`; reader census. |
| Lifecycle | One-shot Get, cached client lifetime, lazy tool bind, per-request scan, bootstrap+live watch, polling cycle, Manager watch, and H1 History have different start/stop/retry rules. | Reader census. |
| Ownership | Raw KV is declared owner/operator seam, but numerous controlled internals bind directly; GraphQL/projection mediate; no single admitted general embedded client exists. | ADR-090 `:36-41`; program `:126-152`. |
| Readers | Exact result shapes disagree on revision, absence, poison, validation, dependency, and transport error. `query_entity` is the existing same-entry value+revision exception. | Reader census; agentic tool evidence. |
| Writers | Graph-ingest returns bare value on exact RPC while mutation APIs accept `ExpectedRevision` and return committed KV revision; read and write halves do not meet for admitted CAS callers. Logical `Version` can also drift (#892). | `processor/graph-ingest/query.go:60-105`; #851; #892. |
| Recovery | Current recovery is reader-specific retry/rebind, poison reset/reingest, or derived reconciliation. Physical backup/checkpoint is operator infrastructure responsibility under the owner ruling. | Reader census; owner correction. |

## Same-class collision table: graph-ingest runtime coordination

| Required dimension | Current spellings and collision | Evidence |
|---|---|---|
| Semantic class | “Single writer” spans authority bucket ownership, component-instance admission, request responders, JetStream durable consumption, per-entity lanes, predicate-owner leases, and infrastructure replicas. These are not equivalent. | ADR-090 `:24-35`; runtime evidence below. |
| Owners | Catalog names graph-ingest owner; registry/manager identify instances by configured name; status/health/metrics use other names; NATS owns replica leadership. | `graph/kvcatalog.go:67-74`; `service/component_manager.go:940-959,2182-2250`. |
| Catalogs | `ENTITY_STATES` and `GRAPH_INGEST_APPLIED_SEQ` are graph-ingest-owned; ports are nonexclusive; stream consumers derive identity from subjects, not runtime instance. | `processor/graph-ingest/component.go:1230-1288,1527-1571`; `component/port_kv.go:28-41`; `component/port_nats.go:34-48,71-86`; `component/port_jetstream.go:55-68`. |
| Status | `GRAPH_STATUS`, `COMPONENT_STATUS`, component-manager state, `Health()`, and package-level Prometheus metrics use different scopes. Catalog owner text for GRAPH_STATUS names graph-index/embedding while graph-ingest and rule also publish. | `graph/kvcatalog.go:76-81,134-149`; `processor/graph-ingest/readiness.go:308-343`; `processor/rule/readiness.go:170-209`; `reviewed-fifth-pass-inventory.md:333-385`. |
| Lifecycle | Same-name local instances are rejected; differently named graph-ingest instances pass. A duplicate local consumer key stops/replaces its earlier consume context while the earlier component can remain lifecycle-reported running. | `component/registry.go:253-280,574-600`; `service/component_manager.go:940-955,1080-1093`; `natsclient/stream.go:321-337`. |
| Ownership | `OWNER_CLAIMS`/`OWNER_PRESENCE` fence predicate producers, not graph-ingest processes. Enforcement defaults false; confirmed mismatches reject only when enabled, while empty token/missing reader/unclaimed/legacy/reader-failure paths fail open. | `processor/graph-ingest/component.go:452-459,2091-2209`; `pkg/ownership/doc.go:11-28`. |
| Readers | Four graph-ingest query subjects and eight mutation subjects are plain request subscriptions; all subscribers receive/execute/respond and replies race. | `processor/graph-ingest/query.go:27-55`; `processor/graph-ingest/mutations.go:79-125`; `natsclient/request.go:337-405`. |
| Writers | Authority writes include create, put, update, delete, retrying RMW, and triple lanes. JetStream ingest uses subject-derived durable names and process-local keyed lanes; the durable guard's plain Put assumes one lane per `(entity,stream)`, which is only process-local. | `processor/graph-ingest/component.go:2464-3007,3167-3527,1543-1574`; `processor/graph-ingest/keyed_ingest.go:260-299`; ADR-072 `:228-240`. |
| Recovery | Guard read/stamp suppresses redelivery after restart but is not process election. Current poison/cutover docs repair or wipe/reseed. Owner ruling assigns physical backup/checkpoint to infrastructure, so it cannot close runtime writer collisions. | `processor/graph-ingest/keyed_ingest.go:260-299`; `docs/operations/33-graph-poison-response-runbook.md:12-65`; `docs/operations/17-predicate-cutover-clean-wipe.md:35-70`. |

## Revision-35 scope-loss inventory under the corrected boundary

| Foundation/adjacent fact | Revision-35 disposition | Observed conflict |
|---|---|---|
| Admitted exact authority value+revision | Recovery-inspection-only reader | #851 CAS consumer remains uncovered; existing agent tool exception was not inventoried as an admitted API. |
| Divergent authority readers | Adds another recovery reader | Existing read collisions remain. |
| Graph-ingest runtime safety | Relocated to GS-02 | Current request, durable, lane, admission, status, and owner-lease collisions remain in GS-01 evidence. |
| Cluster support | Initial support restricted to offline single node, replicas 1 | Conflicts with ADR-032 and owner ruling; cluster is a valid topology. |
| Operational recovery ownership | SemStreams CLI/control KV/provider/maintenance NATS/fences | Conflicts with owner ruling that normal infrastructure owns backup/checkpoint. |
| Architectural recovery from drift | Dominated by physical snapshot mechanics | Predictable read, writer, readiness, and derived convergence defects were displaced. |
| Lifecycle history | No resolution | #681/#843 and H1 conflict remain later-slice evidence. |
| Suffix ownership | Captured and future graph-index owner asserted | Current graph-ingest/GS-05 ownership remains; no future owner ruling exists. |

## Closing searches and measured results

All searches ran from `/private/tmp/semstreams-gs00` against the stated baseline/worktree evidence.

### Authority acquisition closure

```sh
rg -n '(OpenCatalogBucket|GetKeyValueBucket|EnsureCatalogBucket|KeyValue\().*(BucketEntityStates|ENTITY_STATES)|(BucketEntityStates|ENTITY_STATES).*(OpenCatalogBucket|GetKeyValueBucket|EnsureCatalogBucket|KeyValue\()' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!test/**'
```

Literal production matches occur in graph-ingest, `graph/query`, lifecycle, agent-run, agentic tools, graph-index,
spatial, temporal, embedding, gated-DAG, rule exact fetch, and graph-triples HTTP. Variable-name acquisition paths were
separately followed at `processor/rule/entity_watcher.go:106-139` and
`processor/graph-clustering/component.go:1145-1171`. Generic caller-selected diagnostic acquisition was followed at
`service/message_logger_http.go:459-556` and `service/message_logger_kv_watch.go:195-290`.

### Direct access closure

```sh
rg -n 'entity(Bucket|StateBucket|StatesBucket)\.(Get|Keys|KeysByPrefix|ListKeys|Watch|WatchAll|History)' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!test/**'
rg -n '\.History\(' --glob '*.go' --glob '!**/*_test.go' --glob '!test/**'
```

The first search produced the graph-ingest, graph/query, clustering, graph-index, embedding, and rule direct sites
enumerated above. The production History search produced lifecycle authority History at
`pkg/lifecycle/manager_query.go:539`, generic temporal-reader History at `natsclient/kv_temporal.go:82`, storage-report
History at `natsclient/storage_report.go:612`, and the lifecycle-gateway call into Manager at
`gateway/lifecycle-gateway/handlers.go:306`. The lifecycle call explicitly names the `ENTITY_STATES` handle; the generic
temporal-reader and storage-report calls require their own bucket-configuration evidence before attributing a bucket.

### Mediated authority-reader closure

```sh
rg -n 'graph\.ingest\.query\.(entity|batch|prefix|suffix)|graph\.query\.(entity|batch|prefix|summary)' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!test/**' .
rg -n 'graph\.ingest\.query\.(entity|batch|prefix|suffix)|graph\.query\.(entity|batch|prefix|summary)' \
  --glob '*.json' --glob '*.yaml' --glob '*.yml' --glob '*.toml' --glob '!test/**' .
rg -n 'ReadAuthoritative\(' \
  --glob '*.go' --glob '!**/*_test.go' --glob '!test/**' .
```

The named-subject search closes over the four graph-ingest subscriptions; the graph-query internal router; direct
PathRAG, suffix-resolver, gated-DAG, agentic-loop, agentic-tools, and projection subject constants; the four public
graph-query subscriptions; `graph/query.Client`; `fusionnats.Client`; research graph-execute; summarize-graph; and the
GraphQL route table. Those executable matches are assigned to rows in section 5. The remaining matches are package
documentation, schema descriptions, wire-type comments, and graph-query output-port declarations rather than
additional requests. The configuration search also finds the gated-DAG generated-schema descriptions and three
shipped generic HTTP route instances for `graph.query.entity`; the latter are assigned to the configurable HTTP gateway
row. `gateway/http/doc.go:43` and `gateway/doc.go:74` are documentation for that same relay, not two more runtime
consumers.

The `ReadAuthoritative` search returned the interface and implementation; three create/verification sites at
`pkg/projection/mutation_client.go:587,655,1235`; replace verification at `:772`; four append-evidence
ambiguity/anomaly/verification sites at `:928,1038,1063,1080`; and lesson-curator reads at
`processor/agentic-tools/lesson_promotion.go:70,89`. Every returned production call is assigned to a section-5 row.

Shared constants and routed calls were then followed to their request sites rather than counted only at their string
definitions: graph-query router calls in `processor/graph-query/query.go:68-300` and `summary.go:40-124`; gated-DAG
pagination at `processor/gated-dag/reader.go:66-123`; agentic-loop calls at `todos.go:68-94`, `lessons.go:63-113`, and
`graph_writer.go:253-288`; agentic-tools calls at `emit_lesson.go:239-267` and
`executors/summarize_graph.go:149-203`; projection at `mutation_client.go:954-984`; public prefix helper at
`graph/query/prefix.go:35-110`; fusion calls at `pkg/fusion/fusionnats/client.go:280-304,370-426`; research batch at
`processor/research-graph-execute/adapters.go:55-100`; and GraphQL dispatch at
`gateway/graph-gateway/component.go:1781-1886`.

The generic configured HTTP exact route was closed separately:

```sh
sed -n '155,281p' gateway/http/http.go
sed -n '10,21p' gateway/types.go
rg -n 'PathValue|SetPathValue|strings\.Replace.*:|route\.Path.*\{' gateway/http --glob '*.go'
go doc net/http.ServeMux
```

`RegisterHTTPHandlers` concatenates `route.Path` unchanged and passes it to `ServeMux.HandleFunc`; the route contract
nevertheless claims colon-notation parameters. The extraction/conversion search returned zero matches. ServeMux's
documented wildcard segments are `{NAME}` or `{NAME...}` and every non-wildcard segment is literal. Therefore the
three `GET /entity/:id` configs match the literal `:id` segment, not `/entity/<actual-id>`. Once the literal route is
reached, `createRouteHandler` reads and forwards only the body; it never reads a path value. A bodyless literal GET
thus sends empty bytes to exact-query JSON decoding and the generic gateway maps the classified invalid result to
sanitized HTTP 400. An ordinary `/entity/<actual-id>` never reaches this handler and receives mux 404.

### Physical-recovery product closure

```sh
git grep -n -i -E \
  'semstreams-recovery|AUTHORITY_RECOVERY|RecoveryStartupGate|checkpoint-source|restore-target|complete_readonly|MaintenanceProvider' \
  cb09133e0154296664343c5a5d0723b294cbfd5f -- \
  ':!openspec/changes/establish-authority-read-and-recovery/**' \
  ':!docs/proposals/graph-state-read-write-*'
```

Result: zero matches. A broader `snapshot.*ENTITY_STATES|ENTITY_STATES.*snapshot` search matches graph-ingest and
derived-reader bootstrap snapshot language, ADR-090, historical design evidence, and a lifecycle sketch; it does not
establish a shipped SemStreams backup/checkpoint product.

### Architectural-recovery wording

```sh
git grep -n -i -E 'recovery from drift|architectural recovery|predictable graph foundation' \
  cb09133e0154296664343c5a5d0723b294cbfd5f -- openspec docs .agents README.md
```

At baseline, zero matches. The untracked parent-session r36 draft later introduced one `recovery from drift` match;
that is not durable baseline authority.

### Cluster/runbook closure

```sh
rg -n -i 'JetStream cluster|R=3|replication runbook|backup.*SKG|SKG.*backup' \
  docs README.md config configs
```

Matches are ADR-032's accepted replication position and pending migration schedule, plus isolated production/development
replica guidance. No completed general cluster/replication runbook was found under `docs/operations`.

### Live issue evidence

```sh
for n in 681 689 843 851 892; do
  gh issue view "$n" --repo C360Studio/semstreams --json number,title,state,url,body
done
```

All five returned `state: OPEN` on 2026-08-05:

- https://github.com/C360Studio/semstreams/issues/681
- https://github.com/C360Studio/semstreams/issues/689
- https://github.com/C360Studio/semstreams/issues/843
- https://github.com/C360Studio/semstreams/issues/851
- https://github.com/C360Studio/semstreams/issues/892

## Open evidence questions

1. Which canonical repository record will carry the binding correction that program “recovery” means recovery from
   architectural drift and that infrastructure operators own backup/checkpoint?
2. Which stale physical-recovery statements in ADR-090 and the program remain historical decision context, and which
   are intended to be corrected as current authority?
3. What measured production topology explains the current gap between ADR-032 R=3 guidance, catalog/built-in replicas
   1 declarations, preserved operator replica settings, and the still-pending general cluster runbook?
4. Which of the enumerated direct readers are admitted owner/dependency seams, which are diagnostic-only, and which
   are accidental public surfaces requiring disposition by their assigned program slice?
5. Is the agentic `query_entity` value-plus-revision result intentionally a supported tool contract, and which consumers
   read its `revision` metadata today?
6. Which existing admitted operation is the present consumer boundary for #851's same-entry value-plus-revision CAS
   need, given that GraphQL, projection, and graph-ingest exact RPC currently omit revision?
7. What accepted lifecycle-owned evidence supersedes the current `ENTITY_STATES` H1 audit-history claim measured by
   #681 and #843?
8. What deployment/runtime census proves or falsifies multiple differently named or cross-process graph-ingest
   instances against the current request, durable-consumer, keyed-lane, and owner-lease behavior?
9. What owner record, if any, changes the current graph-ingest/GS-05 ownership of suffix resolution or makes the suffix
   bucket part of any authority unit?
