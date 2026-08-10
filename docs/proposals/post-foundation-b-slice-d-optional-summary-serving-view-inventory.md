# Post-Foundation-B Slice D optional-summary serving-view inventory

**Status:** Post-Slice-C merged-tree inventory. This freezes the approved reduced Slice D target. It supersedes the
optional-summary generation-supervisor mechanism in the accepted roadmap and active OpenSpec change; it does not
reopen the other thirteen owner rulings or mark D.1-D.5 complete. Independent review of this reassessment is pending.

**Baseline:** `03ef1e5d8039adbe3e47862912f38bf13ea39b48` (`feat(graph-query): supervise community generations
(#921)`). HEAD, `origin/main`, and their merge base were this commit when the inventory was captured.

**Accepted evidence:**

- Frozen foundation inventory: `docs/proposals/post-foundation-b-graph-foundation-remap-inventory.md`, SHA-256
  `c87cdf12506ac62272f340f975f14a27f28e78307207a6aae554ede595a99040`.
- Owner-reviewed roadmap: `docs/proposals/post-foundation-b-graph-query-contract-closure-roadmap.md`, SHA-256
  `ff23db51ce7bf6e3d45da09a1706bf70ee548ae5e6aa2b12201ceeae64c4f343`.
- Slice C inventory: `docs/proposals/post-foundation-b-slice-c-community-generation-supervisor-inventory.md`,
  SHA-256 `507ac217e62e395b8f8f3236f8b4eb0883bf298511c22965dcbc67c31f89c6dc`.
- Merged Slice C task evidence:
  `openspec/changes/post-foundation-b-graph-query-contract-closure/tasks.md:49-75`.

## Decision

Slice D SHALL consolidate the optional `COMMUNITY_SUMMARIES` reader on the existing `pkg/graphview.View` serving-view
primitive. It SHALL NOT build a second private generation/lease/final-validation system beside the Slice C community
generation supervisor.

This reduction is correct because the two stores have different read semantics:

- `COMMUNITY_INDEX` chooses the current community partition and membership. A stale generation can associate an entity
  with the wrong current partition, so Slice C requires one generation lease and final response validation.
- `COMMUNITY_SUMMARIES` is content-addressed by `(level, membership hash)`. A value for an old membership is not
  selected by a community with a new membership hash. Once a point read returns a valid enhanced record for the key,
  later watcher loss does not make that immutable join false.

`graphview.View.Get` already serializes a point read with projection writes and gates bootstrap, watcher loss, stop,
not-found, and per-key poison (`pkg/graphview/view.go:422-441`; `pkg/graphview/errors.go:9-54`). After a watcher loss,
subsequent point reads fail closed. The query owner maps every non-value outcome to the already admitted statistical
summary fallback. No request lease or final-response revalidation adds correctness here.

## Current summary lifecycle

- `Component` still carries a `resource.Watcher`, mutex, and permanent `summaryWatchStarted` guard dedicated to the
  optional bucket (`processor/graph-query/component.go:158-168`). These are independent of Slice C's community
  generation supervisor.
- Start launches the Slice C supervisor, then calls `startSummaryBucketWatcher`
  (`processor/graph-query/component.go:490-512`). Summary absence does not delay responder installation or component
  startup.
- `startSummaryBucketWatcher` performs one bucket-presence probe through the generic resource watcher. Presence opens
  the bucket; absence starts periodic presence checks (`processor/graph-query/component.go:627-663`).
- `attachSummaryWatch` opens the raw write-capable KV handle through `GetKeyValueBucket`, consumes the once guard only
  after a successful open, and launches one `watchSummaries` goroutine
  (`processor/graph-query/component.go:665-703`). It does not use the read-only catalog seam.
- If `watchSummaries` exits while the bucket remains present, the resource watcher sees no resource loss and the
  permanent guard prevents another attach. Enhanced summaries remain unavailable until component restart
  (`processor/graph-query/component.go:675-700`).
- Stop cancels the component, waits for component goroutines, stops the resource watcher, then stops the cache's stored
  summary watcher (`processor/graph-query/component.go:519-578`; `processor/graph-query/community_cache.go:537-541`).
  The split ownership makes shutdown ordering harder to reason about than one component-owned view.

