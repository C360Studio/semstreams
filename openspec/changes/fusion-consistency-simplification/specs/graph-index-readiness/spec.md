# graph-index-readiness — delta

## REMOVED Requirements

### Requirement: Consumers gate through the canonical readiness gate with a declared mode

**Reason**: the four modes were four policies over conflated questions
(ADR-083 Consequences). `exact` and `degrade-honest` are one evaluation whose
only difference is the caller's reaction (degrade-honest has zero callers —
it existed as call-site documentation), and `sticky-bootstrap` is
graph-index's private bootstrap concern, not a shared policy. The mode
taxonomy is superseded by the two-question gate below (ADR-084).

**Migration**: consumers call the collapsed gate (health + a declared
freshness requirement); graph-index keeps bootstrap exactness internally;
callers that degraded instead of erroring keep doing so at the call site.

## ADDED Requirements

### Requirement: The envelope reports bootstrap completion

The readiness envelope SHALL carry `bootstrap_complete`: true once the
producer's initial build — enumeration plus replay to the enumeration-time
target, including the authoritatively-empty outcome (enumeration delivered,
zero entities) — has completed in this process lifetime, and false again
after a restart until the rebuild completes, so a restart into a format
cutover re-gates. This makes the gh#474 cutover window wire-observable: a
rebuild in progress is no longer byte-identical to ordinary catch-up on a
built index. An absent field (older producer) SHALL read as false (fail
closed); this is an accepted lockstep-upgrade cost.

#### Scenario: A cutover rebuild is distinguishable from ordinary lag

- **GIVEN** graph-index restarts into a format cutover and is re-materializing
  indexes from ENTITY_STATES
- **WHEN** it publishes the envelope during the replay
- **THEN** `bootstrap_complete` is false while `State` is building
- **AND** after the replay completes, subsequent envelopes report
  `bootstrap_complete` true — including under later ordinary lag

#### Scenario: An authoritatively empty graph reports bootstrap complete

- **GIVEN** a fresh deployment whose initial enumeration delivers zero entities
- **WHEN** the envelope is published
- **THEN** `bootstrap_complete` is true and `Ready` is true (the empty-graph
  encoding), so gated reads serve

### Requirement: Consumers gate on health and a declared freshness requirement through the canonical gate

