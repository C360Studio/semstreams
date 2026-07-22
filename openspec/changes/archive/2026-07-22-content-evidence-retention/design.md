## Context

Every content ObjectStore in the system is created through one shared constructor,
`NewStoreWithConfigAndMetrics` (`storage/objectstore/store.go:111-114`), which
hard-codes `TTL: 24 * time.Hour` on the `jetstream.ObjectStoreConfig`. That `TTL`
is a NATS `MaxAge` on the store's backing stream (`OBJ_<bucket>`): **blanket,
time-based, reference-blind**. It deletes every chunk older than 24h regardless of
whether a live entity still references it.

Three construction sites inherit that constant, and all three hold **ref-addressed
`ContentStorable` content** — bulky payloads keyed by content and pointed at by
ref-triples on the owning entity:

- `storage/objectstore/component.go:189` — generic ObjectStore (`MESSAGES`)
- `processor/agentic-loop/component.go:627` — agent content bucket
- `processor/graph-embedding/component.go:970` — verbatim evidence bodies (`#600`)

There is **no refcount, sweep, or GC** anywhere in `storage/objectstore/`. The
blanket TTL is the only thing reclaiming anything, and it reclaims by deleting live,
still-referenced content on a timer. The consumer-visible failure:

- **#600** — a day after ingest, recall/ranking/`embedding.ready`/`indexed_revision`
  are all green, and the ranked nodes carry **empty bodies**. Evidence silently
  expired; every signal says it did not.
- **#616** — fusion swallows the resulting hydration failure. `engine_lens.go:349-354`
  hydrates the body inside `if err == nil`; both the `lens.Hydrate` error and the
  `ResolveBody` error fall into the empty `else` and ship `Node.Body = ""` with no
  signal at any layer.

This directly contradicts the substrate's own established position. **ADR-068** forbids
reference-blind NATS lifecycle retention (`TTL`/`MaxBytes`) on the live graph, and
`ENTITY_STATES` is boot-defended by `AssertNoLifecycleRetention` (`natsclient/kv.go:173`,
called at `processor/graph-ingest/component.go:1135`). The content ObjectStores — which
hold state referenced by that same live graph — never got the guard.

**Owner decision (this change):** remove the TTL entirely rather than default it off;
extend the ADR-068 guard regime to content ObjectStores so a retention policy can never
be set silently; take a **clean break** (no external consumers exist outside our own
ref designs, so no compat shim); defer blob GC to a later increment (disk growth is not
a pre-v1 concern); fix #616 as a standalone reporting improvement.

> Note: the proposal cites "ADR-0008" for the reference-blind-retention rejection. That
> is **ADR-068**; corrected here and to be corrected in the proposal/specs.

## Goals / Non-Goals

**Goals:**

- No content ObjectStore carries lifecycle retention (`MaxAge`/`MaxBytes`). The safe
  state is the **only** state — there is no zero-valued TTL knob left on the surface to
  advertise a lever.
- Setting a retention policy on one of these stores — by operator config, internal
  code, or an out-of-band NATS edit — is **structurally loud**: boot fails closed rather
  than silently expiring evidence a day later.