## Current projection and defects

- `communityCache` stores the optional summaries in a shared process-lifetime map protected by the same mutex as the
  active community-generation pointer (`processor/graph-query/community_cache.go:21-39`). Summary state is not part of
  the generation, but shares its container and lock.
- `watchSummaries` calls one `WatchAll`, stores the watcher on the cache, and blocks until cancellation
  (`processor/graph-query/community_cache.go:301-325`).
- Its receive is `entry := <-watcher.Updates()` without the channel `ok` value. A closed updates channel therefore
  produces `nil` forever and hot-spins through the initial-sync branch instead of reporting loss
  (`processor/graph-query/community_cache.go:326-340`).
- Replay updates mutate the live map before the initial sentinel. A query can observe a partial bootstrap
  (`processor/graph-query/community_cache.go:326-356`).
- Delete is handled, but purge is not classified as delete. A purged key is decoded as an update and can leave stale
  state (`processor/graph-query/community_cache.go:334-339`).
- Invalid JSON is logged and skipped without a canonical poison record. The prior valid value remains in the shared map
  (`processor/graph-query/community_cache.go:344-356`).
- `summaryFor` computes the content-addressed key, reads the shared map, and accepts only a non-empty enhanced record
  (`processor/graph-query/community_cache.go:373-390`). It has no way to distinguish bootstrap, watch loss, poison,
  absence, or a legitimate non-enhanced record; all currently become the same boolean miss.

These defects justify replacing the bespoke reader. They do not justify another summary-specific lifecycle primitive.

## Existing shared serving-view primitive

`pkg/graphview.View[T]` is the already admitted component-owned, in-process KV serving view:

- It accepts a narrow read/watch source and validating decoder, and exposes no write capability or global registry
  (`pkg/graphview/view.go:31-47,135-199`). `graph.CatalogReader` satisfies its `WatchAll` requirement while preserving
  the read-only catalog boundary (`graph/kvcatalog.go:209-245`).
- Start stages bootstrap internally and publishes usability only after the sentinel. A closed updates channel is a
  watcher-loss transition (`pkg/graphview/view.go:201-223,585-610`).
- Restart replays through the same view, reconciles ghost keys not present in the new snapshot, and returns to live only
  after caught-up (`pkg/graphview/view.go:225-248,585-610`).
- Delete and purge share the canonical delete path (`pkg/graphview/view.go:614-620`).
- Decoder failures produce typed per-key poison rather than a stale value; unrelated keys continue
  (`pkg/graphview/view.go:614-626,641-671`; `pkg/graphview/errors.go:33-54`).
- `Get` gives coherent point reads and canonical not-ready, watcher-lost, stopped, poisoned, and not-found outcomes
  (`pkg/graphview/view.go:422-441,556-575`).
- Stop owns watcher and goroutine termination (`pkg/graphview/view.go:286-317`).

Slice D needs one component-owned supervisor around this primitive because the optional bucket may not exist when
graph-query starts. Its control contract is exact:

1. The supervisor is the only code allowed to open, publish, clear, stop, or replace the summary view.
2. It repeatedly calls `graph.OpenCatalogReader` for `COMMUNITY_SUMMARIES` using the existing `RecheckInterval`.
3. After acquisition it constructs exactly one `graphview.View[clustering.CommunitySummaryRecord]`. Its
   `OnWatcherLost` hook performs only a nonblocking send to a capacity-one supervisor control channel; the hook never
   retries, stops a view, logs synchronously, or blocks the graphview watcher goroutine.
4. The supervisor calls `Start`. If `Start` fails, it calls `Stop` on that unpublished view before waiting or retrying;
   this is required because `View.Start` starts its ticker before attempting `WatchAll`
   (`pkg/graphview/view.go:201-223`).
5. After successful Start, the supervisor publishes the single view pointer under the summary-view mutex. Publishing
   during bootstrap is safe because `Get` returns `ErrNotReady` until caught-up. There is never more than one published
   or unstopped view.
6. On a loss signal, the supervisor clears that exact pointer under the same mutex, calls `Stop` on the failed view,
   then reopens the catalog reader and constructs/starts a replacement. It does not call `Restart` on a possibly stale
   bucket handle and never constructs the replacement before the previous view has stopped.
