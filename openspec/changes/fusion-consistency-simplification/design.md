# fusion-consistency-simplification — design

## Context

ADR-083 distributed readiness as GRAPH_STATUS KV state and (Break 3) deleted
`ViewRevision.Coherent` after the owner's ruling that fusion cannot prove
coherence claims. What remains is the conflation ADR-083's Consequences named:
four needs travelled under one word — (1) health / not-mid-rebuild, (2) view
freshness, (3) read-your-writes, (4) authoritative absence — and the fourth is
not answerable by any readiness signal. Today's surfaces still encode the
fourth: `Fuse`'s top gate defers on `!Ready` (coverage), `graph/query`'s
reverse-index reads error `index_not_ready` on ordinary lag (their doc-comment
says the gh#474 cutover window is their job), the canonical gate offers four
modes, and batch hydration silently omits KV-not-found IDs (gh#597's confirmed
drop path: `fusionnats.Entities` never reconciles against the requested set;
`fetchEntitiesConcurrent` drops not-found silently).

Constraints: GRAPH_STATUS transport, heartbeat cadence, `max_staleness`, and
hard-stop semantics (ADR-082/083) are settled. gh#474's partial-topology guard
must not weaken. graph-ingest stays the sole ENTITY_STATES writer. Sister-repo
code is owner-managed.

## Goals / Non-Goals

**Goals**: one health-shaped gate for read paths; staleness as the only
freshness dial; hydration failures visible end-to-end; minimal score
passthrough; ADR-084 recording "readiness licenses health, never absence".

**Non-Goals**: coherence/snapshot semantics (graphview owns them); absence
licenses under any name; transport changes; full scoring explain-plan;
sister-repo migrations (see proposal Non-goals).

## Decisions

**D1 — The gate asks two questions: healthy? fresh enough?** The collapsed
evaluation: status fresh (else fail closed) → no hard stop
(`degraded`/`reset_required`) → not empty/pre-enumeration → optional
`max_staleness` bound. Coverage (`Ready`, lag) stops being a gate input;
it stays on the envelope as observability. *Alternative rejected*: keeping an
`exact` mode for "authoritative absence" consumers — the license was unsound
(ADR-083 Consequences; the ADR-066 not-found license is retired by ADR-084).
The empty/pre-enumeration defer is retained deliberately: it is a health
statement about the index (bootstrap incomplete), not an absence claim about
an entity.

**D2 — `Fuse` proceeds under lag.** The top gate defers (empty-honest envelope,
fail closed) only on status-unknown, hard stops, or empty index. Under
ordinary catch-up it proceeds and the envelope reports `staleness_ms`.
`exact` vs `degrade-honest` disappears: reaction to a defer was always the
caller's policy, never a distinct evaluation. The `isIndexNotReady` degrade
sites keep their shape — with D3, the transient they catch fires only in
genuine health windows.

**D3 — Read paths regate on health; sticky-bootstrap goes private.**
`graph/query`'s `indexNotReadyErr` and graph-index's own query gate fire on
hard stops, status-unknown, and bootstrap-incomplete — not on ordinary lag.
graph-index keeps its bootstrap exactness internally (its private concern, no
longer a shared gate mode). This supersedes the #592 close-out: "retry the
transient" narrows to genuine health windows and stops being the answer to
plain lag. Recorded in ADR-084, cross-referenced on #592, and coordinated in
the same tag as #598 (owner sequencing: no semsource tag before this lands).

**D4 — Hydration failures are reported, not inferable.** Three layers, one
correction: the `graph.query.batch` response gains an explicit
`missing: [{id, reason}]` list (`not_found` vs `error`), preserving partial
success while making omission visible; `fusionnats.Entities` reconciles
returned-vs-requested and synthesizes `reason:"unknown"` entries when an older
handler predates the field (mixed-version safe); the fusion `Response` carries
`unhydrated` (distinct from `Misses`: a miss is "resolution found nothing",
unhydrated is "resolution found a seed I could not fetch"). *Alternative
rejected*: folding into `Misses` — it would re-blur exactly the distinction
gh#597 needed. *Alternative rejected*: failing the whole call on any omission —
partial evidence is fusion's contract; invisibility, not partiality, is the bug.

**D5 — Minimal score passthrough.** `resolveSemantic` stops discarding
`SearchResult.Similarity`; an opt-in request flag surfaces per-node
`resolve_rank` and `score` (omitempty). Off by default; no lens internals
exposed.

**D6 — ADR-084 is decision-only.** Records: readiness licenses health, never
absence; ADR-066's "authoritative not-found" license retired; ADR-083 D4's
mode table superseded by the two-question gate; #592 superseded per D3.
Narrow pointer notes on ADR-066/082/083 (no retrofits, house pattern).

## Risks / Trade-offs

- [semsource's `Ready=false → fall back` path changes meaning] → migration doc
  in the same wave; envelope still carries `Ready`+`staleness_ms` so the old
  policy remains expressible caller-side; owner manages the lockstep PR; tag
  held until this change lands.
- [Weakening the gh#474 cutover guard] → hard stops stay unconditional in the
  collapsed gate; bootstrap-incomplete stays deferred; the
  failedCount→degraded override (ADR-082) is the load-bearing health signal —
  pinned by tests before the regate lands.
- [Consumers treating `unhydrated` as an absence signal in reverse] → field
  docs state it lists only fetch failures; spec scenario pins that an entity
  absent from both `Nodes` and `unhydrated` still licenses nothing.
- [Returning stale evidence where empty was returned before] → staleness is on
  the envelope; products that need bounded freshness set the bound; recorded
  as the deliberate #592 reversal, not a side effect.

## Migration Plan

1. Land this change (framework side; BREAKING semantics, pre-1.0 clean break).
2. Tag TOGETHER with #598's breaks (owner decision: one migration wave, one
   set of release notes — semsource migrates the readiness surface once).
3. Owner-managed lockstep: semsource (gate/fallback semantics, unhydrated
   consumption, scorecard adopts score passthrough), semconnect (conformance
   probe reads health not Ready).
4. Rollback = revert the framework commit; no data migration exists.

## Open Questions

- Exact wire names (`unhydrated` vs `unresolved`; `missing` on the batch
  response) and whether reason strings are a closed enum on the wire.
- Score opt-in shape: request bool vs a `Want` value.
- Whether `Fuse` on status-unknown should honor an `AllowUngatedReads`-style
  escape (current lean: no — fail closed, same as every other consumer).
