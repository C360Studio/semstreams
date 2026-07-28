# Tasks: reopen framework-owned-bucket-guards

## 1. Pre-implementation verification sweeps (gate the deletions)

- [x] 1.1 Grep sisters (`../semsource`, `../semconnect`, `../semboids`, `../semspec`) for
  `EMBEDDINGS_CACHE` / `BucketEmbeddingsCache` — config declarations, direct KV reads/writes. Record
  findings in the PR. A real consumer ⇒ STOP and surface to owner before deleting (a reader of a
  never-written bucket is vestigial, but it gets surfaced, not silently broken).
  **FINDING (2026-07-28, CORRECTED in the Codex round — the original sweep filtered .json hits as
  scorecard noise and under-reported): no reader/writer of the bucket in any sister. Sister-facing
  surface identified by the corrected sweep: semsource — `cmd/semsource/run.go` embeddings output
  declaration, `test/e2e/beta148_cutover_test.go` framework-bucket parity literal,
  `configs/tiers/README.md` stale prose; semspec — 12 checked-in configs under `configs/` carrying
  both the EMBEDDINGS_CACHE output entry and `cache_ttl`. We do not touch sister repos: migration is
  communicated via docs/operations/embeddings-cache-removal.md (the sole downstream migration
  channel) and sisters handle it internally on their next semstreams bump.**
- [x] 1.2 Grep semstreams + sisters for `RegisterComponentErrorHook` callers. No consumer ⇒ task 3.4
  deletes it; a consumer ⇒ keep it and wire boot propagation around it, document which.
  **FINDING (2026-07-28): zero callers anywhere (semstreams non-test + all sisters) for
  RegisterComponentErrorHook, RegisterComponentStartHook, RegisterComponentStopHook. Task 3.4
  deletes; developer verifies each hook (incl. RegisterHealthChangeHook) before deleting.**
- [x] 1.3 Grep in-repo configs (YAML/JSON under `docker/`, `test/`, `examples/`, e2e fixtures) for
  `EMBEDDINGS_CACHE` output declarations; list every file task 4 must update.
  **FINDING (2026-07-28): configs/semantic.json, configs/semantic-8b.json,
  configs/semantic-frontier.json, configs/statistical.json; Go refs (incl. tests):
  processor/graph-embedding/{component.go,doc.go,component_test.go,workers_config_test.go,
  max_text_len_bound_test.go,max_text_len_config_test.go}, graph/{constants.go,
  owned_bucket_retention.go,owned_bucket_retention_test.go,owned_bucket_retention_integration_test.go,
  clustering/main_test.go,structural/main_test.go}, gateway/graph-gateway/doc.go.**

## 2. Component-start barrier + boot failure propagation (`service/`)

- [x] 2.1 Failing test first: a component whose `Start` errors ⇒ `ComponentManager.Start` returns an
  error naming it (drive the real async launch path; must fail on current main).
- [x] 2.2 Failing test first: `ComponentManager.Start` does not return until every launched component
  `Start` has returned (explicit synchronization — e.g. a gated component that blocks `Start` until
  released — no sleeps).
- [x] 2.3 Implement the barrier in `startAllComponents`/`Start`: batch-scoped WaitGroup (do NOT reuse
  `cm.wg`, which tracks long-lived loops), collect per-component errors, return `errors.Join`. Multiple
  failures all named.
- [x] 2.4 `Manager.StartAll` propagates the failure (it already returns on service `Start` error —
  verify the joined error surfaces and HTTP setup is never reached); update the post-start sweep seam
  comment at `service/service_manager.go:299-311` to state the barrier ordering truthfully.
- [x] 2.5 Delete the fire-and-forget semantics cleanly: no flag, no `StartAsync` compatibility variant,
  no deprecated path retained.

## 3. Health truth (`StateFailed` visible)

- [x] 3.1 Failing test first: a component in `StateFailed` ⇒ `performDetailedHealthCheck` returns an
  error naming the component and its `LastError`; recovery (restart to `StateStarted`) clears it.
