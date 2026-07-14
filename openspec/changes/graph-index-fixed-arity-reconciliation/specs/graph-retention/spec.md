## ADDED Requirements

### Requirement: Index cleanup authority is chosen per store from measured owner discovery

Graph retention MUST classify each derived store by semantic row owner and owner-discovery mechanism.
PREDICATE, NAME, source-owned INCOMING, and CONTEXT MUST use fixed-position filtered enumeration when they
pass the versioned 5k CI and 21k full profiles, numeric latency/resource budgets, manifest comparison, and
zero-error rules in the graph-index contract. A reverse manifest or payload-rich tombstone MUST NOT be
required for those stores solely because the entity ID is not a key prefix.

Stores whose owner exists only in values, whose key shape cannot be safely filtered, or whose measured filter
cost exceeds the approved budget MUST declare another cleanup authority. ALIAS, spatial/geohash, embedding
deduplication, shared objects, and cascade/refuse policy remain explicit separate classifications.

#### Scenario: a bare entity retirement can drive a proven owner filter

- **GIVEN** a store whose fixed-position owner filter passed the real-NATS decision gate
- **WHEN** retention receives the entity identity and appropriate semantic delete authority
- **THEN** the store owner can enumerate and reconcile its rows without requiring last-known triples in the
  tombstone

#### Scenario: a non-filterable store does not pretend to self-clean

- **GIVEN** an index whose entity owner appears only in the stored value
- **WHEN** retention classifies its cleanup mechanism
- **THEN** it declares a reverse/value-scan/payload mechanism with explicit budgets
- **AND** it is not marked bare-key self-cleaning

### Requirement: Physical prefix does not override semantic evidence ownership

Retention MUST distinguish physical key position from semantic delete authority. Source-owned INCOMING
assertions are retracted when the source fact changes or an authorized cascade mutates it; target retirement
MUST NOT erase those assertions by applying a target-prefix hard delete as if it were semantic cleanup.

#### Scenario: source death retracts reciprocal rows

- **GIVEN** a source entity owns relationships to several targets
- **WHEN** the source is retired under policy that retracts its facts
- **THEN** INCOMING rows for those assertions are found through the source-axis owner filter and removed
