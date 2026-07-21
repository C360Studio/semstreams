# graph-query — delta

## ADDED Requirements

### Requirement: Batch entity reads report unreturned IDs

The batch entity read (`graph.query.batch`) SHALL report every requested ID it
does not return, with a reason from the closed set `not_found` / `error`,
while preserving partial success for the IDs it can serve. Silent omission is
prohibited: a caller SHALL always be able to partition its request into
returned, not-found, and faulted — and not-found remains a statement about a
single KV read at one instant, never an authoritative absence claim. Under
the preserved first-error contract (a non-not-found fault fails the call),
`error` entries are reserved wire vocabulary; every consumer of the batch
response (the fusion client, the research-graph adapter, the graph-query
passthrough's validator) SHALL tolerate and surface the report rather than
drop it.

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

#### Scenario: Every requested ID missing is still a report, not an error

- **GIVEN** a batch request where every ID's read returns not-found
- **WHEN** the handler responds
- **THEN** the response carries an empty entity list and a missing entry per
  requested ID — a complete miss is partial success at n=0, not a failure
