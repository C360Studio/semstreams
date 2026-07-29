# Tasks: framework-bucket-catalog

## 1. Verification (DONE at design time, 2026-07-28 — architect census at `8d1a4b77`)

- [x] 1.1 **F1 verified live**: `ENTITY_STATES` History race — graph-ingest creates default-1
  (`processor/graph-ingest/component.go:1118`), tool-registration creates History-3
  (`processor/agentic-tools/executors/register_graph_query.go:52`, config `:31-35`); adoption never
  compares (`natsclient/client.go:1219-1225`); the sync comment at `:19-30` is factually wrong
  (mirrors graph/query, not the owner). **Owner decision: catalog declares History=1** (nothing
  reads deeper — only `History()` consumer is Lifecycle's workflow buckets; reconcile-down is
  destructive-but-unread, WARN names it).
- [x] 1.2 **F2 verified live**: four owned buckets created from operator config strings
  (`processor/graph-index/component.go:729-742` over port subjects) with zero validation.
- [x] 1.3 **#717 evidence**: COMPONENT_STATUS has 24 production writers, ZERO production readers
  (only `test/e2e/client/nats.go:1633,1723`); `component.KVLifecycleReporter` is write-only.
  Classification: diagnostic / write-open / retention-unmanaged.
- [x] 1.4 Owner decisions (2026-07-28): History=1; Tier 3 (24 COMPONENT_STATUS sites) in ONE PR;
  e2e-client shadow constants in scope.

## 2. Foundation (TDD where orderable)

- [x] 2.1 `natsclient/kvspec.go`: BucketSpec + enums + RetentionPolicy (Kind+params); unit tests
  incl. the fail-closed unknown-Kind arm (red-first: assert an unknown Kind errors, never no-ops).
  DONE — `Validate()` fails closed on unknown Kind at BOTH seams plus a kept-defensive default
  arm inside Ensure's reconcile switch; unit tests in `natsclient/kvspec_test.go`.
- [x] 2.2 `EnsureFrameworkBucket`: DONE — reuses `CreateKeyValueBucket` (concurrent-create
  resolution untouched) → per-Kind reconcile (`ReconcileNoLifecycleRetention` verbatim; new
  sibling `reconcileBoundedTTL` same read→Update→re-read→assert shape; unmanaged no-op) →
  `reconcileHistory` (UpdateStream MaxMsgsPerSubject, WARN both values, re-read, fatal on
  divergence). Seam-mechanics integration tests (fixture specs) in
  `natsclient/kvspec_integration_test.go`; catalog-driven policy-is-real pair
  (OWNER_PRESENCE 120s preserved / EMBEDDING_INDEX foreign TTL stripped) in
  `graph/kvcatalog_integration_test.go`.
- [x] 2.3 `OpenFrameworkBucket`: DONE — Get only, never creates/reconciles; absent → classified
  `errs.ClassifiedCode(Transient, "index_not_ready")` naming spec.Owner
  (`natsclient.ErrorCodeBucketNotReady`, value-pinned to `graph.ErrorCodeIndexNotReady` by a
  cross-pin test). Still-absent-after-miss asserted in natsclient AND graph integration tests.
- [x] 2.4 `graph/kvcatalog.go`: DONE — 22 rows (ENTITY_STATES History=1, GRAPH_STATUS 3,
  OWNER_CLAIMS 10, OWNER_PRESENCE bounded-ttl 120s, COMPONENT_STATUS diagnostic/open/unmanaged),
  `SpecFor`, derived `FrameworkOwnedBuckets`/`IsFrameworkOwnedBucket` (signatures preserved,
  bodies derived; hand list at `graph/constants.go:78-111` deleted). Derived owned set is now 21
  (OWNER_CLAIMS + OWNER_PRESENCE join via write-policy derivation). Derivation tests: fixture
  entry through the unexported `frameworkOwnedFrom` helper (`graph/kvcatalog_test.go`) AND the
  newly-derived OWNER_CLAIMS/OWNER_PRESENCE rejected by both production rule guards
  (`processor/rule/operational_bucket_ownership_test.go`). Also added name-resolving
  conveniences `EnsureCatalogBucket`/`OpenCatalogBucket` + `OwnerOf` (rejection text).
