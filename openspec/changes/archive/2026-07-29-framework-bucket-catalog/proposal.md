# Framework bucket catalog + acquisition-seam enforcement

## Why

The framework-owned KV plane is governed by choreography: a hand-listed `FrameworkOwnedBuckets()`
(review has repeatedly caught missing members), a two-pass boot sweep, at-creation asserts, five
shadow name catalogs, and "keep these in sync" comments. Two live bugs found while deriving the
catalog prove the cost: **F1** — `ENTITY_STATES` is created with divergent `History` by two paths in
the same binary (graph-ingest: default 1; the graph-query tool-registration path: 3) and adoption
never compares config, so the graph's actual history depth is a boot race — and the sync comment
claiming alignment is factually wrong today. **F2** — four framework buckets (`OUTGOING_INDEX`,
`INCOMING_INDEX`, `ALIAS_INDEX`, `PREDICATE_INDEX`) are created from operator config strings with no
validation: a typo yields a stray bucket the sweep skips, the write guard ignores, and graph-query
never reads. The framework-composition spec already names "the bucket acquisition seam" as the
durable closure for post-boot-cutoff acquisition; this change discharges that promissory clause.

## What Changes

- **One descriptor catalog** (`graph/kvcatalog.go`, 22 entries): name · owner · class
  (authoritative/derived/operational/diagnostic) · retention policy (Kind+params struct:
  `no-lifecycle` | `bounded-ttl` | `unmanaged` — all three populated today) · write policy ·
  create posture · History/Replicas. `FrameworkOwnedBuckets()`/`IsFrameworkOwnedBucket()` keep
  their signatures as DERIVED views; the hand-written list dies.
- **Acquisition seam** (`natsclient/kvspec.go`): `EnsureFrameworkBucket` (owners: create-or-open →
  reconcile retention AND History to spec → verify → handle; unknown retention Kind fails closed)
  and `OpenFrameworkBucket` (readers: must-exist, NEVER creates, not-ready error naming the catalog
  owner — closes #714's reader-creates class). Failure fails the caller's `Start` closed (composes
  with the #719 barrier).
- **Migration, complete-system slice**: Tier 1 — all 17 owner creation sites; Tier 2 — the reader
  class (the tool-registration `ENTITY_STATES` create, `graph/query.ensureBuckets`, graph-query's
  raw `CreateOrUpdateKeyValue`, three private `"ENTITY_STATES"` constants); Tier 3 — all 24
  `COMPONENT_STATUS` sites (owner-decided: one PR); Tier 4 — five shadow name catalogs deleted.
  graph-index output-port subjects MUST resolve through the catalog or fail boot (**BREAKING**,
  closes F2).
- **The post-start sweep pass is DELETED**; the pre-start pass is demoted to a legacy-drift
  backstop (its one honest job: catalog buckets whose owner is absent from this composition).
- **#717 answered**: `COMPONENT_STATUS` = diagnostic / write-open / retention-unmanaged — 24
  production writers, ZERO production readers (e2e harness only); write-protecting it would guard
  state nothing reads. A future ops TTL is a one-line catalog edit.
- **Owner decisions recorded (2026-07-28)**: `ENTITY_STATES History = 1` (owner's intent; nothing
  reads deeper history — the only `History()` consumer is Lifecycle's workflow buckets; reconcile
  down is destructive-but-unread, WARN names it); Tier 3 in this PR; e2e-client shadow constants
  in scope.
- **BREAKING**: `graph/query.Config` loses `EntityStates`/`SpatialIndex`/`IncomingIndex` (verified:
  no sister sets them — semmem/semsage pass `DefaultConfig()`, semsource passes nil); graph/query
  readers now fail not-ready instead of creating (lazy acquisition verified — first call is
  post-boot); off-catalog graph-index output subjects fail boot. Adopter note required (sole sister
  channel; we do not edit sister repos).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `graph-retention` — ADDED requirement (descriptor catalog as single source; seam acquisition;
  config-supplied names must resolve; readers never create; unknown Kind fails closed); MODIFIED
  "The live graph carries no lifecycle retention" (two-pass → seam-primary + single legacy-drift
  backstop; retention is a per-descriptor field); MODIFIED "Framework-owned buckets reject generic
  KV writes" (owned set = derived write-policy view; COMPONENT_STATUS stays open with evidence).
- `framework-composition` — MODIFIED "Component start failures fail boot closed and surface in
  health": the promissory "acquisition seam is the durable closure" clause becomes a discharged
  requirement with a post-boot-cutoff scenario; the barrier's purpose restated (fail-closed boot,
  no longer retention coverage).

NOT `nats-kv-keys` (key grammar ≠ bucket lifecycle); NOT per-component specs (restating acquisition
per capability is the duplication the catalog kills).

## Impact

- **Code**: `natsclient/kvspec.go` + `graph/kvcatalog.go` new (~290 lines incl. the 22-row table);
  ~30 files touched mechanically across the four tiers; `service/service_manager.go` post-start
  pass deleted; `graph/query` config surface deleted (~75 lines); `graph/inference` phantom
  `BucketName` knob deleted.
- **Net-deletion ledger** (the owner's constraint): 3 name catalogs → 1; 2 boot passes → 1; 2
  at-creation asserts → 0; 41 hand-written `KeyValueConfig` literals → 0; 1 exception narrative →
  0; 1 factually-wrong sync comment → 0; ~−450/+350 LOC. The structural win is the point; the
  contract test (no catalog-bucket literals outside the catalog) replaces "review keeps catching
  missing members" with a mechanism.
- **Collision**: `bounded-storage-operability` rebases onto this (its graph-retention delta is
  already stale pre-#622); its later work = add a `RetentionDiscardNewCeiling` Kind + params and
  fill per-bucket — no catalog shape change, no consumer signature change.
- **e2e (BREAKING ⇒ tier before merge)**: `e2e:structural` (F2 path + write guard + query) AND
  `e2e:statistical` (embedding/community owners + the COMPONENT_STATUS mass migration); `e2e:core`
  free. `natsclient` touched ⇒ branch integration sweep.

## Non-goals

- No `DiscardNew`/MaxBytes semantics change and no unpopulated retention Kinds (bounded-storage
  owns that arm later; a placeholder constant is a phantom).
- No runtime caller-identity check on Ensure (owner enforcement is call-site selection +
  review — plumbing identity through 40 sites is the ratchet; limitation stated in spec).
- No catalog-vs-actual diagnostics endpoint/metric (zero consumers today; the WARN + fail-closed
  boot are the consumers with teeth; bounded-storage's operator inventory becomes the consumer and
  owns that surface when it lands).
- No catalog coverage of app/product buckets (AGENT_LOOPS, research-graph, flowstore, personas,
  governance, WORKFLOW_EXECUTIONS…): the catalog covers buckets whose write-ownership or retention
  the FRAMEWORK guarantees — the boundary rule is stated in the spec so the catalog cannot grow by
  accretion.
- No init()-registered bucket registry (re-creates the retired payload-registry singleton class).
- No reader-side reconciliation (a reader that "fixes" config is the same bug class as a reader
  that creates).
