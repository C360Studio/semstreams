# fusion-consistency-simplification — design

## Context

ADR-083 distributed readiness as GRAPH_STATUS KV state and (Break 3) deleted
`ViewRevision.Coherent`. What remains is the conflation ADR-083's
Consequences named: four needs under one word — health, freshness,
read-your-writes, authoritative absence — with the fourth unanswerable.
Where it bites (all code-verified): fusion's top gate is a hand-rolled
`!status.Ready` (`engine_lens.go:87` — fusion never adopted the canonical
gate); the only per-call lag-exact transient is the graph/query client's
direct INCOMING_INDEX gate (`client.go:521-554`) — the graph-index responder
is sticky post-bootstrap and never lag-gates once built; the four gate modes
include one with zero callers (`degrade-honest`); and batch hydration
silently omits KV-not-found IDs (gh#597's confirmed drop path).

Constraints: GRAPH_STATUS transport, heartbeat cadence, and hard-stop
semantics (ADR-082/083) are settled. gh#474's guards must not weaken.
graph-ingest stays the sole ENTITY_STATES writer. Sister-repo code is
owner-managed.

**Review record (2026-07-20).** The 5-lens adversarial review (architect,
breaker, feasibility, code-accuracy, completeness) returned
READY-WITH-CHANGES. Three blocking findings — each independently found by
2–4 lenses and verified against source — were folded and reshaped D1/D2:
(1) "coverage is never a gate input" + "TargetRevision=0 defers" jointly
re-broke the authoritatively-empty graph (`applyKnownIncompleteOverrides`
encodes it only via `Ready=true` at `Target=0`); (2) bootstrap-incomplete was
not wire-observable, so a health-only client gate would serve the gh#474
half-built index; (3) the `staleness_ms` presence encoding (0 on every
caught-up envelope) wedged every bounded consumer exactly when the index
caught up. One additive envelope bit (`bootstrap_complete`) plus
"coverage may license a proceed, never defer a read path" resolves all
three. The review also refuted two Context claims (folded: the transient's
doc-comment in `mutation_responses.go` documents catch-up as the trigger —
this change *redefines* the contract, it does not restore one; semsource's
fusion-surface write-burst symptom was `Ready=false` empty envelopes, not
transients) and surfaced a live ranking bug now fixed by D4 (batch hydration
returns cache-hits-first while ranking is position-based, so cache residency
demotes the resolve-top entity — a plausible ingredient of gh#597's varying
failure mode).

## Goals / Non-Goals

**Goals**: one health-shaped gate; a single freshness parameter; hydration
failures visible end-to-end; minimal score passthrough; ADR-084 recording
"readiness licenses health, never absence".

**Non-Goals**: coherence/snapshot semantics (graphview); absence licenses
under any name; transport changes; full scoring explain-plan; sister-repo
migrations (proposal Non-goals).

## Decisions

**D1 — ~~Two questions; freshness is a parameter, not a mode.~~ SUPERSEDED by
D8 (ADR-085) — the freshness parameter is DELETED, not reparameterized.**

*Recorded as written, because the reasoning below is why D8 exists.* The
original decision: Health = fresh status ∧ no hard stop ∧
`bootstrap_complete`. Freshness = the consumer's declared requirement:
`exact` | `max_staleness(d)` | none. Lag alone never defers an unbounded
consumer; coverage (`Ready`) may still *license a proceed* but never defers a
read path. The bound compared `staleness_ms` + the reading's consumer-local
age. View-rate consumers kept `exact` as the unset/zero default; read paths
declared none. Defer reason `empty` renamed to `bootstrap_incomplete`.

**What survives:** the health definition, the `bootstrap_complete` bit, the
`empty` → `bootstrap_incomplete` rename, and the finding that coverage must
never defer a read path.

**What does not:** everything about a declared freshness requirement. The
`Freshness` type, its three constructors, `max_staleness`, the bound
arithmetic, and the `over_staleness` / `staleness_unknown` defer reasons are
deleted. `EvaluateReadinessGate` takes a `StatusReading` and nothing else,
and `Ready` is inert at the gate — it neither licenses nor withholds. See D8.

**D2 — `bootstrap_complete` on the envelope.** Producer-set: true once the
initial build (enumeration + replay to the enumeration-time target,
including the authoritatively-empty 0/0 outcome) completes in this process
lifetime; false again after restart until rebuilt — a restart into a cutover
re-gates. graph-index derives it from its existing bootstrap latch;
graph-embedding from its own bootstrap. Absent field (older producer) reads
false — fail closed, an accepted lockstep-upgrade cost. This makes the
gh#474 window wire-observable for direct-bucket clients and replaces
`TargetRevision=0` as the pre-enumeration signal (which was false during
cutovers and wrongly deferred the empty graph).

**D3 — Read paths regate on health; the responder is already there.**
`graph/query`'s `indexNotReadyErr` swaps exact-`Ready` for the health
question (now evaluable client-side via D2). The graph-index responder
changes only nomenclature: its pre-bootstrap exactness gate IS
`bootstrap_complete` evaluated in-process and is retained unchanged —
implementers must NOT fold status ahead of the sticky flag
(`processor/graph-index/query.go:176-182` pins post-bootstrap serving under
stuck-watermark degraded as deliberate; reset/failedCount are checked
in-process on every query regardless). Supersedes the #592 close-out for
read paths; recorded in ADR-084, cross-referenced on #592, same tag as
#598's wave.

**D4 — Omission reported with set semantics; resolve order restored.**
Batch response gains `missing: [{id, reason}]`, reason ∈ {`not_found`,
`error`} (`error` reserved while the first-error contract stands — a
non-not-found fault still fails the call). `fusionnats.Entities` reconciles
as ID-sets: handler report authoritative; synthesize `unknown` only for IDs
in neither list; one entry per ID; then **re-order hydrated entities to
resolve order** before returning (fixes the live cache-order ranking
scramble; a fixture pins ranking before any seed plumbing changes). Fusion
`Response` carries `unhydrated` (distinct from `Misses`; the all-seeds-
unhydrated case synthesizes no Miss). A `Miss` is explicitly de-licensed
(reachable under lag now); stale "Miss only when Ready" contract comments
are swept. *Rejected*: folding into `Misses` (re-blurs the gh#597
distinction); failing the call on any omission (partiality isn't the bug,
invisibility is); count-based reconciliation (breaks on dup IDs and
handler+client double-report).

**D5 — Score passthrough, joined by ID.** Resolve rank always carried;
similarity only where the resolve mode provides one (semantic — symbol and
prefix wires carry none). Joined to nodes by entity ID, never slice
position (position is what D4 just fixed). Opt-in request bool
(`include_scores`); omitempty wire fields; the request flag gets a JSON
round-trip test (operator-surface discipline). No `RetrievalClient.Resolve`
break needed for rank (position-derived post-reorder); similarity rides the
existing decode struct gaining one field.

**D6 — Status-unknown at Fuse: defer; wiring failure: error.** `fusionnats`
distinguishes the permanent wiring failure (transport cannot watch
GRAPH_STATUS — stays a loud error) from quiet/stale-feed unknown (returns a
typed readiness-unknown the engine maps to the empty-honest defer envelope).
Fuse gets NO ungated escape — deliberate asymmetry with
`allow_ungated_reads` (fusion is a shared product surface, not a standalone
deployment). Resolves the former open question.

**D7 — ADR-084 is decision-only** (see the ADR for the full statement):
license retired; D4-mode table superseded; #592 superseded for read paths;
hard-stop binding scoped (envelope consumers at heartbeat cadence; responder
in-process checks stronger; stuck-watermark post-bootstrap serving is the
one kept exception); `staleness_ms` documented as a complete-coverage floor,
not a snapshot age.

## Risks / Trade-offs

- [semsource's `Ready=false → fall back` changes meaning] → migration doc in
  the same wave; envelope still carries `Ready`+`staleness_ms`; owner
  manages lockstep; tag held until this lands.
- [gh#474 guards] → `bootstrap_complete` carries the guard to the wire; hard
  stops unconditional; pins land BEFORE the regate (tasks 2.3): empty-graph
  proceeds, cutover defers at the client, pre-bootstrap transient at the
  responder, failedCount override, ranking fixture.
- [Hard-stop propagation is heartbeat-delayed for envelope consumers (≤5s)]
  → accepted ADR-083 cost, now stated in ADR-084 and pinned with a latency
  note, not discovered in production.
- [`unhydrated`/`missing` invite the inverse absence inference] → spec pins
  both directions license nothing; sister adoption reviews watch for it.
- [Returning stale evidence where empty was returned before] → staleness on
  the envelope; bounded consumers declare bounds; recorded as the deliberate
  #592 reversal.
- [`staleness_ms` read as snapshot age] → documented as complete-coverage
  floor (field docs + migration notes); snapshot consumers → graphview.

## Migration Plan

1. Land this change (framework side; BREAKING semantics, pre-1.0 clean
   break; `bootstrap_complete` additive).
2. Tag TOGETHER with #598's breaks (one wave, one sister migration).
3. Owner-managed lockstep: semsource (gate/fallback semantics, unhydrated
   consumption, scorecard adopts score passthrough), semconnect (conformance
   probe reads health not Ready).
4. Rollback = revert the framework commit; no data migration exists.

**D8 — Delete the freshness parameter; gate on health alone (ADR-085).**
Supersedes D1's freshness half. Coverage-derived gating existed only to serve
the ADR-066 absence license; ADR-084 retired the license but left the
machinery. What remained was a public type, three constructors, a
satisfiability floor, two defer reasons and an operator knob serving **one**
call site — and that consumer (community detection) is the safest possible
reader of a stale view. Three tells, all verified: `FreshnessWithin` had
exactly one caller; `max_staleness` / `index_lag_tolerance` had zero adopters
across ~20 sister repos; and the knob needed a floor derived from the publish
heartbeat, because you cannot bound a quantity below the interval at which
you learn it. `max_staleness` also never shipped in a tag, so sister repos go
from `index_lag_tolerance` to nothing. The gate becomes fresh → recognized
state → no hard stop → `bootstrap_complete` → proceed. Staleness is reported
on results (`staleness_at_detection_ms`), never consulted for admission.
*Alternatives rejected*: a better-derived floor (the floor is the symptom);
keeping the parameter defaulted to none (preserves a type and two defer
reasons for a hypothetical consumer that would want stamping, not gating).

**D9 — Ungating a periodic rebuild requires that rebuild to be
non-destructive.** Removing a gate promotes the gated work's failure modes
from rare to permanent. Community detection replaced its partition by
clearing COMMUNITY_INDEX and rebuilding, which was tolerable at a rate of
approximately never and indefensible every tick. Detection now writes the new
partition over the prior one in place and prunes only the keys the run did
not write; a failed prune leaves a correct superset rather than failing the
run. **Consumer-side caveat, found by review and not waved away:** the
prune's deletes now arrive after the writes, which broke
`processor/graph-query/CommunityCache` — it keyed by bare community ID while
storage keys by `{level}.{id}`, so a late delete rebuilt a level index after
higher levels had shadowed the map. That cache is fixed to key by
`(level, ID)` as part of this change (gh#609 item 1); the union-during-
rebuild guarantee is only true once it is.

## Open Questions

None — the three former open questions are resolved: wire names are
`unhydrated` (fusion Response) and `missing` (batch response) with the
closed reason enum `not_found`/`error`/`unknown`; score opt-in is a request
bool with a JSON round-trip test; Fuse fails closed on status-unknown with
no ungated escape (D6).
