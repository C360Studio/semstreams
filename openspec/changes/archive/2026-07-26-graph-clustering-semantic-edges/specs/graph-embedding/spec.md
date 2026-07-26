## ADDED Requirements

### Requirement: The similarity query classifies not-ready distinctly from a genuine empty result

The `graph.embedding.query.similar` handler MUST return the classified transient `ErrorCodeIndexNotReady`
when the embedding index's bootstrap/health gate has not cleared (the initial ENTITY_STATES bootstrap is
still validating, or its watcher is unavailable), and MUST NOT return that transient — or any error — for an
entity that is simply found to have no close neighbors above the caller's threshold. A caller MUST be able to
distinguish "could not ask" (the transient) from "asked, got nothing" (an ordinary empty result) without
matching on error message text.

This is seeded now, verified against existing code, because a second consumer — community detection's
semantic-edge synthesis (`graph-clustering`), not only anomaly detection — depends on the distinction to
implement its embedding-readiness structural-floor fallback correctly.

#### Scenario: A query during embedding bootstrap returns the classified transient

- **GIVEN** the embedding index's initial bootstrap has not yet completed
- **WHEN** `graph.embedding.query.similar` is queried
- **THEN** the handler returns the classified `ErrorCodeIndexNotReady` transient

#### Scenario: A query against a ready index returns an ordinary empty result when there are no close neighbors

- **GIVEN** the embedding index is ready and a queried entity has no neighbor above the caller's similarity
  threshold
- **WHEN** `graph.embedding.query.similar` is queried for that entity
- **THEN** the handler returns a successful response with an empty similarity list, not an error

#### Scenario: The transient is programmatically detectable

- **GIVEN** a caller of `graph.embedding.query.similar`
- **WHEN** it receives the not-ready transient
- **THEN** it can classify it via the shared transient-error check (`errs.IsTransient` plus the
  `ErrorCodeIndexNotReady` code) without matching on error message text