- [x] 3.2 Implement the `StateFailed` check in `performDetailedHealthCheck` (keep the TryRLock
  never-block posture, gh#508).
- [x] 3.3 Verify post-boot dynamic start/restart failure paths (`component_manager.go:613,1708` region)
  record `StateFailed` + `LastError` and do not crash the process.
- [x] 3.4 Per task 1.2 finding: delete `RegisterComponentErrorHook` + `onComponentError` field (and the
  start/stop hooks IF they are equally unconsumed — check each; delete only what has no consumer), or
  wire the surviving hook for real. No unwired exported hooks remain either way.
  **Deleted all four: RegisterComponentErrorHook/onComponentError, RegisterComponentStartHook/
  onComponentStart, RegisterComponentStopHook/onComponentStop, RegisterHealthChangeHook/onHealthChange
  (ComponentManager's; the distinct BaseService/natsclient onHealthChange fields have real internal
  consumers and remain). Re-verified zero callers in semstreams + all four sisters, incl. the
  internal invocation sites. No test used them for synchronization.**

## 4. Delete the `EMBEDDINGS_CACHE` surface (BREAKING)

- [x] 4.1 Delete `graph.BucketEmbeddingsCache` and its `FrameworkOwnedBuckets()` membership
  (`graph/constants.go`); delete `retentionGuardedBuckets()` — the sweep ranges
  `FrameworkOwnedBuckets()` directly (`graph/owned_bucket_retention.go`).
- [x] 4.2 Delete the bucket creation + `embeddingBucket` field
  (`processor/graph-embedding/component.go:296,649-661`), the required-output validation
  (`:106-115`), and the default-config output entries (`:217,244`).
  **Also removed the generic "at least one output port required" check: with the cache output gone
  the component legitimately declares no outputs (its durable writes are direct bucket writes), and
  the updated configs would otherwise fail validation. Review round 2: also deleted the `cache_ttl`
  phantom knob (field, validation, accessor, defaults, shipped-config entries, doc examples) — zero
  non-test consumers on this branch AND origin/main; schema drift (cache_ttl entry removed from
  schemas/graph-embedding.v1.json) kept in tree.**
- [x] 4.3 Update every config found in task 1.3; purge stale references in
  `processor/graph-embedding/doc.go`, `gateway/graph-gateway/doc.go` + `README.md`, and any
  `docs/concepts/` mentions.
  **docs/concepts/ has no EMBEDDINGS_CACHE mentions (grep clean). Additionally updated
  gateway/graph-gateway/TEST_REQUIREMENTS.md, processor/graph-embedding/README.md, and
  configs/README.md — same stale-reference class as the listed files. Review round 2: purged
  docs/basics/02-architecture.md, docs/basics/06-configuration.md, docs/advanced/07-graph-components.md
  (ownership tables, config examples, prose). docs/adr + docs/proposals history and
  docs/operations/29-entity-id-contract-clean-cutover.md left as history per reviewer.**
- [x] 4.4 `task schema:generate`; commit drift.
  **Ran clean — no drift (Config struct fields unchanged; only validation/default behavior changed).**
- [x] 4.5 Adopter note (`docs/operations/`): config migration (drop the output), and that an orphaned
  `KV_EMBEDDINGS_CACHE` bucket in an existing deployment is inert and may be manually deleted
  (`nats kv rm` — bucket removal; `kv del` deletes a key, Codex round correction). No migration
  code. **`docs/operations/embeddings-cache-removal.md`.**

## 5. Production-wire integration tests (replace the sync-mock test)

- [x] 5.1 Rewrite `service/framework_owned_bucket_guards_integration_test.go`: the create-race test
  drives `Manager.StartAll` with the REAL `ComponentManager` and a real lifecycle component whose
  `Start` get-or-creates the pre-dirtied guarded bucket via the production async launch path. Assert:
  TTL stripped after the barrier, stored key preserved, WARN naming the bucket. Delete the
  `mockService`-based variant — do not keep both.
  **Discriminating shape (review round 2): the bucket does NOT exist before StartAll — the
  component's own Start creates it mid-boot carrying the foreign TTL + key (the racing create and
  unchanged adoption compressed into the boot-time step), and records the create-time retention as
  proof. Under a fire-and-forget revert, the sweep's skip-if-absent races the mid-boot create and
  the test goes red; a pre-boot dirty create would pass under either ordering.**
- [x] 5.2 Boot-fails-closed integration test: failing-`Start` component ⇒ `StartAll` errors, HTTP never
  comes up.
- [x] 5.3 All new/rewritten tests race-enabled with explicit synchronization (no sleeps, no wall-clock
  waits without rationale). **One bounded wait remains, with written rationale: the barrier unit test's
  absence assertion ("Start has NOT returned while a component Start is gated") is inherently a
  negative claim and uses a 500ms bound. The create-race integration test needs no waits: the barrier
  itself is the synchronization (StartAll returns ⇒ the mid-boot dirty create already happened).**

## 6. Specs + docs

- [x] 6.1 Spec deltas in this change are the source of truth — verify implementation against them
  (`openspec validate --strict` clean). **Valid; reviewer walked every scenario to its covering
  test/code (lens 6), one gap found and closed (discriminating create-race shape + log-drift canary).**
