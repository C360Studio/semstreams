<!-- markdownlint-disable MD041 -->

## ADDED Requirements

### Requirement: GraphQL is the sole public trajectory application surface

Graph-gateway SHALL expose versioned trajectory reads through GraphQL. Direct agentic-loop trajectory HTTP handlers
and OpenAPI paths SHALL be absent. The gateway SHALL reach agentic-loop only through typed internal NATS
request/reply; public callers SHALL NOT need KV keys, bucket names, Store instances, evidence keys, graph state, or
NATS subjects.

The GraphQL production path SHALL expose strict cursor-paged fact metadata and `message.StorageReference` values. It
SHALL preserve agentic-loop's `next_cursor`, page-local `observed_totals`, and `terminal_observed` truth. Neither the
typed internal request nor GraphQL SHALL accept an evidence-hydration option or carry an evidence body. Graph-gateway
SHALL NOT become a trajectory Store borrower. Authorized evidence retrieval remains a separate registered-Store
operation outside this public query contract.

#### Scenario: public trajectory reads use GraphQL

- **GIVEN** a public client requests a loop trajectory
- **WHEN** the production gateway serves it
- **THEN** the client receives the versioned GraphQL response
- **AND** no direct agentic-loop trajectory HTTP/OpenAPI route exists
- **AND** the client supplies no KV, NATS, Store, evidence-key, or graph detail

#### Scenario: evidence references cross the gateway without hydration

- **GIVEN** a returned fact with a valid registered-Store evidence reference
- **WHEN** GraphQL projects the trajectory page
- **THEN** it exposes the reference metadata and no evidence body
- **AND** neither graph-gateway nor agentic-loop borrows a Store for the query

#### Scenario: missing evidence does not invent content

- **GIVEN** a visible fact whose reference is missing or unverifiable
- **WHEN** GraphQL projects the trajectory page
- **THEN** the recorded capture/reference status remains visible without an invented body
- **AND** the page's other facts and observed totals remain available

### Requirement: Trajectory projection is always observed-only

Every GraphQL trajectory response SHALL expose `coverage: observed` and `observed_totals` derived only from returned
visible facts. It SHALL NOT expose a complete/partial/unknown coverage classification, seal, manifest, membership
proof, attempted/recorded/gap count, or any equivalent completeness promise.

When no `loop.terminal` fact is returned, the response SHALL set `terminal_observed: false`. When one or more are
returned, it SHALL set `terminal_observed: true` and preserve every terminal fact in causal/attempt order. Terminal
presence SHALL NOT change coverage or imply that earlier facts/evidence are complete.

#### Scenario: a visible terminal observation does not upgrade coverage

- **GIVEN** a trajectory response containing one or more `loop.terminal` facts
- **WHEN** GraphQL projects the response
- **THEN** `terminal_observed` is true and all terminal observations remain ordered
- **AND** `coverage` remains `observed`
- **AND** totals remain `observed_totals`

#### Scenario: terminal absence is not inferred from adjacent surfaces

- **GIVEN** returned facts contain no `loop.terminal` observation
- **WHEN** GraphQL projects the response
- **THEN** `terminal_observed` is false
- **AND** no outcome is inferred from `COMPLETE_`, terminal events, cache, process memory, or graph state

#### Scenario: projection schema has no completeness machinery

- **WHEN** the GraphQL schema and projection response types are inspected
- **THEN** they contain no terminal seal, completeness status, audit counters, manifest, membership proof, or
  reconstructed gap claim

### Requirement: Graph prefix continuation survives every projection

Graph prefix queries SHALL preserve the typed internal `PrefixQueryResponse` and its `next_cursor` through
graph-gateway. The breaking GraphQL field SHALL accept `prefix`, `limit`, and `cursor`, and return
`EntityPage { entities, next_cursor }`. The former list-only `[Entity]` response SHALL be absent; no alias or silent
cursor discard is permitted.

#### Scenario: a prefix page continues without loss

- **GIVEN** a graph prefix response containing entities and a non-empty `next_cursor`
- **WHEN** graph-gateway projects it through GraphQL
- **THEN** the client receives the same page entities and continuation token
- **AND** a subsequent request can supply that token through the typed query path

#### Scenario: list-only prefix projection is removed

- **WHEN** the GraphQL schema and gateway adapters are inspected
- **THEN** prefix queries return `EntityPage` rather than `[Entity]`
- **AND** no compatibility alias discards `next_cursor`

### Requirement: The existing typed agentic request family owns trajectory routing

Graph-gateway SHALL retain exactly its three required query-family outputs. Its existing `agentic_queries` output SHALL
be a required `nats-request` port with subject family `agentic.query.*` and interface `agentic.query` v1.
Agentic-loop's `trajectory_query` input SHALL be a required `nats-request` port with exact subject
`agentic.query.trajectory` and the same interface/version.

The gateway SHALL derive the exact trajectory request through its existing `querySubject(agentic_family,
"trajectory")` behavior. Agentic-loop SHALL subscribe through its declared input rather than a hard-coded literal
bypass. No fourth gateway output, platform-derived owner, alias, dual subscription, or compatibility shim SHALL be
added.

An isolated deployment MAY supply complete matching overrides on both components, such as
`tenant.agentic.query.*` and `tenant.agentic.query.trajectory`. Each override SHALL repeat kind, required state, and
interface type/version. Mismatched pairs SHALL fail validation before subscription or request handling.

#### Scenario: canonical agentic family resolves the exact trajectory route

- **GIVEN** graph-gateway's canonical `agentic.query.*` output and agentic-loop's canonical exact input
- **WHEN** the gateway resolves the trajectory query subject
- **THEN** it requests `agentic.query.trajectory`
- **AND** agentic-loop receives it through the declared `trajectory_query` input
- **AND** graph-gateway still has exactly three outputs

#### Scenario: interface or paired override mismatch fails validation

- **GIVEN** the gateway family and loop exact input disagree on subject family, kind, required state, interface type,
  or interface version
- **WHEN** port/config validation runs
- **THEN** validation fails before routing is installed
- **AND** no alias, subject inference, literal bypass, or fourth output repairs the mismatch

#### Scenario: explicit paired isolation remains possible

- **GIVEN** complete matching gateway and agentic-loop overrides under an isolated subject prefix
- **WHEN** both declarations validate
- **THEN** `querySubject` resolves the isolated family to the isolated exact input
- **AND** no platform identity is used to derive either subject
