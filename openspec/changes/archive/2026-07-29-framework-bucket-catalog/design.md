# Design: framework bucket catalog + acquisition seam

Architect analysis 2026-07-28 at `8d1a4b77`, condensed; decisions are settled unless marked.

## D1 — Descriptor + catalog

Mechanism in `natsclient` (name-free), population in `graph` (already imports natsclient; imported
by every consumer that needs it — zero new packages, zero new dependency edges).

`natsclient/kvspec.go`:

```go
type BucketClass string   // ClassAuthoritative | ClassDerived | ClassOperational | ClassDiagnostic
type WritePolicy string   // WriteOwnerOnly | WriteOpen
type CreatePosture string // PostureOwnerCreates | PostureReaderMustExist
type RetentionKind string // RetentionNoLifecycle | RetentionBoundedTTL | RetentionUnmanaged

type RetentionPolicy struct { Kind RetentionKind; TTL time.Duration /* BoundedTTL only */ }

type BucketSpec struct {
    Name, Owner, Description string
    Class BucketClass; Retention RetentionPolicy; Write WritePolicy; Posture CreatePosture
    History uint8; Replicas int
}
```

`RetentionPolicy` is Kind+params (NOT a bare enum) so `bounded-storage-operability` later ADDS
`RetentionDiscardNewCeiling{...}` and fills it per-bucket with zero shape/signature change. Every
Kind switch has a default arm that FAILS CLOSED on an unknown Kind. All three shipped Kinds are
populated (no phantom arms): no-lifecycle = the 19 current owned buckets; bounded-ttl =
`OWNER_PRESENCE` (120s, `pkg/ownership/heartbeat.go:22` — proves the field is real and kills the
"framework-owned ⇒ no retention" conflation); unmanaged = `COMPONENT_STATUS`.

`graph/kvcatalog.go`: `KVCatalog() []natsclient.BucketSpec` (the ONE literal, 22 rows — full table
in the proposal-referenced architect census; includes `OWNER_CLAIMS` History 10, `GRAPH_STATUS`
History 3, `ENTITY_STATES` History **1** per owner decision 2026-07-28), `SpecFor(name)`,
and DERIVED `FrameworkOwnedBuckets()`/`IsFrameworkOwnedBucket()` (filter `Write == WriteOwnerOnly`)
keeping today's signatures — rule-guard consumers (`processor/rule/config_validation.go:363`,
`processor/rule/actions.go:1941`) unchanged except the rejection text gains the catalog Owner.
`graph/constants.go:78-100` hand list dies. Owner enforcement is call-site selection (owners call
Ensure, readers call Open) — no runtime identity param; limitation stated in the spec delta.

**COMPONENT_STATUS (#717)**: diagnostic / open / unmanaged. Evidence: 24 production writers, zero
production readers (only `test/e2e/client/nats.go:1633,1723`); `component.KVLifecycleReporter` is
write-only. Write-protecting or retention-guarding it = phantom guard. Future ops TTL = one-line
catalog edit.

Rejected: new package (no edge benefit); init()-registration (the retired payload-registry
singleton class — half-migrated binaries); catalog inside natsclient (product names in the
transport layer); bare enum (bounded-storage forces reshape); unpopulated future Kind (phantom).

## D2 — The seam

```go
func EnsureFrameworkBucket(ctx, c *Client, spec BucketSpec) (jetstream.KeyValue, error)
func OpenFrameworkBucket(ctx, c *Client, spec BucketSpec) (jetstream.KeyValue, error)
```

Ensure: validate spec (unknown Kind → invalid, fail closed) → create-or-open via the existing
`CreateKeyValueBucket` (reuses the proven concurrent-create resolution, `client.go:1226-1243`) →
reconcile retention per Kind (`ReconcileNoLifecycleRetention` reused verbatim; new sibling
`reconcileBoundedTTL` converging MaxAge TO spec TTL, same read→Update→re-read→assert shape;
unmanaged = no-op) → reconcile History to spec on divergence (closes F1; WARN naming both values)
→ verify by fresh re-read (still divergent → fatal) → handle. Errors return to the caller's
`Start`: #719 made that fail boot closed / health-visible — that composition is WHY the post-start
pass can die.

Open: `GetKeyValueBucket` only; NEVER creates; absent → classified not-ready error carrying the
catalog Owner (the `gtypes.ErrorCodeIndexNotReady` shape graph/query already emits). Deliberately
does NOT reconcile (reader mutation of stream config is the same bug class as reader creation).

### Migration slice (one PR, owner-decided; sequencing INSIDE the PR: seam → migrate all → prove →
delete pass — deleting the guard promotes the seam to load-bearing, so it goes last)

- **Tier 1, 17 owner sites**: graph-ingest `component.go:1118,1150,1178` (+ DELETE the at-creation
  asserts `:1128-1133,:1186-1192`); graph-index `:731` (port loop — subjects resolve through
  `graph.SpecFor`, unresolved → boot failure naming the subject: F2), `:745,:757`; spatial `:470`;
  temporal `:479,:494`; graph-embedding `:879,:888`; graph-clustering `:933,:947` +
  `structural.go:19` + `anomaly.go:165`; `graph/readiness/publisher.go:66` (delegates to seam; its
  20-line adoption-mitigation comment `:44-63` dies); `pkg/ownership/bootstrap.go:57,66`
  (bounded-ttl live proof).
