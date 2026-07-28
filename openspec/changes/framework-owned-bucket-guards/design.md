## Context

Two guards protect the framework-owned KV plane; both are under-enforced at HEAD.

- **Retention guard.** `natsclient.(*KVStore).AssertNoLifecycleRetention` (`natsclient/kv.go:173`)
  reads a bucket's backing-stream config via `bucket.Status()` → `MaxAge`/`MaxBytes` and asserts
  it via the shared pure predicate `natsclient.CheckNoLifecycleRetention` (`kv.go:158`). It has
  exactly two callers, both graph-ingest (`component.go:1135` ENTITY_STATES, `:1195`
  redelivery-guard). Every other owned bucket is unguarded.
- **Write-ownership guard.** `graph.IsFrameworkOwnedBucket` (`graph/constants.go:77`) backs the two
  rule `update_kv` guards (`processor/rule/config_validation.go:363` load-time,
  `processor/rule/actions.go:1941` runtime). `FrameworkOwnedBuckets()` (`:53`) lists 17 buckets but
  omits `ENTITY_SUFFIX_INDEX` (created as a literal at `graph-ingest/component.go:1154`), so a rule
  can `update_kv` into it today.

The ObjectStore side already solved the analogous problem: `storage/objectstore/retention.go`
(`reconcileNoLifecycleRetention`, PR #632/#636) strips a foreign TTL on `OBJ_<bucket>`, warns, then
re-asserts via the *same* `CheckNoLifecycleRetention` predicate, failing closed only when unfixable.
The `graph-retention` spec's "Content ObjectStores carry no lifecycle retention" requirement is this
pattern. This change makes the KV half symmetric and complete.

## Goals / Non-Goals

**Goals:**
- One authoritative boot-time sweep asserts no-lifecycle-retention across the full owned set (minus
  the rebuildable cache), self-healing legacy/foreign TTLs.
- Close the `ENTITY_SUFFIX_INDEX` write-ownership hole by registering it as framework-owned.
- Keep the KV and ObjectStore retention guards sharing one definition of "binding retention."

**Non-Goals:**
- No `MaxBytes`/`DiscardNew` capacity policy — deferred wholesale to `bounded-storage-operability`.
- No predicate/vocabulary surface.
- Not the ObjectStore analogue (already shipped), not #625/#629 (consume this primitive next), not
  the reader-creates-owned-bucket audit (filed separately).

## Decisions

**D1 — Reconcile-then-assert, not pure-assert-extended.** The sweep strips a binding
`MaxAge`/`MaxBytes` in place (via `UpdateStream` on `KV_<bucket>`), logs a WARN, re-reads fresh, and
fails closed on the shared predicate only if still binding.
- *Why:* Extending the current *pure-assert* to ~17 buckets would multiply graph-ingest's
  process-lifetime-sticky boot-takedown (a failed assert wedges rule evaluation permanently —
  `processor/rule/entity_watcher.go:93`, `service/message_logger_http.go:479`) across every derived
  bucket: one stray TTL on a tier-2 index would wedge the whole graph boot. Reconcile turns the
  common case (legacy/foreign TTL) into self-heal and keeps fail-closed only for genuinely unfixable
  drift.
- *Why this is not "extra scope":* It makes the KV requirement **symmetric with the ObjectStore
  requirement already in `graph-retention`**, which strips-then-asserts. Leaving KV on pure-assert
  while ObjectStore self-heals is the inconsistency.
- *Alternative rejected:* pure-assert on all buckets — simplest patch, but 17× the sticky-takedown
  blast radius, and asymmetric with the shipped ObjectStore guard.

**D2 — One authoritative sweep at a single boot seam, not per-creator asserts.** Owned buckets are
created across graph-ingest, graph-index(-temporal/-spatial), graph-embedding, graph-clustering — and
some are also get-or-created by *readers* (graph-query creates ENTITY_STATES/SPATIAL/INCOMING,
`graph/query/client.go:206/219/232`). A per-creator assert would be redundant and would entangle the
reader-creates anti-pattern. Instead, a `graph`-level `AssertOwnedBucketsClean(ctx, client, logger)`
ranges the guarded set, binds each **read-only / must-exist (never create) / skip-if-absent**, and
calls the KV reconcile atom. Wire it once at a deterministic boot seam (same shape as
`ownership.EnsureBuckets`, called from `service/ownership_service.go:130`).
- *Ordering:* none required — skip-if-absent means a not-yet-created bucket (tier-gated deploys) is
  passed over; it cannot carry a foreign TTL, and its true owner creates it clean.
- Keep graph-ingest's existing at-creation asserts (ENTITY_STATES + guard bucket) as belt-and-
  suspenders for the create-time race the sweep cannot see; the sweep is the *coverage* guarantee.
- *Alternative rejected:* assert at each creation site — redundant, misses reader-creates, N places
  to drift.

**D3 — KV reconcile atom mirrors the ObjectStore one and reuses the shared predicate.** Add
`natsclient.ReconcileNoLifecycleRetention(ctx, bucket)` that reaches the KV backing stream
`KV_<bucket>` (the KV analogue of `OBJ_<bucket>`; `MaxAge`/`MaxBytes` are `UpdateStream`-mutable
there too) and reuses `CheckNoLifecycleRetention` for the final assert, so KV and ObjectStore can
never diverge on what "binding" means. Optionally migrate graph-ingest's two pure-assert call sites
onto the atom for one guard behavior (see Open Questions — this changes ENTITY_STATES from
refuse-to-boot to strip-then-boot; strictly safer, deletes nothing).

**D4 — Retention set ⊂ write-ownership set.** `EMBEDDINGS_CACHE` stays in `FrameworkOwnedBuckets()`
(write-protected) but is excluded from the retention sweep: it is the lone rebuildable cache, a
capacity cap on it is legitimate, and `bounded-storage-operability` wants to own that. Model this as
a `graph`-level `retentionGuardedBuckets()` = `FrameworkOwnedBuckets()` minus `EMBEDDINGS_CACHE`.

**D5 — Write-ownership fix is additive.** Add `BucketEntitySuffixIndex = "ENTITY_SUFFIX_INDEX"`,
replace the literal at `graph-ingest/component.go:1154`, add it to `FrameworkOwnedBuckets()`. The two
`IsFrameworkOwnedBucket` guard sites gain coverage automatically. Verified: no shipped config in
`configs/` writes `ENTITY_SUFFIX_INDEX`, so no live rule breaks.

## Risks / Trade-offs

- **Self-heal strips another process's stream config** → It only clears `MaxAge`/`MaxBytes` on a
  graph-owned backing stream, deletes no keys, and logs the bucket + removed retention loudly. This
  is exactly the shipped ObjectStore behavior; the graph never legitimately sets these.
- **Migrating graph-ingest's two asserts to the atom changes ENTITY_STATES boot from refuse→strip**
  → Strictly safer (self-heals instead of wedging), no data change. Flag explicitly for the reviewer;
  it is optional to inc-0 (see Open Questions).
- **Cross-change spec collision with `bounded-storage-operability`** → Both edit `graph-retention`.
  Mitigated by the Non-goal: this change lands strict no-retention coverage only; the storage-limits
  epic rebases its `DiscardNew` delta onto this broadened requirement. Sequence this change first.
- **Reader-creates-owned-bucket weakens the single-writer thesis** #629 later leans on → out of scope
  here; file a follow-up so the "single-owner derived plane" claim is not quietly undercut.

## Migration Plan

Additive, no format/API change. Deploy = the new binary's boot sweep self-heals any legacy/foreign
TTL on owned buckets and WARNs; steady-state (no foreign TTL) is a no-op. Rollback = prior binary —
buckets remain clean (nothing was deleted), the prior binary simply stops sweeping. Prudent (not
obligatory, since additive) pre-merge gate: `task e2e:core` green (health + dataflow exercises the
graph boot path).

## Open Questions

1. **Boot seam:** wire `AssertOwnedBucketsClean` from the ownership-service boot path
   (`service/ownership_service.go`) or a dedicated graph-boot helper? Developer + architect to pin so
   it runs once, before rule evaluation depends on the buckets.
2. **Fold graph-ingest's two existing pure-assert sites onto the atom (D3)?** Recommended for one
   guard behavior, but it is a visible behavior change (refuse→strip) on ENTITY_STATES; confirm at
   review rather than assume.
3. **Write-ownership requirement home:** `graph-retention` (chosen — co-located with retention over
   the same owned set) vs `nats-kv-keys` (architect's lean). Reviewer confirms.