- [x] 2.5 F1 regression test: DONE —
  `TestIntegration_EntityStatesHistory_NoLongerDecidedByBootOrder` (executors pkg): legacy
  History-3 create then owner seam → History 1 + WARN; owner first then production
  RegisterBuiltins(graph_query) → History stays 1. RED observed on pre-change HEAD via the
  reader-creates test (`Created new KV bucket bucket=ENTITY_STATES` then FAIL on the
  non-creation assert) — no checkout/stash used; the red test compiled against HEAD directly.

## 3. Migration — Tier 1 (17 owner sites → Ensure)

- [x] 3.1 graph-ingest: DONE — 3 sites → `graph.EnsureCatalogBucket`; both at-creation asserts
  DELETED (the seam reconciles + fails closed at the same point).
- [x] 3.2 graph-index: DONE — port loop resolves via `graph.SpecFor`, unresolved →
  `WrapInvalid` naming port + subject; CONTEXT/NAME via the catalog. **F2 closure test
  red-first**: `TestIntegration_Start_OffCatalogOutputSubjectFailsBoot` observed RED on HEAD
  (`Created new KV bucket bucket=OUTGOING_INDEX_TYPO`, Start succeeded); NOTE the reachable F2
  hole is an ADDITIONAL off-catalog output port — Config.Validate already rejects a MISSING
  required index.
- [x] 3.3 spatial/temporal/graph-embedding/graph-clustering (incl. structural.go + anomaly.go):
  DONE — all 8 sites → `graph.EnsureCatalogBucket`.
- [x] 3.4 DONE — `readiness.EnsureBucket` now takes `*natsclient.Client` and delegates to the
  seam; `BucketCreator` iface + `bucketDescription` + `BucketHistory` const + the
  adoption-mitigation comment DELETED (History 3 lives on the catalog row; the two former
  fake-creator unit tests replaced by a catalog-shape pin + nil-client test).
  `pkg/ownership.EnsureBuckets` → seam via catalog (ownerClaimsHistory const deleted — History 10
  on the row); `buckets.go` names re-export `graph.BucketOwnerClaims/BucketOwnerPresence`;
  `claim_reader.go` migrated to the reader seam (a constant-based must-exist reader the census
  missed — same class as 4.4). OWNER_PRESENCE TTL literal lives on the catalog row; cross-pin
  test in pkg/ownership asserts it equals `ownership.PresenceTTL`.

## 4. Migration — Tier 2 (reader class, closes #714)

- [x] 4.1 DONE — `registerGraphQuery` → `graph.OpenCatalogBucket` under `retry.Quick`;
  `entityStatesBucketConfig` + the factually-wrong sync comment + the `entityStatesBucket` const
  + `register_graph_query_ttl_test.go` (which pinned the deleted config) DELETED; warn-and-skip
  posture kept. Red-first observation recorded under 2.5.
- [x] 4.2 DONE — ensureBuckets → 3 `OpenCatalogBucket` binds; Config bucket-structs + defaults +
  doc.go bucket section + `retention_guardrail_test.go` DELETED; `client_test.go` DefaultConfig
  assertions rewritten (no bucket config to assert on — the structural point).
- [x] 4.3 DONE — graph-query's raw `js.CreateOrUpdateKeyValue` → the shared
  `component.NewCatalogLifecycleReporter` (wrapper + circuit breaker back in the path); the
  component keeps a `rawNATSClient *natsclient.Client` field because its `natsClient` field is a
  narrow test interface.