- **Tier 2, reader class (#714)**: `register_graph_query.go:52` → Open (deletes
  `entityStatesBucketConfig` + the factually-wrong 12-line sync comment `:19-30`; keep its
  warn-and-skip miss posture); `graph/query/client.go:191-236` ensureBuckets → 3 Opens (lazy
  acquisition verified — called from GetEntity/ListEntities `:343,:418`, never NewClient; deletes
  Config bucket-structs `:41-58` + defaults `:70-96` + `doc.go:107-122` +
  `retention_guardrail_test.go` whole); `processor/graph-query/component.go:365` (raw
  `CreateOrUpdateKeyValue` bypassing the wrapper AND its circuit breaker) → seam; bare-literal
  must-exist readers onto SpecFor: `processor/rule/entity_watcher.go:960`,
  `processor/gated-dag/executor.go:367` (+const `:17`), `service/graph_triples_http.go:182`
  (+const `:34`).
- **Tier 3, COMPONENT_STATUS ×24** (owner: one PR): the 21 bare-literal sites in the architect
  census (inputs udp/file; processors json_filter/json_map/json_generic, five research-graph-*,
  graph-index `:805`, graph-ingest `:1382`, graph-embedding `:920`, rule `:876`; outputs
  httppost/websocket/file; objectstore `:232`; gateways http `:126`, graph-gateway `:579`,
  graph-query `:365`) + 3 constant users.
- **Tier 4, shadow catalogs DELETED**: `graph/embedding/storage.go:19,22`,
  `graph/clustering/storage.go:22`, `graph/structural/storage.go:18`,
  `graph/inference/storage.go:22` + phantom operator knob `graph/inference/config.go:234`,
  `test/e2e/client/nats.go:65,455,657,1616` → import from graph.
- **Deferred (boundary stated in spec)**: app/product buckets keep plain CreateKeyValueBucket —
  the catalog covers only framework-guaranteed write-ownership/retention.

## D3 — Sweep demotion

DELETE the post-start pass + rationale (`service/service_manager.go:299-321`): its entire justified
class (created-dirty during this boot) is now reconciled at creation inside each owner's Start —
earlier and more precisely. KEEP the pre-start pass (`service/ownership_service.go:149`) demoted to
a legacy-drift backstop with the honest justification: a catalog bucket whose OWNER IS NOT DEPLOYED
in this composition never has its seam called (e.g. `EMBEDDING_INDEX` left by a prior semantic
deploy, booting statistical) — one boot-time catalog pass is the right instrument for exactly that.
Spec truth: graph-retention's "two-pass" requirement is rewritten (seam-primary + single backstop);
the "barrier is load-bearing for the guarantee" sentence is REWRITTEN, not relocated — the barrier
stays load-bearing for #719's fail-closed boot, no longer for retention coverage. Rejected: keeping
both passes "for safety" — a pure ratchet; the only class it would cover (mid-boot foreign
re-dirtying after the seam call) is already declared out of scope by the spec's boot-time posture.

## D4 — Guard + diagnostics

Write guard derives (`Write == WriteOwnerOnly`); rejection text gains Owner. Diagnostics: NONE
(zero consumers; the WARN + fail-closed boot have teeth). Flip condition recorded: when
bounded-storage's operator inventory lands, the catalog is its data source and THAT change owns the
surface.

## Compose/risk notes

- History reconcile DOWN discards revisions (destructive): only ENTITY_STATES-with-tool-race
  deploys are affected, nothing reads the depth, WARN names old→new. Owner accepted (History=1).
- `graph/query` behavioral break: readers fail not-ready instead of creating; sisters verified
  compile-clean (none set the removed fields; semmem/semsage DefaultConfig, semsource nil); adopter
  note on the established sole-channel pattern.
- Do NOT soften F2's boot failure to warn-then-fail (warn-not-fail masks drift).
- Schema drift expected: `graph-query.v1.json` (Config fields die) — regenerate + commit.
- Framework packages touched (natsclient, graph, service) ⇒ full `-race -tags=integration ./...`.

## Test plan (verbatim from the architect; all red-first where orderable, stash for
fails-without-fix, no sleeps)

1. **Post-boot-cutoff proof** (the discharged spec clause; integration, production wire): boot
   fully → out-of-band `UpdateStream MaxAge=1h` on `KV_EMBEDDING_INDEX` → post-boot dynamic
   component EDIT through the real config watcher restarting the owner → TTL stripped + WARN, with
   NO sweep having run.
2. **Policy is real**: Ensure preserves OWNER_PRESENCE's 120s TTL; strips EMBEDDING_INDEX's —
   same seam, opposite outcomes.
3. **Fail-closed default arm**: unknown RetentionKind → invalid error, never a silent no-op.
4. **F1 regression**: tool-registration path first, then graph-ingest boot → History equals
   catalog; then the reverse order → same. RED on today's code in one order.
5. **#714 closure**: Open on absent bucket → classified not-ready naming the Owner AND the bucket
   is STILL ABSENT after (assert non-creation explicitly).
6. **F2 closure**: graph-index with an off-catalog output subject fails boot naming it.
7. **Derivation, not snapshot**: a fixture WriteOwnerOnly entry appears in FrameworkOwnedBuckets()
   AND is rejected by both rule guards.
8. **Contract test** (the highest-leverage artifact): no non-test file outside the catalog names a
   catalog bucket in a KeyValueConfig or acquisition call — replaces "review keeps catching missing
   members" with a mechanism.
9. Rewrites: `graph/owned_bucket_retention_integration_test.go` OrderedCreateRace → seam;
   `service/framework_owned_bucket_guards_integration_test.go` shrinks to the backstop;
   `graph/query/retention_guardrail_test.go` deleted.
