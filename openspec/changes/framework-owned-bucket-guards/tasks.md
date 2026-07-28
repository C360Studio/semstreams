## 1. Write-ownership fix (closes the live `update_kv` hole)

- [x] 1.1 Add `BucketEntitySuffixIndex = "ENTITY_SUFFIX_INDEX"` to `graph/constants.go` and add it to `FrameworkOwnedBuckets()`.
- [x] 1.2 Replace the bare `"ENTITY_SUFFIX_INDEX"` literal at `processor/graph-ingest/component.go:1154` with `graph.BucketEntitySuffixIndex`.
- [x] 1.3 Audit both `IsFrameworkOwnedBucket` consumers (`processor/rule/config_validation.go:363`, `processor/rule/actions.go:1941`) — confirm the new entry closes the hole at load and runtime with no other-site side effects. Confirm no shipped config in `configs/` writes `ENTITY_SUFFIX_INDEX` (additive, non-breaking). (Verified: both guard sites call `gtypes.IsFrameworkOwnedBucket(bucket)` and gain coverage automatically; `grep -rn ENTITY_SUFFIX_INDEX configs/` → no matches.)
- [x] 1.4 Failing-first unit test: a rule `update_kv` targeting `ENTITY_SUFFIX_INDEX` is rejected at validation and runtime; a non-owned bucket is still permitted (negative control). Test must fail before 1.1. (`processor/rule/entity_suffix_index_ownership_test.go`; confirmed RED before 1.1 — load + runtime both failed — then GREEN.)

## 2. KV reconcile-then-assert atom

- [x] 2.1 Add `natsclient.ReconcileNoLifecycleRetention(ctx, bucket)` mirroring `storage/objectstore/retention.go`: inspect the KV backing stream `KV_<bucket>`, strip a binding `MaxAge`/`MaxBytes` via `UpdateStream` + WARN (naming bucket + removed retention), re-read fresh, then assert via the shared `natsclient.CheckNoLifecycleRetention` predicate; fail closed only if still binding. Delete no keys. (`natsclient/kv_retention.go`; signature `ReconcileNoLifecycleRetention(ctx, js jetstream.JetStream, bucket string, logger *slog.Logger)` — takes `js`+`logger` to mirror the ObjectStore free-function precedent exactly, `KV_` prefix vs `OBJ_`.)
- [x] 2.2 Unit-test the atom against a fake/real backing stream: (a) clean → pass no-op; (b) foreign `MaxAge` → stripped + warned + passes; (c) unstrippable binding → fatal error naming the bucket. Assert no key deletion in (b). (`natsclient/kv_retention_test.go` `TestReconcileNoLifecycleRetention_KV`, fake JS harness mirroring objectstore; the no-key-deletion assertion is exercised end-to-end against real NATS in 4.1 where the atom strips a TTL and the stored key survives.)

## 3. Authoritative owned-bucket retention sweep + boot wiring

- [x] 3.1 Add `graph.retentionGuardedBuckets()` = `FrameworkOwnedBuckets()` minus `EMBEDDINGS_CACHE`, with a doc comment recording the exclusion rationale (rebuildable cache; capacity policy owned by `bounded-storage-operability`). (`graph/owned_bucket_retention.go`.)
- [x] 3.2 Add `graph.AssertOwnedBucketsClean(ctx, client, logger)` that ranges `retentionGuardedBuckets()`, binds each read-only / must-exist / skip-if-absent, and calls the reconcile atom. A missing bucket is skipped (no creation, no ordering dependency); an unfixable one aborts boot. (`graph/owned_bucket_retention.go`; skip-if-absent via `client.GetKeyValueBucket` → `jetstream.ErrBucketNotFound`; nil-client returns an invalid-class error rather than panicking.)
- [x] 3.3 Wire `AssertOwnedBucketsClean` once at a single deterministic boot seam (resolve Open Question 1: ownership-service boot path vs a graph-boot helper) so it runs before rule evaluation depends on the buckets. Keep graph-ingest's existing at-creation asserts as create-race belt-and-suspenders. (OQ1 RESOLVED: wired inside `service.WireOwnership` — the one shared pre-StartAll seam BOTH `cmd/semstreams` and `cmd/e2e-semstreams` call exactly once, so no half-migration drift; graph-ingest's two at-creation asserts kept as-is. Reviewer-adjustable.)
- [ ] 3.4 (Open Question 2 — confirm at review before doing) optionally migrate graph-ingest's two `AssertNoLifecycleRetention` sites (`component.go:1135`, `:1195`) onto the reconcile atom for one guard behavior. Flag the ENTITY_STATES refuse→strip behavior change to the reviewer. (DELIBERATELY LEFT UNDONE per OQ2 — a visible refuse→strip behavior change on ENTITY_STATES; recommend to reviewer but not applied in this pass.)