7. On component cancellation, the supervisor atomically clears the exact published pointer, stops the current view,
   drains no work, creates no replacement, and exits. Cancellation while catalog open, retry wait, construction, Start,
   or loss handling has the same terminal cleanup obligation.

Summary reads copy the synchronized pointer under the mutex and call `Get` outside it. A concurrent loss either makes
that `Get` fail closed or occurs after a successful content-addressed read; both map truthfully. There is no second
map, watcher owner, retry goroutine, or control loop.

## Target decode and read policy

The shared surface owns acquisition and canonical outcome classification; graph-query owns the policy response. The
view's typed decoder is `graphview.DecodeFunc[clustering.CommunitySummaryRecord]` and has this closed contract:

- Parse the KV key as canonical `{level}.{membership_hash}`: one dot, non-negative base-10 level whose formatted value
  equals the prefix, and one 64-character lowercase hexadecimal SHA-256 membership hash
  (`graph/clustering/storage.go:31-46`; `graph/clustering/summary_store.go:248-254`). A malformed key or noncanonical
  hash is poison.
- Decode JSON exactly once into `clustering.CommunitySummaryRecord`. Invalid JSON is poison. Unknown JSON fields may be
  ignored; the decoder validates the fields it owns rather than predicting future producer fields.
- Require canonical `record.MembershipHash`, and require the delivered key to equal
  `clustering.SummaryKey(record.Level, record.MembershipHash)` exactly. A key/record mismatch is poison.
- Treat status as a closed vocabulary. `SummaryStatusEnhanced` with a non-empty `LLMSummary` is servable
  (`keep=true`). `SummaryStatusFailed` is valid absence (`keep=false`). An unknown status, or enhanced status with an
  empty summary, is poison. The decoder does not invent another status or repair malformed producer data.
- Resolve the existing key with `clustering.SummaryKey(level, MembershipHash(members))`; do not add a second index or
  change the producer key (`processor/graph-query/community_cache.go:373-386`;
  `graph/clustering/summary_store.go:248-255`).
- On a successful point read, use the enhanced summary. On not-ready, watcher-lost, stopped, not-found, poison, or no
  usable record, use `Community.StatisticalSummary` (`processor/graph-query/graphrag.go:2379-2396`).
- Emit a bounded warning on acquisition/watch loss and poison as appropriate. Do not expose these optional-view
  outcomes as query degradation or readiness.

The summary result is a string fallback decision, not another graph-query availability contract. `localSearch`,
`globalSearch`, and `searchGraph` continue to use the Slice C community lease for partition-derived data, but none
acquires a summary generation or validates a summary token before response return.

## Durable ownership

- `COMMUNITY_SUMMARIES` remains a derived bucket owned by `graph-clustering`, content-addressed by membership hash
  (`graph/kvcatalog.go:126-133`). Graph-query opens it through the must-exist reader seam and never creates, reconciles,
  or writes it (`graph/kvcatalog.go:229-245`).
- The enhancement worker remains the sole producer (`graph/clustering/enhancement_worker.go:45-55`). Slice D changes
  only the graph-query projection.
- There is no new port, configuration, schema field, status key, readiness requirement, metric, bucket, stream,
  service, response field, degradation reason, or operational dependency.
- The existing `RecheckInterval` is an internal reuse, not a new summary-specific prediction knob.

## External adopter seam

The specific adopter is a developer outside this repository who configures a SemStreams flow, writes a component, or
calls the admitted graph-query API without reading graph-query internals.

- **What must they know?** Nothing new. Enhanced summaries remain optional; returned summary text may use the
  statistical floor.
- **What happens if they do nothing?** Existing flows start and queries succeed. No config or port migration is
  required. Enhanced text appears once the optional view catches up.
- **Where do they find out?** Existing GraphRAG tier/fallback documentation describes the result contract. Bounded
  component logs diagnose optional-view loss; there is no new operator surface to discover.
- **What SHOULD they have to know?** Ideally nothing about buckets, watchers, retries, hashes, view state, or deadlines.
  Those are framework-owned mechanics.

