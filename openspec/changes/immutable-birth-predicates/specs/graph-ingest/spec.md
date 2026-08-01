# graph-ingest — delta (immutable-birth-predicates)

## ADDED Requirements

### Requirement: The first committed write of an immutable predicate MUST freeze it on that entity

For a predicate carrying the vocabulary immutable classification, the first committed write on
a given entity — through any accepted lane, including Graphable first arrival — MUST fix that
predicate's canonical value set on that entity. The frozen basis MUST be the canonical object
value set with datatype, order-independent, excluding envelope volatiles (timestamp,
confidence, source, context), and MUST be pinned by test. Enforcement MUST be
caller-independent: no writer, including the seeder, is exempt after the seed.

#### Scenario: Any lane seeds

- **WHEN** an entity first acquires an immutable predicate via entity creation, a triple add,
  an update, or a Graphable arrival
- **THEN** the committed value set is frozen identically regardless of which lane wrote it

#### Scenario: The seeder is bound too

- **WHEN** the same writer that seeded an immutable predicate later submits a different value
  for it
- **THEN** the attempt is refused exactly as any other writer's would be

### Requirement: Request/reply mutations touching a frozen predicate MUST reject atomically or no-op on exact replay

Every request/reply mutation lane MUST refuse, with a stable classified immutable-predicate
error naming the entity, the predicate, and the lane, any request that would replace, remove,
or conflict-append a frozen predicate; the whole request MUST be rejected with no partial
application. A request carrying the identical canonical value MUST be accepted with no change
to the frozen predicate, so exact replay of a seed is idempotent. The refusal MUST be
observable through the existing mutation-rejection metering.

#### Scenario: Replacement refused on every lane

- **WHEN** any request/reply mutation lane receives a request replacing a frozen predicate's
  value
- **THEN** the request is rejected with the stable immutable-predicate error and no part of
  the request is applied

#### Scenario: Removal refused

- **WHEN** a triple removal or an update's removal set names a frozen predicate
- **THEN** the request is rejected with the stable immutable-predicate error

#### Scenario: Conflicting append refused

- **WHEN** an add or batch-add would introduce a value not in a frozen predicate's set
- **THEN** the request is rejected with the stable immutable-predicate error

#### Scenario: Exact replay converges

- **WHEN** a request carries the frozen predicate with its identical canonical value set
- **THEN** the request succeeds, the frozen predicate is unchanged, and no revision churn is
  attributable to that predicate

### Requirement: The Graphable merge MUST preserve frozen predicates and record every drop

The Graphable ingest merge MUST NOT overwrite or remove a frozen predicate: incoming triples
for frozen predicates whose values differ from the resident set MUST be dropped before merge
while the arrival's remaining triples merge normally, and every drop MUST increment a metric
and log the entity, predicate, and lane. An arrival carrying the identical canonical value
MUST merge cleanly with no drop recorded.

#### Scenario: Divergent arrival preserved-and-continued

- **WHEN** a Graphable arrival carries a frozen predicate with a different value alongside
  unrelated triples
- **THEN** the stored frozen value is preserved, the unrelated triples merge, and the drop is
  metered and logged with entity, predicate, and lane

#### Scenario: Exact re-arrival is clean

- **WHEN** a Graphable arrival carries the frozen predicate with its identical canonical value
- **THEN** the merge proceeds with no drop recorded

### Requirement: Deleting a carrier of frozen predicates MUST be refused

Entity deletion MUST be refused with the stable immutable-predicate error, naming the frozen
predicates present, while the entity carries any frozen predicate. Privileged teardown is
explicitly not offered by this lane and belongs to the retention/deletion system.

#### Scenario: Carrier deletion refused

- **WHEN** an entity-delete request targets an entity holding a frozen predicate
- **THEN** the request is rejected with the stable immutable-predicate error naming the
  frozen predicates, and the entity remains
