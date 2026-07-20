# graph-query — delta

## ADDED Requirements

### Requirement: Batch entity reads report unreturned IDs

The batch entity read (`graph.query.batch`) SHALL report every requested ID it
does not return, with a reason distinguishing authoritative-state not-found
from a fetch fault, while preserving partial success for the IDs it can serve.
Silent omission is prohibited: a caller SHALL always be able to partition its
request into returned, not-found, and faulted — and not-found remains a
statement about a single KV read at one instant, never an authoritative
absence claim.

#### Scenario: A not-found ID is reported, not omitted

- **GIVEN** a batch request where one ID's ENTITY_STATES read returns not-found
- **WHEN** the handler responds
- **THEN** the response carries the other entities AND lists the missing ID
  with reason not-found

#### Scenario: A faulted read does not masquerade as not-found

- **GIVEN** a batch request where one ID's read fails with a non-not-found error
- **WHEN** the handler responds
- **THEN** that ID is reported with a fault reason (or the call errors,
  per the existing first-error contract), never dropped or conflated with
  not-found
