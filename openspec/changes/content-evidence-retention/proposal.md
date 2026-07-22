## Why

For a product whose contract is "here is citable evidence you can trust without
re-deriving it," the evidence has a **24-hour shelf life and says nothing when it
expires**. Every `CONTENT` ObjectStore bucket is created with a hard-coded 24h TTL
(`storage/objectstore/store.go:114`), so verbatim bodies expire ~24h after ingest
while the entities referencing them live forever in `ENTITY_STATES`. A day after
ingest an operator sees correct recall, correct ranking, `embedding.ready: true`,
`indexed_revision == target_revision`, and **ranked nodes with empty bodies** —
every signal green, the answer citing nothing (#600). Fusion is where that becomes
invisible: it discards both the `Hydrate` and `ResolveBody` errors and ships an
empty `Body` with no signal at any layer (`engine_lens.go:349-355`, #616).

This is Epic A's literal case — evidence that *silently* expires — and it directly
contradicts the substrate's own position: ADR-0008 rejects reference-blind NATS
retention on the live graph, and `ENTITY_STATES` is boot-defended by
`AssertNoLifecycleRetention`. `CONTENT` gets no such guard.

## What Changes

- **Make the ObjectStore body-store TTL configurable, defaulting to OFF** for
  content-addressed body stores (replaces the hard-coded `24 * time.Hour`).
- **Extend the `AssertNoLifecycleRetention`-class boot guard to `CONTENT`**, so a
  lifecycle policy on a body store fails loudly at boot rather than silently
  emptying answers a day later.
- **Surface body-hydration outcome separately from seed hydration in fusion**, with
  a bounded reason set and a metric — a missing body becomes a reportable partial
  result, not an empty string. Precedent in-package: `engine_graph.go:20-41`
  reports every graph-facet cap through its own metadata; `Response.Unhydrated`
  today covers only seeds (`contract.go:239`), not bodies. **BREAKING** if the
  fusion response contract gains a body-hydration field consumers must read.
- **Orphan reclamation for content-addressed blobs (the coupled half — see below).**

## The coupling that decides this change's scope

**The accidental 24h TTL is currently the ONLY thing reclaiming orphaned blobs.**
Bodies are content-addressed (`doc:<sha>`/`code:<sha>`); an edit writes a new key
*alongside* the old and strands the previous object. There is no refcount, sweep,
or GC in either repo. **Removing or disabling the TTL without an orphan-reclamation
story converts the blob store into genuinely unbounded growth.** #600 is explicit:
"both halves belong to one retention design; please do not fix half of it."

This is the design phase's central question, and it may be ADR-scale (a
content-addressed blob GC / refcount / per-source retention-depth design; ADR-0008
open item #5 already sketches per-source retention). The design must resolve
whether increment 2:
  (a) ships all of it (TTL-off + boot guard + fusion reporting + orphan GC), or
  (b) ships the **loud-not-silent** half now (boot guard + fusion reporting +
      configurable TTL, default LEFT at a safe value) and sequences TTL-default-off
      behind the orphan-GC design as its own increment.

Recommendation to carry into design: option (b) unblocks the *silent* defect —
the theme of Epic A — without the unbounded-growth risk, and keeps the GC design
from gating the observability fix. The owner/architect decides.

## Capabilities

### New Capabilities

<!-- Possibly `content-store-retention` (the TTL contract + boot guard + orphan
     reclamation) if the objectstore capability spec does not exist yet — confirm
     in design against openspec/specs/. -->

### Modified Capabilities

- `fusion`: body-hydration outcome is reported separately from seed hydration
  (bounded reason set + metric), so a missing body is a partial-result signal, not
  silence.
- `graph-retention` (or a new `content-store-retention`): the CONTENT body store's
  lifecycle contract — configurable TTL, default off, boot-guarded — plus the
  orphan-reclamation invariant. Confirm the exact spec home in design; this may be
  where the ADR-068 retention contract extends to blobs.

## Impact

- **Code:** `storage/objectstore/store.go` (TTL config), a boot guard analogous to
  `processor/graph-ingest/component.go:1107`'s `AssertNoLifecycleRetention`,
  `pkg/fusion/engine_lens.go` + `contract.go` (body-hydration reporting).
- **Sister repos:** semsource attaches every content store through the framework
  constructor (`ast-source/bodystore.go`, `doc-source`, `code-context`,
  `supersession`); a TTL default change and any new required config surface reaches
  them — coordinate via `semstreams-asks`. #616's fusion-contract change is
  consumed by fusion callers (mcp-gateway, semsource).
- **Retention/GC:** the orphan-reclamation design is new substrate work (no GC
  exists today) and is the increment's scope-defining decision.
- **Related:** #599 (no e2e tier exercises fusion Fuse / batch reconciliation /
  unhydrated reporting — the coverage gap that let this hide), #601 (offloaded
  title/`text_suffixes`, adjacent on the offloaded lane), ADR-0008 open item #5.

## Non-goals

- **Not** #601 (offloaded-entity title embedding) unless folded deliberately in
  design — it is a separate lane concern.
- **Not** the readiness-semantics decision #613 ("readiness attests we-stopped-
  trying") — a distinct ADR-084-frame owner call.
- **Not** a rewrite of the fusion degrade-don't-fail *policy* — that policy is
  correct; only its *silence* is the defect.
