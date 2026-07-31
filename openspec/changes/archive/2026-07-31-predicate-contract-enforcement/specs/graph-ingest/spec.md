## ADDED Requirements

### Requirement: Every ENTITY_STATES commit validates the complete final candidate

graph-ingest MUST apply the canonical predicate contract at one authoritative persistence seam used by every
ENTITY_STATES create, update, merge, batch, CAS, Graphable, foreign-edge, inference, rule, direct-adapter,
and repair lane. Validation MUST inspect the complete candidate after normalization, merging, routing, and
framework triple injection, and before any state or required projection side effect commits.

Handler-level validation MAY return earlier classified errors but MUST NOT be the correctness boundary.

#### Scenario: a malformed foreign triple cannot bypass Graphable validation

- **GIVEN** one Graphable arrival containing valid own triples and an invalid foreign-subject predicate
- **WHEN** normalization and foreign routing construct their final candidates
- **THEN** the invalid foreign candidate reaches the same authoritative structural gate
- **AND** graph-ingest commits neither malformed state nor a partial derived projection

#### Scenario: a direct mutation adapter cannot bypass the gate

- **WHEN** an internal adapter calls a public create, merge, or add-triples path with an invalid predicate
- **THEN** the final persistence seam applies the same typed rejection as the external mutation lane

### Requirement: Replacement validates before destructive mutation

An operation that replaces a predicate value MUST validate its intended complete final candidate before
removing the existing fact. If validation or persistence of the replacement fails, the prior authoritative
value MUST remain unchanged.

#### Scenario: a rejected replacement does not lose the old value

- **GIVEN** an entity carrying valid state and an update request that would produce an invalid final candidate
- **WHEN** replacement validation fails
- **THEN** the original triple remains in ENTITY_STATES
- **AND** no remove-then-fail partial update is visible
