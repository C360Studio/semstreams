# graph-query Specification

## ADDED Requirements

### Requirement: Compact top-level entity digests project canonical display text

Every top-level `EntityDigest` returned by `globalSearch` or `searchGraph` SHALL preserve the ranked result ID, count,
position, and relevance semantics already produced by its search branch. Label hydration SHALL NOT recompute
relevance. Its type SHALL be derived from the canonical entity ID.

For bounded compact-result branches, graph-query SHALL batch-read the final IDs through the admitted graph-ingest
batch surface. For each returned entity it SHALL inspect these predicates in order: `dc.terms.title`,
`agent.identity.display-name`, `agent.capability.name`, then `agent.model.name`. For each predicate, it SHALL inspect
the first matching stored triple and use its object only when it is a non-empty string; otherwise it SHALL advance to
the next predicate. These four steps are recognized-label resolution.

If no recognized label resolves, graph-query SHALL retain the legacy compatibility heuristic: the first triple in
stored order whose object is a non-empty string and is not a valid entity ID. This is heuristic display text, not a
recognized label predicate. If the entity is missing, hydration fails ordinarily, or neither recognized-label
resolution nor the heuristic yields text, the digest SHALL retain its row and use the entity-ID instance. That
fallback does not assert that canonical state supplied human-readable text. An authoritative graph-state contract
failure SHALL stop the response.

Batch response order SHALL NOT determine digest order. This requirement adds no property projection, label index,
caller byte budget, or payload-size prediction. Actual carrier refusal remains governed by the shared response-bounds
contract.

#### Scenario: Auto-summary labels a non-representative entity

- **GIVEN** an auto-summarized semantic result contains a non-representative entity with `dc.terms.title`
- **WHEN** graph-query builds the top-level compact digests
- **THEN** that entity's digest label is its title
- **AND** its original rank and branch-owned relevance are unchanged

#### Scenario: Direct fallback completes type and label

- **GIVEN** searchGraph reaches direct semantic fallback
- **AND** a returned entity has a recognized title
- **WHEN** the compact response is built
- **THEN** its digest contains the title and the type parsed from its ID
- **AND** fallback strategy, degradation reason, row order, and per-row relevance remain unchanged

#### Scenario: Batch response order differs from result order

- **GIVEN** graph-ingest returns hydrated states in an order different from the semantic hits
- **WHEN** graph-query composes the digests
- **THEN** the digests remain in semantic-hit order
- **AND** each label is joined by entity ID

#### Scenario: Missing or unresolved entity retains its row

- **GIVEN** a successful batch omits an ID or returns an entity yielding neither recognized nor heuristic display text
- **WHEN** the digest list is composed
- **THEN** the row remains present with its ID-instance label
- **AND** resolved siblings retain their display text

#### Scenario: Ordinary label hydration is unavailable

- **GIVEN** the label batch receives an ordinary transport, decoding, or observed response-size failure
- **WHEN** compact search can otherwise answer
- **THEN** graph-query returns the ranked digests with ID-instance labels
- **AND** it requires no caller payload-size prediction or retry knob

#### Scenario: Authoritative state is poisoned

- **GIVEN** the batch contains authoritative graph state that violates the graph-state contract
- **WHEN** graph-query validates the response
- **THEN** it propagates the classified graph-state failure
- **AND** no partially validated digest success escapes

#### Scenario: Duplicate semantic IDs preserve per-hit relevance

- **GIVEN** direct semantic fallback returns two rows for the same entity ID with different relevance scores
- **WHEN** graph-query enriches their type and label
- **THEN** both rows remain in their original positions
- **AND** each row retains its original relevance
- **AND** both rows receive the resolved type and label through one batch hydration request