- [x] 4.4 DONE — entity_watcher / gated-dag (+const) / graph_triples_http (+const) →
  `OpenCatalogBucket`; also fixed graph-query's `"COMMUNITY_INDEX"` watcher-label literal →
  constant.

## 5. Migration — Tier 3 (COMPONENT_STATUS ×24, owner-decided one PR) + Tier 4 (shadow catalogs)

- [x] 5.1 DONE — ONE shared helper `component.NewCatalogLifecycleReporter(ctx, nc, name, logger)`
  (component may import graph: no cycle — graph imports message/natsclient/errs/vocabulary only);
  all 24 sites (21 bare-literal + 3 constant users) collapsed to one-liners. Judgment note:
  the helper is new exported surface in `component`, chosen over 24 four-line seam blocks
  (≈ −240 LOC); graph-clustering's reporter was previously UNthrottled
  (`NewKVLifecycleReporter`) and is now throttled like the other 23 — reviewer should confirm.
- [x] 5.2 DONE — shadow constants deleted (embedding ×2, clustering CommunityBucket, structural,
  inference DefaultAnomalyBucket); phantom `StorageConfig.BucketName` knob deleted (field,
  default, validation, merge — schema drift lands in `graph-clustering.v1.json`, NOT
  graph-query.v1.json; see 8.3); `test/e2e/client/nats.go` constants + IndexBuckets values now
  re-export `graph.Bucket*`.

## 6. Sweep demotion (LAST inside the PR — deleting the guard promotes the seam to load-bearing)

- [x] 6.1 DONE — post-start pass + its 17-line rationale deleted from `Manager.StartAll`; replaced
  by a short comment stating WHY none is needed (seam reconciles at creation inside each owner's
  Start; barrier fails boot closed). Done strictly AFTER sections 2–5 (every site on the seam).
- [x] 6.2 DONE — backstop now ranges the catalog's NO-LIFECYCLE descriptors (not the owned set —
  it must never strip OWNER_PRESENCE's declared bounded TTL); comments in both
  `graph/owned_bucket_retention.go` and `WireOwnership` state the ONE honest job (catalog bucket
  whose owner is absent from this composition). NOTE: the iteration-set retarget happened
  immediately with the catalog landing (before 6.1) — required, or the pre-start pass would have
  stripped OWNER_PRESENCE's TTL mid-PR.
