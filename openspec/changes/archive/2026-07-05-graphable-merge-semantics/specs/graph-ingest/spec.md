# Graph Ingest

## ADDED Requirements

### Requirement: A re-arriving entity's triples merge by predicate-level replacement

graph-ingest MUST merge the incoming triples of a re-arriving (already-existing)
entity by replacing per `(subject, predicate)`, not by appending, when the write
comes through the Graphable (JetStream) ingest lane. A predicate carried by the
incoming entity MUST replace that `(subject, predicate)`'s prior triples, so the
entity does not accumulate duplicate triples across repeated arrivals. This matches
the mutation (`AddTriples`) lane's merge semantics.

#### Scenario: republishing the same entity does not accumulate duplicates

- **GIVEN** an entity previously ingested with `flock.position.x = 1`
- **WHEN** the same entity is ingested again with `flock.position.x = 2`
- **THEN** the stored entity has exactly one `flock.position.x` triple
- **AND** its value is `2`

### Requirement: A predicate absent from an arrival is preserved

Merging MUST preserve any existing triple whose `(subject, predicate)` is not
present in the incoming arrival, so a Graphable arrival does not clobber
predicates written by a different writer (e.g. lifecycle-managed triples).

#### Scenario: a non-conflicting predicate survives a later arrival

- **GIVEN** an entity carrying `lifecycle.phase = active` and `sensor.temp = 20`
- **WHEN** a Graphable arrival for that entity carries only `sensor.temp = 21`
- **THEN** the stored entity still has `lifecycle.phase = active`
- **AND** `sensor.temp` is `21`

### Requirement: The create-time indexing profile is not overridden by a re-arrival

MUST preserve the create-time indexing profile across a merge: it is immutable
after create (ADR-054), so even though the merge is otherwise newer-wins, a
re-arrival that declares a different indexing profile MUST NOT change the stored
profile. A profile-less referential-integrity stub is the sole exception — its
first real arrival's declared profile stands as the entity's true birth.

#### Scenario: a re-arrival cannot change the create-time profile

- **GIVEN** an entity created with indexing profile `content`
- **WHEN** a later Graphable arrival for that entity declares profile `trace`
- **THEN** the stored indexing profile is still `content`
- **AND** the entity has exactly one indexing-profile triple

#### Scenario: a profile-less stub's first real arrival sets the profile

- **GIVEN** a profile-less referential-integrity stub for an entity
- **WHEN** the first real Graphable arrival declares indexing profile `content`
- **THEN** the stored indexing profile is `content`

### Requirement: A multi-valued predicate is full-set replaced

On merge, a multi-valued relationship predicate MUST be full-set replaced.
For a predicate a subject may hold several times (such as `flock.neighbor`), an
arrival that carries that predicate replaces the entire prior set for that
`(subject, predicate)` with the arrival's set. Producers therefore own publishing
the complete object set for such a predicate on each arrival; this lane MUST NOT
append individual relationship objects.

#### Scenario: a new neighbor set replaces the prior set

- **GIVEN** an entity whose stored `flock.neighbor` set is `{b, c}`
- **WHEN** a Graphable arrival carries `flock.neighbor` = `{c, d}`
- **THEN** the stored `flock.neighbor` set is exactly `{c, d}`
- **AND** the prior-only member `b` is no longer present