- [x] 6.2 Update `docs/concepts/` pages that describe boot ordering or the two-pass sweep, if any state
  the fire-and-forget behavior. **Grep found none: the only ComponentManager mentions
  (docs/concepts/12-flow-architecture.md) describe post-boot config-driven flows, which remain
  accurate; no EMBEDDINGS_CACHE or fire-and-forget claims anywhere under docs/concepts/.**

## 7. Gates (mirrors CI + house rules; all before PR merge)

- [x] 7.1 `task lint` (revive warnings = fail), `go vet ./...`, `gofmt` clean. **All clean.**
- [x] 7.2 `go test -race ./...` green. **135 packages ok, zero FAIL — run twice (post-impl,
  post-review-fixes).**
- [x] 7.3 Framework-package branch integration sweep: `go test -race -tags=integration ./...`
  (service/, graph/, natsclient touched). **Green except `TestWebSocketFederation_NackFlow` —
  investigated per flake discipline: passes clean in isolation (4.3s), fails only under full-sweep
  container churn at a 30s bound, uses raw testcontainers with NO ComponentManager/StartAll in its
  path (diff not implicated). Classified: pre-existing testcontainers-churn substrate flake
  (same class the developer hit on 3 other tests, all dying at NewTestClient setup).**
- [x] 7.4 Tagged vets: `go vet -tags=integration ./...` AND `go vet -tags=live_llm ./...`. **Clean.**
- [x] 7.5 `task schema:generate` + `git diff schemas/ specs/` no uncommitted drift;
  `go test ./test/contract/...` green. **Intended drift only (graph-embedding.v1.json cache_ttl
  deletion, committed with the change); contract tests ok.**
- [x] 7.6 BREAKING ⇒ e2e tier green before merge: `task e2e:statistical` (minimum; exercises
  graph-embedding boot) — run `task e2e:semantic` as well if feasible. **Run TWICE, both green from
  structured results (success:true, error_count:0, data_loss:0, embeddings queue drained, 0 failed):
  once post-implementation, once after the cache_ttl config-surface change.**

## 8. PR + review + merge

- [x] 8.1 Follow-up branch off main (retrospective-review workflow); PR references #622/#716 and the
  Codex retrospective findings; conventional-commit, BREAKING flagged. **PR #719, branch
  `fix/reopen-framework-owned-bucket-guards` off `d03c49f7`. Merge #718 (archive) FIRST, then
  trivial rebase — no file overlap.**
