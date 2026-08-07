# entity-id-contract — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: Canonical entity-ID enforcement is unconditional at graph boundaries

Canonical literal entity IDs, declaration patterns, and query prefixes MUST remain separately validated before graph
I/O. Validation applies to Graphable ingest, exact reads, all four mutation operations, local projection contracts,
rules, lifecycle, tools, and hierarchy. No ownership registry or owner configuration participates in identity validity.

#### Scenario: Invalid ID fails before CAS or hierarchy work

- **GIVEN** a mutation carries a malformed literal entity ID
- **WHEN** graph-ingest validates the request
- **THEN** it returns typed invalid before KV read, write, metric, or hierarchy side effect

### Requirement: The pre-v1 beta cutover is a clean owned-source break

Every in-repo source, configuration, generated schema, and fixture MUST move to the canonical mutation port and exact
entity result in the coordinated cutover. There is no ownership-configuration compatibility path and no downstream
source edit in this change.

#### Scenario: Generated configuration contains no ownership field

- **GIVEN** post-cutover schema generation
- **WHEN** graph-ingest and rule schemas are inspected
- **THEN** they contain no owner lease, token, claim, presence, or foreign-edge mode field
