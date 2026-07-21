# ADR-084: Readiness Licenses Health, Never Absence

## Status

**D1's freshness parameter and D5's bounded-staleness clause superseded by
ADR-085 — 2026-07-21.** This ADR retired the absence license that coverage-gating
existed to serve, but kept the gating machinery as a per-consumer *parameter*
(`exact` | `max_staleness` | none). ADR-085 finishes the removal: the gate asks
health alone, staleness is reported on results rather than consulted for
admission, and `Freshness`/`max_staleness` are deleted. Decisions 2, 3, 4, 6 and 7
below — `bootstrap_complete`, the retired absence license, the narrowed transient,
reported omission, and hard-stop propagation — stand unchanged and are load-bearing
under ADR-085. One further clause of D5 is also superseded: "`sticky-bootstrap`
returns to graph-index as private bootstrap exactness (*its responder gate is
retained unchanged*)". The responder gate was NOT retained unchanged — ADR-085
Alternative 3 records why the exactness there was a bootstrap probe that
`bootstrap_complete` answers directly.

Accepted — 2026-07-20, after the 5-lens adversarial review (architect /
breaker / feasibility / code-accuracy / completeness; all blocking findings
verified against source and folded — see the change's design.md). Decision
record for the `fusion-consistency-simplification` change. Builds on ADR-066
(honest readiness), ADR-082 (consumer-class split, hard stops), and ADR-083
(distributed state, staleness in time). **Retires ADR-066's "authoritative
not-found" license**, **supersedes ADR-083 D4's gate-mode taxonomy**, and
**deliberately supersedes the #592 close-out** for read paths (the transient
stops firing on ordinary lag, so "retry the transient" stops being the
prescribed response to it). ADR-066, ADR-082, and ADR-083 otherwise stand;
they receive narrow pointer notes, not retrofits.

## Context

ADR-083's Consequences named the conflation: four needs travelled under the
one word "readiness" — (1) health / not-mid-rebuild, (2) view freshness,
(3) read-your-writes, (4) authoritative absence — and the fourth is not
answerable by any readiness signal, because coverage says nothing about
whether a source ever published the thing being looked for.

Where the conflation bites, stated precisely (each verified against code):

- **Fusion's top gate defers on `!Ready`** (`pkg/fusion/engine_lens.go:87`, a
  hand-rolled check — fusion never adopted the canonical gate). Under a write
  burst every `Fuse` call returns an empty-honest envelope until the index
  catches up: coverage gating an evidence-retrieval surface that only needed
  health. This — not the transient — was semsource's write-burst symptom on
  the fusion surface.
- **The per-call lag-exact transient exists in exactly one place**: the
  `graph/query` client's direct INCOMING_INDEX gate
  (`graph/query/client.go:521-554`), which fires `index_not_ready` on any
  `!Ready`, i.e. on ordinary catch-up. The graph-index responder itself is
  sticky post-bootstrap and never gates on lag once built
  (`processor/graph-index/query.go:176-226`). One comment
  (`client.go:501-506`) frames the client gate's job as "the cutover/failure
  window"; `graph/mutation_responses.go:115-124` documents catch-up as the
  trigger. This ADR *redefines* that contract to the cutover/failure window —
  it does not restore a prior one.
- **The four gate modes** are four policies over the conflated questions:
  `exact` and `degrade-honest` are literally one evaluation (the latter has
  zero callers — it exists as call-site documentation), and `sticky-bootstrap`
  is graph-index's private bootstrap concern.
- **Silent absence in the field (gh#597)**: batch hydration silently omits
  KV-not-found IDs (`processor/graph-ingest/query.go:562-568`) and
  `fusionnats.Entities` never reconciles against the requested set — "I could
  not fetch it" is indistinguishable from "it does not exist" all the way to
  a UI that deleted items on that inference.

## Decision

1. **A readiness gate asks exactly two questions.** *Health*: status is fresh;
   no hard stop (`degraded`, `reset_required`); the producer's initial build
   is complete (`bootstrap_complete`, decision 2). *Freshness*: the consumer's
   declared requirement — `exact` (caught up), a `max_staleness` bound, or
   none. The freshness requirement is a **parameter with two degenerate
   endpoints, not a mode**: the health question is identical for every
   consumer, and lag alone never defers a consumer that declared no bound.
   Evidence-retrieval read paths (fusion, the graph/query client) declare
   none; view-rate re-derivers (community detection) keep `exact` as their
   default — an unset `max_staleness` continues to mean "require exact
   catch-up" (the shipped operator contract; serving under lag stays an
   explicit opt-in). Coverage may still *license a proceed* — a caught-up
   index answers the freshness question with zero — but coverage SHALL NOT
   *defer* a read path.