- [x] 8.2 `semstreams-reviewer` pre-merge review — reviewer explicitly checks the production
  concurrency shape of every new test (the miss class that let #716 merge).
  **Round 1 CHANGES-REQUESTED: caught the rewritten create-race test as STILL non-discriminating
  (pre-boot dirty create passes under fire-and-forget) + 3 SHOULD-FIX (stale docs, cache_ttl phantom
  knob, README contradiction). Round 2 APPROVE: gate machinery judged sound (developer proved even
  the reviewer's smallest-fix was green under revert; sweep-record gate + revert-proven red);
  durability canary requested and added (skipProbeMentioning ENTITY_STATES).**
- [x] 8.3 Merge gate: `gh pr checks` + `mergeStateStatus` verified at merge (no required checks on the
  repo); Codex review is owner-run/out-of-band — hand off, do not self-approve past it.
  **MERGED `a4287869` (2026-07-28) after THREE owner-run Codex rounds, each addressed and re-pushed
  (round 1: boot ordering + loud rejection; round 2: boot-boundary drain; plus three parallel-review
  checkpoints). All 5 checks pass + mergeStateStatus CLEAN verified immediately before merge.
  Follow-up filed by owner review post-merge: delete the graph/embedding library cache CLASS
  (Cache iface, NATSCache, HTTPConfig.Cache + branches, doc example) — the instance died in this
  change, the class survived; scheduled BEFORE #625/#629.**

## 9. Codex review round (PR #719)

- [x] 9.1 Boot/config-update serialization: launch `watchConfigUpdates` only AFTER the component-start
  barrier (and never on a failed boot), so no update is processed with `started == false` (which
  created-but-never-started components, parked StateInitialized and invisible to health). Producer
  side verified: `config.Manager.OnChange` channels are cap-1 buffered; per-key sends are
  non-blocking drop-on-full; the bulk `notifySubscribers` drain-and-blocking-send holds only
  config.Manager's own mu, which boot never acquires (`GetConfig` is lock-free) — deferring
  consumption cannot deadlock boot. Both dynamic paths verified start-or-fail-loudly with
  `started == true` (StateFailed + LastError → health). `watchConfigUpdates` confirmed the ONLY
  consumer of `cm.configUpdates`. Test:
  `TestComponentManagerStart_ConfigUpdatesSerializedAfterBarrier` — gate-held cold boot, buffered
  mid-boot update, order-ledger proof; observed failing on pre-fix code ("never reached
  StateStarted"), green on the fix.
- [x] 9.2 Loud rejection of the removed surface: `Config.Validate` rejects ANY `ports.outputs` entry
  ("graph-embedding declares no output ports; remove ports.outputs (see
  docs/operations/embeddings-cache-removal.md)"); the factory probes raw JSON and rejects a present
  `cache_ttl` key naming the migration note (targeted probe, NOT DisallowUnknownFields). Tests:
  `TestConfig_Validate_RejectsDeclaredOutputs` (EMBEDDINGS_CACHE + arbitrary output),
  `TestCreateGraphEmbedding_RejectsRemovedCacheTTL` — both observed failing pre-fix; clean configs
  still pass. In-repo fixture re-sweep: only the four shipped configs/ reference graph-embedding and
  all are already clean; no test/, docker/, cmd/, examples/ fixture trips the new rejections.
  Adopter note updated to state both exact errors and rewritten as the complete standalone
  migration checklist (sole downstream channel; sisters migrate internally).
- [x] 9.3 `nats kv del` → `nats kv rm` corrected in docs/operations/embeddings-cache-removal.md,
  tasks.md 4.5, and design.md Decision 3 (`rm` removes the bucket; `del` deletes a key).
- [x] 9.4 Ledger correction: 1.1 finding rewritten to the corrected sister-surface truth (the
  original sweep filtered .json hits as scorecard noise — semspec's 12 checked-in configs and
  semsource's beta148 parity literal were missed); no sister edits from here — migration
  communicated via the adopter note, sisters handle it on their next bump.
- [x] 9.5 Boot-boundary config drain (Codex round 2, blocker 1): synchronous coalesced drain loop in
  `ComponentManager.Start` after the barrier — each pass consumes the buffered OnChange events and
  reconciles against LIVE SafeConfig (edit-aware boot mode of the shared reconcile core; new
  components barrier-started with failures joining boot failure; edits rebuilt + barrier-started,
  rebuild failure fails boot; removals as the watcher's reconcile; rule packs immutable), loops
  until quiescent, bounded by the lifecycle ctx (cancellation fails boot). Design.md Decision 5.
- [x] 9.6 Registry apply-not-discard (Codex round 2, blocker 2): apply-if-different against the
  built-with baseline (captured at Initialize) everywhere — the boot drain rebuilds
  DepModelRegistry dependents on content drift (barrier-joined), the watcher's entry backlog check
  APPLIES a pending event instead of blind-discarding it, and the per-event path is
  content-gated (initial snapshot = no restart storm; identical events = no double restart). The
  round-3 "freshness edge (not correctness)" comment was WRONG (it was a correctness bug — a lost
  change stayed unapplied until the next change) and is removed with the fix.
- [x] 9.7 Tests (all real config.Manager + production KV writes, driven through Manager.StartAll,
  race-enabled, explicit sync): `TestIntegration_StartAll_MidBootComponentJoinsBootTransaction`
  (mid-boot add starts BEFORE the sweep: no skip-if-absent probe for its bucket, TTL stripped, key
  preserved, WARN — also the strip-before-HTTP end-to-end case),
  `TestIntegration_StartAll_MidBootEditJoinsBootTransaction` (edit applied by Start-return; the
  rebuilt instance started with the new value),
  `TestIntegration_StartAll_MidBootRegistryChangeRebindsDependents` (dependent rebuilt against the
  new registry by Start-return). ALL THREE revert-proven RED against the pre-drain code
  (drain call disabled + entry blind-discard restored via cp/md5 backup): T1 red on
  skip-probe-present + TTL-survives + no-WARN (the reopened create-race), T2 red with started
  values [A] (edit lost to the dropped notification + skip-existing reconcile), T3 red with builds
  [ep-a] (registry change lost to the blind discard). T1's red direction is forced by the
  sweep-skip-record gate (deterministic whenever the ordering permits the race); T2/T3 are
  deterministic by the cap-1 drop + skip-existing/blind-discard mechanics.
- [x] 9.8 Spec/design/comment updates: framework-composition delta extended with the
  boot-transaction paragraph + "a configuration update arriving during boot joins the boot
  transaction" scenario (cutoff stated: post-final-drain-pass updates are post-boot dynamic;
  acquisition seam is the durable closure); design.md Decision 5; Start doc comment states the
  drain + cutoff; the obsolete round-3 freshness comment removed.
