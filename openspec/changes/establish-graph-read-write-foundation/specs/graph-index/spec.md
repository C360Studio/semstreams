# graph-index — delta for establish-graph-read-write-foundation

## MODIFIED Requirements

### Requirement: Every surviving derived index declares semantic ownership and reconciliation capability

Every surviving derived index MUST declare its source authority, row provenance, retraction responsibility, rebuild
behavior, and readiness contract. In this requirement, responsibility means which component maintains derived rows; it
MUST NOT imply predicate claims, mutation permission, leases, tokens, or `pkg/ownership` registration.

#### Scenario: Index row ownership is provenance, not mutation authority

- **GIVEN** graph-index maintains a row derived from entity A
- **WHEN** A changes or disappears
- **THEN** graph-index retracts or replaces its row according to its convergence contract
- **AND** that responsibility grants no right to mutate A's predicates