## 4. Integration coverage (real NATS)

- [x] 4.1 Integration test (`//go:build integration`, real NATS): pre-create a derived owned bucket (e.g. `EMBEDDING_INDEX`) with a foreign `MaxAge`, run the sweep, assert the TTL is stripped, a WARN is emitted, and no key is lost — reproduces the #610/#611 shape and proves boot self-heals it. (`graph/owned_bucket_retention_integration_test.go` `TestIntegration_AssertOwnedBucketsClean_StripsForeignTTL`; also added `_SkipsAbsentBuckets` proving resourceless-deploy safety.)
- [x] 4.2 Integration test: `EMBEDDINGS_CACHE` with a TTL is NOT asserted/stripped by the sweep (excluded) yet remains write-ownership-protected. (`TestIntegration_AssertOwnedBucketsClean_ExcludesEmbeddingsCache`.)
- [ ] 4.3 Integration test: an unstrippable binding retention on an owned bucket fails boot fast with the bucket named. (NOT a real-NATS test — a denied `UpdateStream` is not deterministically reachable against cooperative NATS, so the strip always takes. The fail-closed-naming-the-bucket path is proven at the atom UNIT level — `TestReconcileNoLifecycleRetention_KV` "a denied strip fails closed naming the bucket" — driving the exact function the sweep calls. This mirrors the ObjectStore precedent's identical documented decision. Documented in the integration test file; left unchecked because the literal "integration test" wording is not satisfied.)

## 5. Gates (mirror CI; all must pass before PR)

- [x] 5.1 `go build ./...`; `go vet ./...` plain + `-tags=integration` + `-tags=live_llm`. (All clean.)
- [x] 5.2 `task lint` clean (revive warnings = fail); `go test -race ./...` (0 FAIL). (Both green; 2 regressions found + fixed: nil-client panic in the sweep, aliased-import contract violation.)
- [x] 5.3 `task schema:generate` then `git diff schemas/ specs/` shows no drift (commit if any); `go test ./test/contract/...`. (No drift; contract tests pass.)
- [x] 5.4 Tagged integration `-race -tags=integration` on touched packages (`natsclient`, `graph`, `processor/graph-ingest`, `processor/rule`). (All 4 green.)
- [x] 5.5 Prudent (additive, not obligatory): `task e2e:core` green — exercises the graph boot path. (2/2 scenarios PASSED, 8/8 components healthy — real boot path now runs the sweep via WireOwnership.)

## 6. Constraint & scope verification

- [x] 6.1 Verify zero predicate/vocabulary surface: no read/write/validate/register of any predicate or vocabulary entry; the guard keys only on bucket name + backing-stream retention config. (Do not touch the predicate validation adjacent to `config_validation.go:357`.) (Verified: `config_validation.go` untouched by this change; the guard keys only on bucket name + `KV_<bucket>` MaxAge/MaxBytes.)
- [x] 6.2 Confirm no `MaxBytes`/`DiscardNew` policy is introduced — this change enforces the no-lifecycle-retention status quo only; the emergency-ceiling carve-out stays deferred to `bounded-storage-operability`. (Verified: the atom STRIPS MaxBytes to the -1 unlimited sentinel; it introduces no cap and no `DiscardNew`.)
- [x] 6.3 Verify the ObjectStore analogue (#622 ask 3) coverage still holds and close #622 ask 3 as already-implemented (`storage/objectstore/retention.go`). (Verified: `storage/objectstore/retention.go` untouched and its tests still green; #622 ask 3 issue-close is an owner action — flagged in handoff.)
- [x] 6.4 File a follow-up issue for the reader-creates-owned-bucket anti-pattern (graph-query get-or-creates ENTITY_STATES/SPATIAL/INCOMING) so the single-writer thesis #629 relies on is tracked. (Filed **#714**; the review also surfaced the `GRAPH_INGEST_APPLIED_SEQ` write-ownership gap → filed **#715**.)

## 7. Spec, docs, and closeout

- [x] 7.1 `openspec validate framework-owned-bucket-guards --strict` passes. (Valid.)
- [x] 7.2 Confirm the `graph-retention` delta reads correctly against the implemented behavior; resolve Open Question 3 (write-ownership requirement home: `graph-retention` vs `nats-kv-keys`) at review. (Reviewer confirmed the delta matches the implemented behavior and resolved OQ3: keep `graph-retention` for inc-0; extract a dedicated `framework-owned-buckets` capability only if #625/#629 bring a third guard.)
- [ ] 7.3 semstreams-reviewer pass → address findings → Codex (owner-run) → CI green → merge → `openspec archive`. Update the baton (#622 → done; #625/#629 pair next).
