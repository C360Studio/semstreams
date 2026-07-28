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

**D2 — A two-pass authoritative sweep at two fixed boot seams, not per-creator asserts.** Owned
buckets are created across graph-ingest, graph-index(-temporal/-spatial), graph-embedding,
graph-clustering — and some are also get-or-created by *readers* (graph-query creates
ENTITY_STATES/SPATIAL/INCOMING, `graph/query/client.go:206/219/232`). A per-creator assert would be
redundant and would entangle the reader-creates anti-pattern. Instead, a `graph`-level
`AssertOwnedBucketsClean(ctx, client, logger)` ranges the guarded set, binds each **read-only /
must-exist (never create) / skip-if-absent**, and calls the KV reconcile atom. It is invoked at TWO
fixed seams, and the coverage guarantee is the pair:
- **Pre-start belt** — from `service.WireOwnership` (`service/ownership_service.go`), before component
  start. Both `cmd/semstreams` and `cmd/e2e-semstreams` call `WireOwnership` exactly once before
  `StartAll`, so this seam covers both binaries with no half-migration drift. It self-heals
  prior-boot / out-of-band dirt early, before rule evaluation or a component's get-or-create leans on
  a persisted-dirty bucket.
- **Post-start coverage pass** — from the tail of `service.(*Manager).StartAll`, after the
  service-start loop completes and BEFORE `completeHTTPSetup()` (so a fail-closed abort never briefly
  reports healthy). This is the same shared seam both mains funnel through (`cmd/semstreams/main.go`,
  `cmd/e2e-semstreams/main.go` both call `Manager.StartAll`), so one call covers both binaries with no
  per-main edit. It is guarded `if m.natsClient != nil` (a NATS-less Manager skips + debug-logs,
  honoring resourceless-deploy discipline) and returns its error from `StartAll` so boot fails closed.
- *Ordering:* none required within a pass — skip-if-absent means a not-yet-created bucket (tier-gated
  deploys) is passed over; it cannot carry a foreign TTL, and its true owner creates it clean.
- Keep graph-ingest's existing at-creation asserts (ENTITY_STATES + guard bucket) as belt-and-
  suspenders.
- *Alternative rejected:* assert at each creation site — redundant, misses reader-creates, N places
  to drift.
- *Why two passes and not one:* a single pass can only reconcile the buckets that exist at the moment
  it ranges the set. The pre-start belt runs before owners hold handles, so it necessarily skips a
  bucket created dirty during this boot's own service-start loop (a create-race); the post-start pass
  is what covers that window. See D6.

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

**D5b — Extend the same additive fix to two framework operational buckets (F2/F3).** The Codex
review found two more owned buckets missing from `FrameworkOwnedBuckets()`, both generically
writable by a rule `update_kv`:
- **F2 `GRAPH_INGEST_APPLIED_SEQ`** — was a private literal `graphIngestGuardBucket` at
  `graph-ingest/component.go:490`. Promote it to `graph.BucketGraphIngestAppliedSeq` (Operational
  buckets block), delete the private literal, and replace its two uses (creation `:1182`, assert
  `:1195`) with the constant. Add to `FrameworkOwnedBuckets()` → both rule guards reject it. It is
  **included in the retention sweep** (correctness-critical no-eviction state; graph-ingest already
  asserts no-retention on it at create time, so the sweep is belt-and-suspenders). The blind uint64
  decode at `keyed_ingest.go:272` is left unchanged — write-ownership closes the only injection
  vector; decoder hardening is a noted optional follow-up.
- **F3 `GRAPH_STATUS`** — was `readiness.BucketGraphStatus = "GRAPH_STATUS"`. Add
  `graph.BucketGraphStatus` as the single source of truth and change `graph/readiness/watcher.go` to
  re-export it (`const BucketGraphStatus = graph.BucketGraphStatus`), keeping `readiness.BucketGraphStatus`
  stable for all its consumers (a drift-guard test pins the two equal). Add to
  `FrameworkOwnedBuckets()` → both rule guards reject a forged readiness envelope. It is **included
  in the retention sweep** (created clean with `History=3` and no TTL, so a steady-state no-op).
  CRITICAL: the sweep strips only `MaxAge`/`MaxBytes`; `History`/`MaxMsgsPerSubject` is left
  untouched (an integration test asserts History survives a strip).

