# predicate-contract — Delta

## MODIFIED Requirements

### Requirement: The beta cutover updates owned producers and resets incompatible state

The breaking release MUST update every SemStreams producer, owned reference design, generated
schema/tool surface, exact query, and participating owned sister repository to the canonical
contract. The release MUST publish an exact source/configuration rename ledger, but that ledger
MUST NOT be loaded as a runtime alias or transformation table.

Existing ENTITY_STATES containing a noncanonical predicate MUST surface as typed
`graph_state_reset_required` poison per the graph-state-contract reader classes: projection and
replay consumers MUST block whole-view readiness until clean reingest and index replay reach
the authoritative watermark, while the authoritative graph-ingest surface refuses exactly the
poisoned entities and keeps serving valid state. SemStreams MUST NOT rewrite malformed beta
state in place; repair is the operator delete/reset path.

#### Scenario: incompatible beta state requires a clean reset

- **GIVEN** an existing ENTITY_STATES bucket containing a noncanonical predicate
- **WHEN** the breaking SemStreams binary starts
- **THEN** projection and replay consumers refuse readiness with reset/reingest instructions
- **AND** the authoritative surface refuses reads of the affected entities with
  `graph_state_reset_required` while serving unaffected entities
- **AND** no compatibility reader or in-place transformer accepts the old state

#### Scenario: clean reingest exposes only canonical identities

- **GIVEN** incompatible graph/index buckets have been cleared
- **WHEN** owned canonical sources are reingested and index replay completes
- **THEN** exact and namespace queries expose only canonical predicate identities