This is the do-nothing-safe path: an absent, late, rebuilding, failed, stopped, or poisoned optional store cannot cause
startup failure, `index_not_ready`, a new degradation code, or a configuration burden.

## Issue boundaries

| Issue | Slice D disposition |
|---|---|
| #609 | Exact boundary below. |
| #608 | Producer-side summary work; no consumer serving-view expansion. |
| #829 | Producer/content-quality work; no content fetcher or synthesis redesign here. |
| #710 | Content-addressed-summary retention and GC remain a separate, measurement-gated retention design. |
| #820 | No new `GRAPH_STATUS` producer, key, readiness gate, or generic readiness work. |

For #609 exactly: Slice C addressed the `COMMUNITY_INDEX` consumer subset. The remaining producer cold-start
first-ticker delay is separate, and Slice D does not close it.

## Same-class collision disposition

The current raw summary watcher and `pkg/graphview.View` are two implementations of the same semantic class: a
component-owned current-state projection of a KV bucket. Keeping both would preserve the drift this foundation program
is deleting. Slice D therefore removes the summary bucket-presence watcher, once guard, raw KV handle, shared summary
map, stored watcher, and bespoke update/delete/watch methods. It does not lift a new interface or generalize the Slice C
partition supervisor.

## Verification obligations

Tests use explicit synchronization and prove:

1. an absent optional bucket does not block Start or queries, and a late bucket attaches without component restart;
2. replay remains unservable before the sentinel, including a replay containing entries;
3. empty replay becomes a usable empty view and falls back statistically;
4. valid enhanced update, delete, and purge produce the corresponding point-read behavior;
5. invalid JSON is poison for that key and falls back while unrelated records remain available;
6. watcher close is detected without a hot loop; subsequent reads fail closed and use statistical fallback;
7. loss clears the exact synchronized pointer, stops the failed view, reopens the catalog reader, and constructs only
   one replacement after the old view has stopped;
8. a key deleted during the gap is absent after replacement replay and never served as a ghost;
9. a failed initial `View.Start` is stopped before retry, leaving no ticker, watcher, view, or published pointer;
10. orderly component cancellation stops acquisition, view, watcher, and goroutines without retry; and
11. no query response gains `index_not_ready`, degradation metadata, or a new outward-facing contract from summary
   availability.

Focused race tests and real-NATS integration cover the reader and lifecycle. The slice then receives independent
`semstreams-reviewer` approval.

## Stop and remap gates

Stop for owner ruling if implementation evidence shows any of the following:

1. `pkg/graphview.View` cannot represent sentinel-gated replay, close detection, delete/purge, poison, fail-closed point
   reads, restart reconciliation, or orderly Stop without changing its public contract.
2. Content-addressed lookup does not prevent an old-membership summary from matching a new-membership community.
3. Correctness requires a summary generation ID, request lease, final-response validation, readiness/status fact,
   degradation reason, metric contract, config knob, or new infrastructure.
4. Graph-query must create, reconcile, write, or expose a raw mutation-capable handle for `COMMUNITY_SUMMARIES`.
5. The slice reaches producer synthesis, content fetching, retention/GC, clustering partition semantics, or downstream
   implementation.

## Approved Slice D plan

- **D.1** Add explicit-synchronization failing tests for absent and late buckets, replay staging and empty caught-up,
  update/delete/purge, poison, nonblocking loss signaling, failed-Start cleanup, loss/reopen/replacement, ghost removal,
  single-pointer publication, and orderly cancellation.
- **D.2** Replace the bucket-presence watcher, once guard, raw KV handle, shared summary map, and bespoke watcher with
  one component-owned, catalog-backed `pkg/graphview.View` serving projection.
- **D.3** Make subsequent point reads fail closed after view loss. The sole supervisor clears and stops the failed view,
  then reopens and replaces it using the existing recheck interval; use no summary generation ID, request lease, or
  final-response validation.
- **D.4** Preserve statistical fallback for absent, late, staging, empty, failed, stopped, poisoned, and not-found
  summaries without `index_not_ready`, readiness/`GRAPH_STATUS`, degradation metadata, metric contract, config, or new
  infrastructure.
- **D.5** Run focused race and real-NATS integration tests with no arbitrary sleeps and obtain independent review.
