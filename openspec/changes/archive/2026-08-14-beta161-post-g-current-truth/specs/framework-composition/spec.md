## MODIFIED Requirements

### Requirement: Graph research is an atomic framework capability

SemStreams MUST retain the `research_graph` agent-facing capability, its research payloads, classifier/query/fusion
primitives, five bounded components, R0-R6 coordinated rule pack, AGENT_LOOPS/ObjectStore evidence contract,
provenance-bearing result, and `read_loop_result` retrieval path. The tool MUST be advertised only when the complete
configured execution path is available. A partial graph-research configuration MUST fail boot with an actionable
error rather than register a tool that can stall.

Repository-owned deterministic proof SHALL retain both admitted branch shapes: `synthesize_directly`, which bypasses
execute and assess, and a deterministic `walk_seeds` route that traverses `execute_subqueries`, `fusion.Fuse`,
assessment, and terminal synthesis. The two branches SHALL be asserted independently so success on one cannot mask
absence or misrouting of the other.

#### Scenario: graph research is absent by choice

- **GIVEN** a valid deployment that does not configure graph research
- **WHEN** tool registration completes
- **THEN** direct graph query tools remain available
- **AND** `research_graph` is not advertised

#### Scenario: graph research is partially configured

- **GIVEN** a deployment configures `research_graph` or one research stage without all required stages, rules, stores,
  and result retrieval
- **WHEN** bootstrap validates the selected capabilities
- **THEN** bootstrap fails before serving agent tool catalogs
- **AND** the error identifies the missing graph-research dependency

#### Scenario: graph research is complete

- **GIVEN** all graph-research components, R0-R6 rules, stores, graph dependencies, and result tools are configured
- **WHEN** bootstrap completes
- **THEN** `research_graph` is advertised
- **AND** an invocation can progress to a provenance-bearing result retrievable by the parent

#### Scenario: The direct route remains independently proven

- **GIVEN** the deterministic direct-route fixture
- **WHEN** a research invocation completes
- **THEN** the route action is `synthesize_directly`
- **AND** classifier evidence and terminal result are present
- **AND** execute and assess completion markers are absent
- **AND** the result remains retrievable by the parent

#### Scenario: The walk-seeds execute and fusion route is independently proven

- **GIVEN** the deterministic `walk_seeds` fixture with controlled graph evidence
- **WHEN** a research invocation completes
- **THEN** `execute_subqueries` invokes the production fusion path
- **AND** execute completion carries a positive evidence count and controlled evidence identity
- **AND** assessment completes and routes to terminal synthesis
- **AND** synthesis references only evidence returned by execution
- **AND** the result remains retrievable by the parent