**Deferred: `COMPONENT_STATUS`.** A third candidate operational bucket is deliberately NOT added by
this change — it is a cross-layer, many-writer bucket with a different retention/ownership posture,
tracked as follow-up **#717**. It is (and stays) absent from `FrameworkOwnedBuckets()` here.

**D6 — Create-race coverage is the post-start sweep pass, not a per-site opt-in.** The pre-start
belt (D2) cannot see a bucket created dirty during this boot's service-start loop, because it runs
before owners hold handles. Two ways to close that window were considered:
- *(A) rejected — a per-owner post-create assert/reconcile at every creation site.* This re-opens
  exactly the per-creator drift D2 rejected (N sites to keep in sync, misses reader-creates, entangles
  the reader-creates anti-pattern), and each site would have to decide refuse-vs-strip independently.
- *(B) chosen — a second whole-set sweep pass at one shared seam (`Manager.StartAll` tail).* One call,
  after every owner has created its bucket, before the surface reports healthy; reuses the exact
  `AssertOwnedBucketsClean` the belt uses (skip-if-absent, reconcile-then-assert), so there is a single
  definition of the guard and a single place it is wired per binary. The cost — the sweep runs twice
  per boot — is negligible (a `Stream.Info` per existing guarded bucket) against the drift it avoids.

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
- **Boot-time enforcement leaves a runtime timing window** → foreign retention applied to an owned
  bucket AFTER boot (an out-of-band edit while the process runs) is not continuously reconciled; it is
  caught at the next boot's sweep. This is an accepted bound, symmetric with the ObjectStore
  precedent's boot-time posture: a TTL only takes semantic effect over time, and the graph never sets
  one itself, so the exposure is a stray manual edit that survives at most until the next restart.
- **`MaxBytes` capacity policy is explicitly out of scope** → the reconcile atom STRIPS a binding
  `MaxBytes` (to the `-1` unlimited sentinel) but sets none; this change introduces no size cap and no
  `DiscardNew`. An emergency capacity ceiling on authoritative graph KV remains deferred wholesale to
  `bounded-storage-operability`, which rebases its delta onto this broadened requirement. The one
  rebuildable cache `EMBEDDINGS_CACHE` is excluded from the retention sweep precisely so that epic can
  legitimately bound it later.

## Migration Plan

Additive, no format/API change. Deploy = the new binary's boot sweep self-heals any legacy/foreign
TTL on owned buckets and WARNs; steady-state (no foreign TTL) is a no-op. Rollback = prior binary —
buckets remain clean (nothing was deleted), the prior binary simply stops sweeping. Prudent (not
obligatory, since additive) pre-merge gate: `task e2e:core` green (health + dataflow exercises the
graph boot path).

## Open Questions

1. **Boot seam:** RESOLVED — not "once" but a two-pass model at two shared seams (D2/D6): a
   **pre-start belt** in `service.WireOwnership` (self-heals prior-boot dirt before rule evaluation)
   and a **post-start coverage pass** at the tail of `service.(*Manager).StartAll` (closes the
   create-race window). Both seams are the single shared function both mains funnel through, so there
   is no per-main edit and no half-migration drift.
2. **Fold graph-ingest's two existing pure-assert sites onto the atom (D3)?** Recommended for one
   guard behavior, but it is a visible behavior change (refuse→strip) on ENTITY_STATES; confirm at
   review rather than assume.
3. **Write-ownership requirement home:** `graph-retention` (chosen — co-located with retention over
   the same owned set) vs `nats-kv-keys` (architect's lean). Reviewer confirms.