- Existing persistent buckets that already carry the legacy 24h `MaxAge` **self-heal**
  to the safe state on boot, because the constructor otherwise never reconciles the
  config of an already-created stream (the fix would be inert exactly where it matters:
  the adopters' persistent NATS).
- Fusion reports a missing body as a **partial-result signal with a bounded reason**,
  not an empty string — the node exists and ranks; only its `Body` is absent, and the
  caller can tell why.

**Non-Goals:**

- **Orphan/blob GC.** Content-addressed dedup strands the previous object on every edit;
  reclaiming those needs a reference-aware refcount or mark-and-sweep. That is new
  substrate, ADR-scale, and deferred to its own increment. Filed, not built here.
- **Disk-exhaustion mitigation.** No `MaxBytes` backstop is added — a size cap is itself
  reference-blind (ADR-068) and would reintroduce #600 by another door. Growth is
  accepted pre-v1.
- **#601** (offloaded-entity title embedding) and **#613** (readiness semantics) — separate
  lanes / owner calls.
- Rewriting fusion's degrade-don't-fail *policy*. The policy is correct; only its silence
  is the defect.

## Decisions

### D1 — Remove the TTL from the constructor; do not zero it

Delete the `TTL` field from the `ObjectStoreConfig` literal in
`NewStoreWithConfigAndMetrics` rather than setting `TTL: 0`.

*Why over the alternative:* a configurable-TTL-default-off design (the proposal's
option b) leaves a retention lever on the surface. Any nonzero value silently eats live
content; the knob's mere existence is the footgun. Removing the field makes the safe
state unconditional and un-settable through our API. Rejected: "configurable TTL, safe
default" — it is exactly the confusing zero-value the owner ruled out.

### D2 — Reconcile-then-assert boot guard for content ObjectStores

Because the constructor's create-or-get path (`store.go:118-125`) *gets* an existing
store without reconciling its config, D1 alone is a no-op on any already-created bucket.
Boot therefore does two things, in order, on the backing stream (`OBJ_<bucket>`):

1. **Reconcile (strip-and-log).** Read the backing stream's `Config.MaxAge`/`MaxBytes`.
   If either is binding, `UpdateStream` to clear it and emit a `WARN`
   (`removed lifecycle retention from content store <bucket>`). Stripping is
   non-destructive — it stops *future* time-based deletion and deletes nothing — so it
   self-heals toward the safe state. This covers the legacy 24h buckets and anything set
   out of band.
2. **Assert (fail-closed).** Re-check the config with the existing pure
   `CheckNoLifecycleRetention` (`natsclient/kv.go:158` — already I/O-free and reused
   verbatim). If retention is *still* binding (e.g. `UpdateStream` was denied), fail boot
   with `ErrGraphBucketRetention`. This is the belt-and-suspenders backstop: we never
   proceed with a binding retention config.

This composition resolves the strip-vs-fail question without choosing: we strip what we
can (our own legacy value is known-safe to auto-correct) and fail-closed on anything we
could not strip.

*Reader mechanics:* mirror `BucketRetention` (`natsclient/kv.go`) with an ObjectStore
analog that reads `js.Stream(ctx, "OBJ_"+bucket).Info().Config.{MaxAge,MaxBytes}`. The
`OBJ_<bucket>` convention and mutable `MaxAge`/`MaxBytes` are confirmed against
nats.go v1.48.0 (`jetstream/object.go:480`, `objNameTmpl = "OBJ_%s"`).

*Guard home — shared constructor, not per-component.* The TTL was shared in the ctor;
the guard is placed there too so all three sites inherit it in one seam (DRY; the
alternative of three `AssertNoLifecycleRetention`-style calls like graph-ingest is more
explicit but triplicates the logic). Because every current ObjectStore holds
ref-addressed content, the guard is **blanket** — it applies to `MESSAGES`, the agent
content bucket, and the embedding evidence store alike. If a genuinely ephemeral
ObjectStore is ever needed, it gets an *explicit* opt-out via a deliberate future change
— not a silent default knob today.

### D3 — Fusion body-hydration reporting (per-node, bounded reason)

Model on the existing `Unhydrated` / `UnhydratedReason` precedent
(`pkg/fusion/retrieval.go:100-129`), but attach it **per-node**, not top-level. Seed
non-hydration is top-level (`Response.Unhydrated`) because a seed that does not load
produces no `Node`. A body-hydration failure is different: the node *exists and ranks*;
only its `Body` is missing. A top-level list would sever the node↔reason association, so
the reason rides on the node.

At `engine_lens.go:349`, replace the error-swallowing `if err == nil` with explicit
capture: on `Hydrate`/`ResolveBody` failure, leave `Body` empty and set a bounded
`Node` reason field (e.g. `BodyReason` with values mirroring the closed set —
`not_found`, `error`). Also increment a metric
(`fusion_body_hydration_failures_total{reason}`), consistent with how
`engine_graph.go:20-41` meters every graph-facet cap through its own metadata.

*Contract impact — additive, not wire-breaking.* The new `Node` field is
`json:"...,omitempty"`, so a fully-hydrated response is byte-unchanged and existing
consumers ignore it. The proposal's "BREAKING" flag overstates this: it is an additive
contract extension. The exact field name/type and whether an all-bodies-missing query
warrants any engine-level signal (it does **not** defer — a node still exists, so it is a
partial result, not a refusal) are resolved in the fusion spec delta.

### D4 — Spec homes

- **`graph-retention`** (extend): the content-ObjectStore retention invariant — no
  lifecycle `MaxAge`/`MaxBytes`, reconcile-then-assert at boot — is the same ADR-068
  invariant already owned by this capability, applied to content-addressed ObjectStores.
  Extending it avoids a near-duplicate `content-store-retention` capability.
- **`fusion`** (extend): body-hydration outcome reported separately from seed hydration.

## Risks / Trade-offs

- **Blanket ctor guard blocks a future legitimately-ephemeral ObjectStore** → today none
  exists; every store is ref-addressed content. The escape hatch is a deliberate future
  change adding an explicit opt-out, never a silent knob. Documented as the intended
  path.
- **`UpdateStream` reconcile fails or is denied** → the D2 assert backstop fails boot
  loudly with a clear message rather than proceeding with binding retention. Worst case
  is a loud, correct-direction failure — not silent expiry.
- **Unbounded blob growth once the TTL is gone** → accepted pre-v1 (owner de-prioritized
  disk). Optional cheap mitigation: a CONTENT size/object-count metric that turns "build
  GC later" into a *triggered* decision before any cliff. Listed as an optional
  observability task, not a gate on this increment. The GC increment + ADR is the real
  follow-up.
- **e2e coverage gap (#599)** → no e2e tier exercises fusion `Fuse`/batch/unhydrated
  reporting, which is what let #600/#616 hide. This increment covers the guard and the
  reporting with unit + integration tests; the e2e gap stays filed under #599.

## Migration Plan

- **Clean break, no compat shim** — no external consumer holds refs into these stores
  outside our own ref designs, so the contract changes hard.
- **Deploy self-corrects** — the D2 boot reconcile strips the legacy 24h `MaxAge` from
  existing persistent buckets automatically; no operator action needed.
- **Sister repos** — semsource/semboids attach content stores through the framework
  constructor, so they inherit D1+D2 with no code change. The fusion `Node` field (D3) is
  additive/wire-compatible; fusion callers (mcp-gateway, semsource) need no change unless
  they choose to read the new body-reason field. Coordinate the contract note via
  `semstreams-asks`.
- **Rollback** — reverting restores the ctor's 24h constant; the reconcile only *strips*
  and never re-adds retention, so newly created buckets return to the old TTL behavior
  while already-stripped buckets stay safely stripped. Acceptable.

## Open Questions

- **Fusion reason type:** reuse `UnhydratedReason`'s vocabulary (`not_found`/`error`) on a
  distinct `Node` field, vs. a small dedicated `BodyReason` type. Lean: distinct type,
  shared vocabulary — body and seed are different failure surfaces. Resolve in the fusion
  spec.
- **ADR needed?** Removing the TTL + guarding content stores is an *application* of
  ADR-068 to content-addressed ObjectStores, not a new irreversible decision — lean: no
  new ADR; capture mechanics in the `graph-retention` spec. The eventual orphan-GC design
  *is* ADR-scale and gets its own record when that increment opens.
- **Metric inclusion:** include the optional CONTENT size/count metric in this increment,
  or hold it entirely for the GC increment? Lean: include it (cheap, and it is the GC
  trigger), but keep it non-blocking.
