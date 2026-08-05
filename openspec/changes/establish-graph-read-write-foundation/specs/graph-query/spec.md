# graph-query — delta for establish-graph-read-write-foundation

## ADDED Requirements

### Requirement: Exact entity reads carry same-entry authority revision

The admitted exact entity operation MUST return the validated canonical entity and nonzero KV revision from one
`ENTITY_STATES` entry. Remote applications consume it through GraphQL as `{entity, kvRevision}`. Embedded framework
consumers receive one operation-specific typed adapter. Raw KV, MCP, literal-colon HTTP routes, provider JSON, and the
aggregate `graph/query.Client` MUST NOT become alternate application contracts.

#### Scenario: GraphQL and embedded exact reads agree

- **GIVEN** entity A is resident at KV revision R
- **WHEN** GraphQL and the embedded adapter exact-read A without an intervening write
- **THEN** both return the same canonical entity and R
- **AND** neither substitutes logical `EntityState.Version`

### Requirement: Dereference reports unresolved object IDs without hiding source edges

Exact dereference, batch hydration, and traversal MUST preserve every valid source relationship and report unresolved
object IDs through their existing missing/unknown shapes. Missing objects MUST NOT be silently omitted, fabricated as
stubs, treated as source poison, or interpreted as permission to delete the source edge.

#### Scenario: Later birth resolves without replay

- **GIVEN** source A references absent B and dereference reports B missing
- **WHEN** a real producer later creates B
- **THEN** the next dereference resolves B
- **AND** A is not replayed or rewritten
