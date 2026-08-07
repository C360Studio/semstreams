<!-- markdownlint-disable MD041 -->

## ADDED Requirements

### Requirement: GraphQL is the sole public trajectory application surface

Graph-gateway SHALL expose versioned trajectory reads through GraphQL. Direct agentic-loop trajectory HTTP handlers
and OpenAPI paths SHALL be absent. The gateway SHALL reach agentic-loop only through typed internal NATS
request/reply; public callers SHALL NOT need KV keys, bucket names, Store instances, evidence keys, graph state, or
NATS subjects.

The GraphQL production path SHALL support fact metadata reads and opt-in evidence hydration. When hydration is
requested, the typed internal request SHALL ask agentic-loop's canonical reader to resolve each
`message.StorageReference` through its declared logical Store instance and verify expected digest/size. The gateway
SHALL project the hydrated response and report missing or unverifiable evidence honestly rather than fabricating a
body or failing the whole loop response. Graph-gateway SHALL NOT become a trajectory Store borrower.

#### Scenario: public trajectory reads use GraphQL

- **GIVEN** a public client requests a loop trajectory
- **WHEN** the production gateway serves it
- **THEN** the client receives the versioned GraphQL response
- **AND** no direct agentic-loop trajectory HTTP/OpenAPI route exists
- **AND** the client supplies no KV, NATS, Store, evidence-key, or graph detail

#### Scenario: requested evidence is hydrated through its reference

- **GIVEN** a returned fact with a valid registered-Store evidence reference
- **WHEN** the GraphQL request opts into evidence hydration
- **THEN** agentic-loop's canonical reader returns the verified full body for gateway projection
- **AND** the fact-only path does not fetch the body when hydration is not requested

#### Scenario: missing evidence is reported without invented content

- **GIVEN** a visible fact whose reference is missing or unverifiable
- **WHEN** GraphQL hydration is requested
- **THEN** the response reports that body as missing or unverifiable
- **AND** other visible facts and observed totals remain available

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