2. **Health becomes fully wire-observable: the envelope gains
   `bootstrap_complete`.** Today "bootstrap-incomplete" lives in graph-index
   process-local atomics; on the wire a cutover replay is byte-identical to
   ordinary catch-up, and an authoritatively-empty graph is encoded only via
   `Ready=true` at `TargetRevision=0`. Both blocked a health-only gate: the
   review showed a health gate without this bit either serves the gh#474
   half-built index (partial topology to the anomaly detector) or, if
   `TargetRevision=0` is used as the pre-enumeration proxy, re-breaks the
   authoritatively-empty graph (defers every read forever — the exact bug
   `applyKnownIncompleteOverrides` fixed). `bootstrap_complete` is set by the
   producer once its initial build (enumeration + replay to the
   enumeration-time target) completes in this process lifetime — including
   the authoritatively-empty case — and resets on restart, so a restart into
   a cutover re-gates. Absent field (older producer) reads false: fail
   closed, an accepted lockstep-upgrade cost. The `empty` defer reason is
   renamed `bootstrap_incomplete` to match what it now means.

3. **The absence license is retired.** No signal in the system — `Ready`,
   emptiness, a fusion `Miss` (which becomes reachable under lag and licenses
   nothing), an unhydrated list, or any future field — licenses the claim
   "not returned ⟹ not in the graph". Read-your-writes remains the one sound
   per-entity check (`IndexedRevision >= myRev`, revision supplied by the
   caller from the mutation response's `kv_revision`; the revision spaces
   agree and a test SHALL pin that). Consumers needing snapshot-consistent
   views use graph-view subscriptions (ADR-081).

4. **The classified `index_not_ready` transient means health failure** — hard
   stops, status-unknown, bootstrap-incomplete — and never fires for ordinary
   catch-up on a healthy, built index. Bounded retry converges once the
   health condition clears, with two carve-outs stated rather than implied:
   `reset_required` is fatal at the responder and never self-clears (operator
   action), and emitters outside the reverse-index read path (lifecycle,
   rule, spatial, temporal, embedding, ingest — all responder-up /
   watcher-health semantics) keep their meanings; the narrowing applies to
   the reverse-index/byName read contract.

5. **The gate-mode taxonomy collapses.** `exact`/`degrade-honest` were one
   evaluation with different caller reactions; reaction stays at the call
   site. `sticky-bootstrap` returns to graph-index as private bootstrap
   exactness (its responder gate is retained unchanged). `bounded-staleness`
   becomes the freshness parameter. ADR-083 D4 is superseded by the
   two-question gate. The freshness comparison accounts for delivery age
   (`staleness_ms` + consumer-local reading age vs the bound), so a bound
   cannot be silently exceeded by the heartbeat window.

6. **Omission is reported, never inferable.** Batch hydration reports every
   requested ID it does not return (closed reason set: `not_found` / `error`
   / `unknown`); the fusion response carries what it failed to hydrate,
   reconciled as ID-sets (the handler's report is authoritative; clients
   synthesize `unknown` only for IDs in neither list; one entry per ID);
   hydration results are re-ordered to resolve order before ranking. By
   decision 3, these lists say what was not returned — never what does not
   exist, in either direction: an entity absent from both lists licenses
   nothing, and an entry's `not_found` licenses no deletion.

7. **Hard stops bind every envelope-gated consumer, with the propagation
   delay and one responder exception stated.** Envelope consumers see a hard
   stop at heartbeat cadence — the trust in the unconditional
   failedCount→degraded override (ADR-082) is delayed by up to one heartbeat
   for out-of-process readers, an accepted ADR-083 cost. graph-index's own
   responder checks reset/failedCount in-process on every query (stronger
   than the heartbeat) but deliberately does not consult watermark-stall
   `degraded` post-bootstrap (`processor/graph-index/query.go:176-182` pins
   this as load-bearing); that exception predates this ADR and is kept.

## Consequences

Reads serve under lag. Fusion returns ranked evidence with `staleness_ms`
where it previously returned an empty-honest envelope; direct-index client
reads serve during catch-up where they previously errored. Products that
treated `Ready=false` or the transient as a fallback trigger see behavior
change — that is the point, and it is a breaking wave coordinated with
ADR-083's (one tag, one sister migration).

`staleness_ms` is a **complete-coverage floor, not a snapshot age**: the
watermark is low-water-of-pending, so a served view additionally contains an
arbitrary subset of newer writes — it corresponds to no single instant.
Consumers that read it as "the graph as of T ago" will over-trust it; the
field documentation and migration notes say so, and snapshot consumers belong
on graphview.

What ADR-066's exact gate actually defended — "no symbol written in the last
N revisions is returned as an authoritative miss" — was never a sound absence
proof, only a narrower window on an unsound one; retiring the license makes
the unsoundness explicit instead of load-bearing. The sharpest new surface is
the fusion `Miss` under lag: a just-written, not-yet-indexed entity can now
return a miss with suggestions, so the spec de-licenses `Miss` explicitly and
the stale "a Miss only appears when Ready is true" contract comments are
swept.

The unhydrated lists invite the inverse inference decision 6 prohibits
("listed as not-found, so I may delete it"); the spec pins that neither
presence in nor absence from these lists is an existence claim, and sister
adoption reviews must watch for it.

Thirteen-odd concepts become four: healthy?, how stale?, is my write
visible?, and what failed to hydrate. Everything deleted was an
implementation of the question this ADR rules unanswerable.