- [x] 6.3 DONE — OrderedCreateRace → `TestIntegration_OrderedCreateRace_SeamReconcilesAtAcquisition`
  (backstop skips absent → rival dirty create → owner seam strips at acquisition; WARN pinning
  moved to natsclient seam tests where the client logger is injectable); guards file shrinks to
  the seam-inside-boot test + boot-fails-closed (skip-probe canary machinery deleted with the
  sweep); boot-drain MidBootComponent test rewritten to seam semantics (dirty create + seam
  acquire inside the late owner's Start). Sweep wording in retention tests → "backstop".
- [x] 6.4 DONE — `service/post_boot_seam_reconcile_integration_test.go`
  `TestIntegration_PostBootDynamicEditReconcilesBucketAtSeam`: real graph-embedding (bm25)
  through the bootDrainHarness (real config.Manager KV wire, watch_config) → StartAll → out-of-
  band UpdateStream MaxAge=1h on KV_EMBEDDING_INDEX → component EDIT via semstreams_config KV
  put → watcher restarts graph-embedding → TTL stripped + WARN (recording default logger,
  installed pre-client so natsclient captures it) with the WARN timestamp proven AFTER StartAll
  returned (no boot pass could have done it; StartAll has none). SURFACED + FIXED a latent data
  race: `stopAllComponents` cancelled/nil'd `mc.Context` outside `cm.mu` while the health loop
  reads it under RLock (`component_manager.go`, cancel pass now under the write lock).

## 7. Contract + docs

- [x] 7.1 DONE — `test/contract/kvcatalog_literal_contract_test.go`: go/parser walk of every
  non-test .go file (allowlist: graph/constants.go, graph/kvcatalog.go, test/ tree); ANY string
  literal exactly equal to a catalog bucket name fails, naming file:line. Stricter than the
  KeyValueConfig/acquisition-call scope (comments can't false-positive — AST literals only);
  proven discriminating with a deliberate probe const (caught, then clean after removal).
- [x] 7.2 DONE — `docs/operations/framework-bucket-catalog.md`: not-ready-instead-of-create (error
  text + code), removed Config fields, off-catalog boot failure, History 3→1 reconcile WARN +
  capture-before-upgrade caveat, grep guidance, owner/reader/app-bucket rules of thumb.
- [x] 7.3 `openspec validate --strict` clean; implementation walked against every delta scenario
  (see PR notes; scenario-by-scenario mapping in the implementation report).

## 8. Gates (GOFLAGS=-mod=readonly; go.mod/go.sum byte-clean after every stage)

- [x] 8.1 DONE — `task lint` clean (2 revive package-comment warnings fixed); `go vet ./...` +
  `-tags=integration` + `-tags=live_llm` clean (the tagged vet caught 5 shadow-constant test
  users — fixed to catalog references); `gofmt -l` clean; full `go test -race ./...` zero FAIL;
  new unit tests 3× stable (natsclient, graph, readiness, query, ownership, rule, component,
  contract at -count=3). go.mod/go.sum restored from HEAD after every stage (gopls kept
  re-adding a semsource indirect + oauth2 bump).
- [x] 8.2 DONE — full `go test -race -tags=integration ./...` (Docker): first pass surfaced
  (a) expected reader-fallout in graph/query + graph-index tests (clients must provision as
  owners now — fixed), (b) graph-ingest's TTL-guard test pinning the RETIRED fail-on-strippable
  posture (rewritten: seam self-heals, stamp survives; unstrippable arm stays pinned at the
  natsclient unit level), (c) one testcontainers infra symptom in graph/clustering ("failed to
  get mapped port: port 4222 not found" under peak concurrent container startup — green
  standalone and on the rerun), and (d) a LATENT DATA RACE in ComponentManager stop-vs-health
  (fixed, see 6.4). Clean rerun: 136 packages ok, ZERO FAIL. New integration tests
  (seam mechanics, catalog policy pair, F1 both-orders, #714, F2, StartAll seam, mid-boot
  drain, proof test, ingest-guard reconcile) all 3× stable under -race.
- [x] 8.3 `task schema:generate` — ACTUAL drift: `schemas/graph-clustering.v1.json`
  (`storage.bucket_name` dies with the phantom knob), committed. The design's expected
  `graph-query.v1.json` drift did NOT materialize — `graph/query.Config` was never part of the
  generated component schema (the processor's Config doesn't embed it); deviation recorded.
  `go test ./test/contract/...` green (incl. the new catalog-literal scan).
- [ ] 8.4 **BREAKING ⇒ e2e before merge: `task e2e:structural` AND `task e2e:statistical`** green
  (structural = F2 path + write guard + query; statistical = embedding/community owners + the
  COMPONENT_STATUS mass migration); `e2e:core` free — run it.

## 9. PR + review + merge

- [ ] 9.1 Branch off main; conventional commit, BREAKING flagged; PR body carries F1/F2, the
  #717 answer, the net-deletion ledger, and the bounded-storage rebase note.
- [ ] 9.2 `semstreams-reviewer` pre-merge — explicit lenses: catalog census completeness
  (guarantee-defines-hole-class); seam failure postures; the sweep-deletion sequencing (seam →
  migrate → prove → delete); contract-test enforceability; derivation-not-snapshot.
- [ ] 9.3 Owner-run Codex gate; merge on addressed + CI-green; archive + baton (record: Epic C
  structural leg COMPLETE; #712 next; bounded-storage rebases).
