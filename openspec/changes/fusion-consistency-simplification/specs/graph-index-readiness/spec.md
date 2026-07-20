# graph-index-readiness — delta

## REMOVED Requirements

### Requirement: Consumers gate through the canonical readiness gate with a declared mode

**Reason**: the four modes were four questions dressed as four policies over
one question (ADR-083 Consequences). With the coherence claim deleted, `exact`
and `degrade-honest` are one evaluation whose only difference is the caller's
reaction, and `sticky-bootstrap` is graph-index's private bootstrap concern,
not a shared policy. The mode taxonomy is superseded by the two-question gate
below (ADR-084).

**Migration**: consumers call the collapsed gate (health + optional
`max_staleness`); graph-index keeps bootstrap exactness internally; callers
that degraded instead of erroring keep doing so at the call site.

## ADDED Requirements

### Requirement: Consumers gate on health and freshness through the canonical gate

The canonical readiness gate SHALL evaluate exactly two questions over the held
status and its consumer-local freshness: is the producer healthy (fresh status;
no `degraded` / `reset_required`; not an empty / pre-enumeration index,
`TargetRevision = 0`), and — when the consumer declares a bound — is the view
within `max_staleness`. Coverage (`Ready`, `Lag`) SHALL NOT be a gate input on
any read path; both remain on the envelope as observability. Status-unknown
SHALL fail closed (`allow_ungated_reads` remains the explicit deployment
escape). The gate SHALL live in one canonical helper beside the envelope type;
per-consumer hand-rolled gate logic is prohibited.

#### Scenario: Ordinary catch-up lag does not defer

- **GIVEN** a healthy index catching up under continuous write (`Lag > 0`, no
  hard stop, enumeration complete)
- **WHEN** a consumer without a staleness bound evaluates the gate
- **THEN** it proceeds, and the envelope reports the current `staleness_ms`

#### Scenario: Hard stops and unknown status always defer

- **GIVEN** `State ∈ {degraded, reset_required}`, or `TargetRevision = 0`, or a
  stale/absent status feed
- **WHEN** the gate is evaluated with any configuration
- **THEN** it defers (fail closed), with the typed defer reason

#### Scenario: A declared staleness bound is the only freshness dial

- **GIVEN** a view-rate consumer with `max_staleness` configured
- **WHEN** the view's `staleness_ms` exceeds the bound
- **THEN** the gate defers with reason `over_staleness`
- **AND** no revision-count tolerance exists anywhere on the gate surface

## MODIFIED Requirements

### Requirement: Ready reports exact revision coverage

The readiness envelope's `Ready` bool SHALL be true only when the index has
applied every committed ENTITY_STATES revision at compute time (`target > 0 &&
indexed >= target`) AND no required index write is unresolved. `Ready` is
observability, not a gate input: no read path SHALL defer on `!Ready` alone,
and no consumer SHALL treat an empty result under `Ready=true` as an
authoritative not-found — coverage says nothing about whether the source ever
published the thing being looked for (ADR-084 retires ADR-066's
authoritative-absence license). `IndexedRevision >= myRev` remains the
caller-supplied read-your-writes check, the only legitimately per-entity one.

#### Scenario: Ready stays exact and observable under continuous write

- **GIVEN** a bucket under continuous write so `Lag > 0` at compute time
- **WHEN** any consumer decodes the envelope from `GRAPH_STATUS`
- **THEN** `Ready` is false while `staleness_ms` reports the view's age
- **AND** healthy read paths still serve, reporting that staleness

#### Scenario: Empty results license nothing

- **GIVEN** a query returning no rows while `Ready=true`
- **WHEN** a consumer interprets the result
- **THEN** no correctness argument may treat the emptiness as proof of absence

### Requirement: Read consumers retry the readiness transient

Reverse-index and by-name read handlers SHALL return the classified transient
`ErrorCodeIndexNotReady` only for health failures — hard stops
(`degraded` / `reset_required`), status-unknown, and bootstrap-incomplete
(the gh#474 cutover window, the transient's documented job) — and SHALL NOT
return it for ordinary catch-up lag on a healthy index. Consumers SHALL detect
it via `errs.IsTransient` (never by message text) and retry rather than
treating it as a permanent failure; bounded retry converges once the health
condition clears. This deliberately supersedes the #592 close-out: retrying
the transient stops being the prescribed response to plain lag, because the
transient no longer fires on plain lag.

#### Scenario: A read during ordinary catch-up serves with staleness

- **GIVEN** a healthy, bootstrapped index catching up after a write burst
- **WHEN** a reverse-index or by-name read arrives
- **THEN** it is served (no transient), with staleness observable on the envelope

#### Scenario: A read during the cutover window is retryable

- **GIVEN** the index is bootstrap-incomplete or in a hard-stop state
- **WHEN** a reverse-index or by-name read arrives
- **THEN** it returns the classified `ErrorCodeIndexNotReady` transient
- **AND** a consumer that retries converges once health is restored

### Requirement: Fusion degrades consistently on the readiness transient

The fusion engine SHALL gate `Fuse` on health (the canonical two-question
gate), not coverage: it SHALL proceed under ordinary catch-up lag, reporting
`staleness_ms` on the envelope, and SHALL return the empty-honest envelope
(fail closed, carrying the current `IndexStatus`) only on health defers —
status-unknown, hard stops, or an empty index. When `Resolve`, `Entities`, or
the **relations** neighbor expansion returns the classified
`ErrorCodeIndexNotReady` (now health-scoped), `Fuse` SHALL degrade to the same
empty-honest envelope rather than propagating a hard error, and a degraded
envelope SHALL NOT carry `State="ready"`. Genuine, non-transient errors SHALL
still propagate. The facet walks (impact / paths / graph projection) remain
out of scope: they carry their own per-facet honesty markers (`Truncated`; the
graph facet carries no coherence claim — see the fusion capability spec).

#### Scenario: Fuse serves ranked evidence under lag

- **GIVEN** a healthy index with `Lag > 0` and a query with matching entities
- **WHEN** `Fuse` runs
- **THEN** it returns ranked results with `staleness_ms` reported, not an
  empty envelope

#### Scenario: A health-scoped transient degrades, not errors

- **GIVEN** a core read inside `Fuse` returns the classified readiness transient
- **WHEN** `Fuse` handles it
- **THEN** it returns the empty-honest envelope, the same degrade as its top gate

#### Scenario: A genuine error still propagates

- **GIVEN** an internal read returns a non-transient error (e.g. a real decode
  or connection failure)
- **WHEN** `Fuse` handles it
- **THEN** it propagates the error (not degraded to an empty envelope)
