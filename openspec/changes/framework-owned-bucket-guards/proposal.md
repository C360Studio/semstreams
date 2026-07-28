## Why

The framework builds ~17 derived/operational KV buckets (indexes, caches, community and
embedding stores) from authoritative graph state. Two guards are supposed to keep that
derived plane exclusively framework-controlled — a **retention guard** (no NATS TTL/`MaxBytes`
lifecycle eviction, which is reachability-blind and would silently drop live data) and a
**write-ownership guard** (generic KV writers such as a rule `update_kv` must not mutate a
framework bucket). Both guards are under-enforced at HEAD:

- The retention guard runs on **2 of ~17** owned buckets — only `ENTITY_STATES` and the
  graph-ingest redelivery-guard bucket call `AssertNoLifecycleRetention`. Every derived index
  (`EMBEDDING_INDEX`, `COMMUNITY_INDEX`, `SPATIAL_INDEX`, `TEMPORAL_INDEX`, `ANOMALY_INDEX`,
  `STRUCTURAL_INDEX`, `ENTITY_SUFFIX_INDEX`, …) would silently honor a foreign TTL. This is not
  theoretical: #610/#611 created graph buckets with a 7-day TTL, and only the `ENTITY_STATES`
  one was caught.
- `ENTITY_SUFFIX_INDEX` is created as a bare literal (`graph-ingest/component.go:1154`) and is
  **absent from `FrameworkOwnedBuckets()`**, so the write-ownership guard returns false for it —
  **a rule can legally `update_kv` into it today.** The live bug.

This is Epic C increment-0: the shared bucket-ownership guard **primitive** that #625 (embedding
cleanup repair loop) and #629 (coalescer resurrection) then consume. It generalizes the ownership
pattern B3 proved on one store (content-addressed community summaries) to the derived-KV plane the
`pkg/projection` contract arc leaves ungoverned.

## What Changes

- **Extend the retention guard to the full owned-bucket set** via a single authoritative boot-time
  sweep, replacing the two ad-hoc per-creator asserts as the coverage guarantee.
- **Adopt reconcile-then-assert** for the guard (mirroring the already-shipped ObjectStore
  precedent `storage/objectstore/retention.go`): strip a foreign binding TTL/`MaxBytes` →
  self-heal + WARN → re-read fresh → fail-closed on the shared `CheckNoLifecycleRetention`
  predicate only when the drift is genuinely unfixable. This is what lets full coverage exist
  **without** multiplying the current pure-assert's process-lifetime-sticky boot-takedown blast
  radius across every derived bucket.
- **Register `ENTITY_SUFFIX_INDEX` as framework-owned**: add a `BucketEntitySuffixIndex` constant,
  replace the literal at its creation site, and add it to `FrameworkOwnedBuckets()` — closing the
  live `update_kv` write-ownership hole at both rule guard sites.
- **Exclude `EMBEDDINGS_CACHE` from the retention sweep** while keeping it write-protected: it is
  the one legitimately rebuildable cache, and bounding its capacity belongs to the separate
  storage-limits epic, not here.
- Additive, **not breaking**: no shipped config writes `ENTITY_SUFFIX_INDEX`, and the reconcile
  path self-heals rather than newly rejecting steady-state deploys.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities

- `graph-retention`: **broaden** the "The live graph carries no lifecycle retention" requirement
  from its `ENTITY_STATES`-specific enumeration to the full framework-owned-bucket set (excluding
  the `EMBEDDINGS_CACHE` cache), and adopt reconcile-then-assert semantics (strip-and-warn /
  fail-closed-on-unfixable / clean-boot) symmetric with the existing "Content ObjectStores carry
  no lifecycle retention" requirement. **Add** a framework-owned-bucket write-ownership
  requirement: the `FrameworkOwnedBuckets()` set (now including `ENTITY_SUFFIX_INDEX`) rejects
  generic KV writers at rule load and runtime. *(Home pick: co-located here over `nats-kv-keys`
  because both guards express the framework's exclusive control of the same bucket set;
  reviewer-adjustable.)*

## Non-goals

- **No capacity/size policy.** Any `DiscardNew`/`MaxBytes` emergency-ceiling on authoritative graph
  KV is deferred **entirely** to the separate `bounded-storage-operability` change; this change
  lands the strict "no lifecycle retention" status quo only and does not touch `MaxBytes` policy.
  `bounded-storage-operability` rebases its `graph-retention` delta onto this broadened requirement.
- **No predicate or vocabulary surface.** This change keys exclusively on bucket **name** and NATS
  backing-stream retention config. It reads, writes, validates, and registers no predicate or
  vocabulary entry — making the in-flight BREAKING `predicate-contract-enforcement`,
  `predicate-raw-key-representation`, and the `pkg/projection` contract-bound mutation arc
  deterministically irrelevant to it.
- **Not the ObjectStore analogue.** The ObjectStore "no lifecycle retention" guard is already
  implemented (`storage/objectstore/retention.go`, PR #632/#636) and specced; this change verifies
  coverage and closes that ask, it does not rebuild it.
- **Not the reader-creates-owned-bucket audit.** Several readers get-or-create owned buckets
  (e.g. graph-query creates `ENTITY_STATES`/`SPATIAL_INDEX`/`INCOMING_INDEX`); that "reader is an
  emitter" anti-pattern is filed separately, out of scope here.
- **Not #625/#629.** Those consume this primitive in the following graph-embedding increment.

## Impact

- **Code:** `graph/constants.go` (new bucket constant + list entry), `natsclient/kv.go` (KV
  reconcile-then-assert atom mirroring the ObjectStore predicate), a `graph`-level owned-bucket
  sweep helper wired at one deterministic boot seam, `processor/graph-ingest/component.go:1154`
  (literal → constant). The two `IsFrameworkOwnedBucket` guard sites
  (`processor/rule/config_validation.go:363`, `processor/rule/actions.go:1941`) gain
  `ENTITY_SUFFIX_INDEX` coverage automatically.
- **APIs/format:** none. No wire, key-codec, or envelope change.
- **Consumers:** all sem* products rely on the live graph not silently expiring; **semsource**
  (lead v1 product) is the primary consumer. No product-side change required (additive).
- **Ops:** a legacy/foreign TTL on a derived bucket is now self-healed + WARNed at boot rather than
  silently honored; a genuinely unfixable retention state fails boot fast with the bucket named.
