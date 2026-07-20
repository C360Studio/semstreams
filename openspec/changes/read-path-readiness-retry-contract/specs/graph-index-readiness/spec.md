# graph-index-readiness — delta

## ADDED Requirements

### Requirement: Read consumers retry the readiness transient
Reverse-index and by-name read handlers SHALL return the classified transient
`ErrorCodeIndexNotReady` while the index is catching up to ENTITY_STATES, and
consumers SHALL detect it via `errs.IsTransient` (never by message text) and
retry rather than treating it as a permanent failure. Readiness is sticky
(`indexBootstrapped`), so bounded retry converges; a consumer that wants a
self-serve bounded decision instead of retrying MAY gate on the envelope's
`IndexedRevision >= myRev` (ADR-066's finer contract), never on serving an
unmarked stale answer.

#### Scenario: A read arriving during catch-up is retryable
- **GIVEN** the index is catching up right after a write burst
- **WHEN** a reverse-index or by-name read arrives
- **THEN** it returns a classified `ErrorCodeIndexNotReady` transient
- **AND** a consumer that retries converges once readiness flips (sticky)

#### Scenario: The transient is programmatically detectable
- **GIVEN** a read consumer
- **WHEN** it inspects the error
- **THEN** `errs.IsTransient` classifies it without matching any message string

### Requirement: Fusion degrades consistently on the readiness transient
The fusion honesty envelope SHALL treat the readiness transient identically on
every core read path that lacks its own incompleteness marker: when `Resolve`,
`Entities`, or the **relations** neighbor expansion returns the classified
`ErrorCodeIndexNotReady`, `Fuse` SHALL return the empty-honest envelope
(`Ready=false`, carrying the current `IndexStatus`) — the same degrade as its
top-level `!Ready` gate — rather than propagating a hard error, and a `Ready=false`
envelope SHALL NOT carry `State="ready"`. Genuine, non-transient errors SHALL
still propagate. The facet walks (impact / paths / graph projection) are OUT of
scope: they carry their own per-facet honesty markers (`Truncated`, and the graph
facet's `ViewRevision.Coherent`), so a readiness transient there yields an honest
lower-bound and is handled identically to any other walk fault.

#### Scenario: A Resolve-path transient degrades, not errors
- **GIVEN** `Fuse`'s top `Ready` gate passed but `Resolve` hits the readiness
  transient in the narrow first-catch-up race under load
- **WHEN** `Fuse` handles it
- **THEN** it returns the empty-honest envelope (`Ready=false`), not a hard error
- **AND** the caller falls back exactly as it does on the top-gate `!Ready` path

#### Scenario: A genuine error still propagates
- **GIVEN** an internal read returns a non-transient error (e.g. a real decode or
  connection failure)
- **WHEN** `Fuse` handles it
- **THEN** it propagates the error (not degraded to an empty envelope)
