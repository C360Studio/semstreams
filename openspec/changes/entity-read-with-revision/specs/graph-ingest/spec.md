# graph-ingest — delta (entity-read-with-revision)

## ADDED Requirements

### Requirement: The authoritative entity query MUST offer a revision-bearing response

The authoritative entity query lane MUST, when the request opts in, return the entity together
with the KV revision of the exact ENTITY_STATES entry whose bytes produced the returned
entity, taken from the same read. Without the opt-in the response MUST remain the existing
bare entity shape, byte-compatible with current consumers. Not-found, stub, and poison
behavior MUST be unchanged by the opt-in; the revision rides the success path only.

#### Scenario: Opt-in returns entity plus revision

- **WHEN** a caller queries an existing entity with the revision opt-in set
- **THEN** the response carries the entity and a non-zero revision identifying the exact
  entry read, and that revision is accepted unchanged as an expected revision by the
  conditional update lane

#### Scenario: Bare shape preserved without opt-in

- **WHEN** a caller queries without the opt-in
- **THEN** the response is the existing bare entity representation, unchanged

#### Scenario: Read-modify-write with one winner

- **WHEN** two writers read the same entity revision over the production wire and both submit
  conditional updates expecting it
- **THEN** exactly one commits and the other receives the stable revision-mismatch error, and
  a refetch returns the new revision from which a retry succeeds

#### Scenario: Error contracts unchanged

- **WHEN** a revision-requesting query targets a missing or poisoned entity
- **THEN** the response is identical to the same query without the opt-in