The canonical readiness gate SHALL evaluate exactly two questions over the
held status and its consumer-local freshness. *Health*: the status is fresh;
no hard stop (`degraded`, `reset_required`); and `bootstrap_complete` is
true. *Freshness*: the consumer's declared requirement — `exact` (proceed
only when caught up), a `max_staleness` bound, or none. Lag alone SHALL NOT
defer a consumer that declared no freshness requirement; coverage (`Ready`)
MAY license a proceed (a caught-up index answers the freshness question with
zero age) but SHALL NOT be required by the health question. When a bound is
declared, the gate SHALL compare the view's age including consumer-local
delivery age (`staleness_ms` plus the reading's age) against the bound, so a
bound cannot be silently exceeded by the heartbeat window, and SHALL treat a
not-ready envelope whose staleness is not computable (`staleness_ms` absent)
as over the bound. Status-unknown SHALL fail closed (`allow_ungated_reads`
remains the explicit deployment escape, applying to exactly the unknown
branch). The gate SHALL live in one canonical helper beside the envelope
type; per-consumer hand-rolled gate logic is prohibited. The typed defer
reasons remain closed: `hard_stop`, `over_staleness`, `status_unknown`, and
`bootstrap_incomplete` (renamed from `empty`, which it now precisely means).

#### Scenario: Ordinary catch-up lag does not defer an unbounded consumer

- **GIVEN** a healthy, built index catching up under continuous write
  (`Lag > 0`, `bootstrap_complete` true, no hard stop)
- **WHEN** a consumer with no declared freshness requirement evaluates the gate
- **THEN** it proceeds, and the envelope reports the current `staleness_ms`

#### Scenario: A caught-up index passes every freshness requirement

- **GIVEN** a healthy, caught-up index (`Ready` true, `staleness_ms` reported
  as 0-not-computed per the presence encoding)
- **WHEN** a consumer with a declared `max_staleness` bound evaluates the gate
- **THEN** it proceeds — caught-up answers the freshness question with zero
  age and is never deferred as unknown-staleness

#### Scenario: Hard stops, unknown status, and incomplete bootstrap always defer

- **GIVEN** `State ∈ {degraded, reset_required}`, or a stale/absent status
  feed, or `bootstrap_complete` false
- **WHEN** the gate is evaluated with any freshness requirement
- **THEN** it defers (fail closed) with the typed reason

#### Scenario: The exact freshness requirement preserves the strict default

- **GIVEN** a view-rate consumer whose `max_staleness` is unset or zero (the
  documented "require exact index catch-up" operator contract)
- **WHEN** the index is healthy but lagging
- **THEN** the gate defers with reason `over_staleness` — serving under lag
  remains an explicit opt-in for view-rate consumers

#### Scenario: A declared staleness bound is the only freshness dial

- **GIVEN** a view-rate consumer with `max_staleness` configured
- **WHEN** the view's age (staleness plus delivery age) exceeds the bound
- **THEN** the gate defers with reason `over_staleness`
- **AND** no revision-count tolerance exists anywhere on the gate surface

## MODIFIED Requirements

### Requirement: Ready reports exact revision coverage

The readiness envelope's `Ready` bool SHALL be true only when the index has
applied every committed ENTITY_STATES revision at compute time (`target > 0 &&
indexed >= target`, or the authoritatively-empty override) AND no required
index write is unresolved. `Ready` is observability and a caught-up fast
path: it MAY license a proceed (the freshness question answered with zero
age), but no read path SHALL defer on `!Ready` alone — with one carve-out:
graph-index's private pre-bootstrap exactness gate, which is the
`bootstrap_complete` condition evaluated in-process. No consumer SHALL treat
an empty result as an authoritative not-found under any envelope state —
coverage says nothing about whether the source ever published the thing being
looked for (ADR-084 retires ADR-066's authoritative-absence license).
`IndexedRevision >= myRev` remains the caller-supplied read-your-writes
check, the only legitimately per-entity one; the mutation response's
`kv_revision` and the envelope's revision space SHALL be pinned as comparable
by a test.

#### Scenario: Ready stays exact and observable under continuous write

- **GIVEN** a bucket under continuous write so `Lag > 0` at compute time
- **WHEN** any consumer decodes the envelope from `GRAPH_STATUS`
- **THEN** `Ready` is false while `staleness_ms` reports the view's age when
  computable (>= 1ms)
- **AND** healthy read paths still serve, with that staleness observable

#### Scenario: Empty results license nothing

- **GIVEN** a query returning no rows under any envelope state
- **WHEN** a consumer interprets the result
- **THEN** no correctness argument may treat the emptiness as proof of absence

### Requirement: View-rate readiness interpretation with hard stops

A view-rate consumer SHALL gate through the canonical gate with a declared
`max_staleness` (a view-rate consumer re-derives its whole result each pass,
e.g. community detection). Unset or zero `max_staleness` SHALL keep meaning
"require exact index catch-up" — the strictest reading, preserving the
shipped operator contract; a duration relaxes only the freshness question.
Hard stops and incomplete bootstrap defer under every value. The unit is
wall time; no revision-count tolerance exists.

#### Scenario: Zero tolerance requires exact catch-up

- **GIVEN** `max_staleness` unset or zero and a healthy index with `Lag > 0`
- **WHEN** the view-rate consumer evaluates the gate
- **THEN** it defers until the index is caught up — bit-compatible with the
  pre-ADR-084 exact default

#### Scenario: A bounded view runs under bounded lag

- **GIVEN** `max_staleness: "3s"` and a healthy, built index whose view age
  is within the bound
- **WHEN** the view-rate consumer evaluates the gate
- **THEN** it proceeds, with the age observable in the defer/proceed telemetry

### Requirement: Community detection runs under bounded lag

Community detection SHALL gate each pass through the canonical gate with its
configured `max_staleness` (default unset = exact catch-up), SHALL defer on
hard stops, incomplete bootstrap, and unknown status under every
configuration, and SHALL surface every defer through the structured defer log
and `defer_total{reason}` counter. Detection under a relaxed bound SHALL
remain observable (staleness at detection time exported), so an operator can
correlate community churn with view age.

#### Scenario: Default preserves the exact gate

- **GIVEN** a deployment that never set `max_staleness`
- **WHEN** continuous write keeps `Lag > 0`
- **THEN** detection defers exactly as the pre-ADR-084 default did

#### Scenario: A bounded deployment detects under lag

- **GIVEN** `max_staleness: "3s"` under continuous write with view age within
  the bound
- **WHEN** the detection tick fires
- **THEN** detection runs and `staleness_at_detection` records the age

### Requirement: Read consumers retry the readiness transient

Reverse-index and by-name read handlers SHALL return the classified transient
`ErrorCodeIndexNotReady` only for health failures — hard stops, status-
unknown, and bootstrap-incomplete (the gh#474 cutover window) — and SHALL NOT
return it for ordinary catch-up lag on a healthy, built index. Consumers
SHALL detect it via `errs.IsTransient` (never by message text) and retry;
bounded retry converges once the health condition clears, with two stated
carve-outs: `reset_required` is fatal at the responder
(`ErrorCodeGraphStateResetRequired`) and never self-clears, and emitters
outside the reverse-index read contract (lifecycle, rule, spatial, temporal,
embedding, ingest — responder-up / watcher-health semantics) keep their
existing meanings. This deliberately supersedes the #592 close-out for read
paths: the transient no longer fires on plain lag, so retrying it stops being
the response to plain lag.

#### Scenario: A read during ordinary catch-up serves

- **GIVEN** a healthy, built index catching up after a write burst
- **WHEN** a reverse-index or by-name read arrives
- **THEN** it is served (no transient), with staleness observable on the
  GRAPH_STATUS envelope

#### Scenario: A read during the cutover window is retryable

- **GIVEN** the index is bootstrap-incomplete or degraded by unresolved write
  failures
- **WHEN** a reverse-index or by-name read arrives
- **THEN** it returns the classified `ErrorCodeIndexNotReady` transient
- **AND** a consumer that retries converges once health is restored

### Requirement: Fusion degrades consistently on the readiness transient

The fusion engine SHALL gate `Fuse` through the canonical health gate
(adopting it — the current top gate is a hand-rolled `!Ready` check), with no
declared freshness requirement: it SHALL proceed under ordinary catch-up lag,
reporting `staleness_ms` on the envelope, and SHALL return the empty-honest
envelope (fail closed, carrying the last-known `IndexStatus`) only on health
defers — status-unknown, hard stops, or incomplete bootstrap. A status
*wiring* failure (the transport cannot watch GRAPH_STATUS) SHALL remain a
loud error, never degraded to an eternal empty envelope; only a quiet or
stale feed defers. Fuse SHALL NOT offer an ungated-reads escape (it is a
shared product surface, not a standalone deployment — the asymmetry with
`allow_ungated_reads` is deliberate). When `Resolve`, `Entities`, or the
relations neighbor expansion returns the classified
`ErrorCodeIndexNotReady` (now health-scoped), `Fuse` SHALL degrade to the
same empty-honest envelope rather than propagating a hard error, and a
degraded envelope SHALL NOT carry `State="ready"`. Genuine, non-transient
errors SHALL still propagate. The facet walks (impact / paths / graph
projection) remain out of scope: they carry their own per-facet honesty
markers (`Truncated`; the graph facet carries no coherence claim — see the
fusion capability spec).

#### Scenario: Fuse serves ranked evidence under lag

- **GIVEN** a healthy, built index with `Lag > 0` and a query with matching
  entities
- **WHEN** `Fuse` runs
- **THEN** it returns ranked results with `staleness_ms` reported, not an
  empty envelope

#### Scenario: A quiet status feed defers; a wiring failure errors

- **GIVEN** a fusion deployment whose status feed goes quiet past the
  freshness window
- **WHEN** `Fuse` runs
- **THEN** it returns the empty-honest envelope (fail closed)
- **AND** a transport that cannot watch GRAPH_STATUS at all yields a loud
  error instead, never an eternal empty envelope

#### Scenario: A health-scoped transient degrades, not errors

- **GIVEN** a core read inside `Fuse` returns the classified readiness
  transient
- **WHEN** `Fuse` handles it
- **THEN** it returns the empty-honest envelope, the same degrade as its top
  gate

#### Scenario: A genuine error still propagates

- **GIVEN** an internal read returns a non-transient error (e.g. a real
  decode or connection failure)
- **WHEN** `Fuse` handles it
- **THEN** it propagates the error (not degraded to an empty envelope)
