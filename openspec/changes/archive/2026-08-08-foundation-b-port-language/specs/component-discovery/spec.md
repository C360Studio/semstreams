## Purpose

Defines the single normalized port-fact projection used by component discovery, composition, schema, and provisioning.

## ADDED Requirements

### Requirement: Shared component views consume one normalized port projection

One closed port binding SHALL own each kind's allowed directions, decoder, validation, normalization, resource
identity, exclusivity, interface metadata, interaction pattern, connection metadata, NATS metadata, and stream facts.

The resolver SHALL produce immutable normalized port facts. Registry discovery, flow-graph discovery, ComponentManager
discovery, generated schema, composition, and provisioning SHALL consume that shared projection.

Consumers SHALL NOT inspect concrete configuration types or independently reclassify a resolved port.

#### Scenario: All discovery views agree

- **GIVEN** one successfully resolved port
- **WHEN** the registry, flow graph, and ComponentManager expose that port
- **THEN** every view reports identical direction, kind, interaction pattern, resource identity, interface metadata,
  and connections

#### Scenario: Invalid declaration does not partially surface

- **GIVEN** a port declaration that cannot be decoded, validated, or normalized
- **WHEN** component discovery is constructed
- **THEN** resolution fails before any consumer receives a discovery record for that port
- **AND** no partial or consumer-specific interpretation is exposed

#### Scenario: KV read and watch remain distinct

- **GIVEN** canonical `kv-read` and `kv-watch` declarations
- **WHEN** normalized facts are projected
- **THEN** `kv-read` has interaction pattern `PatternRead`
- **AND** `kv-watch` has interaction pattern `PatternWatch`
- **AND** no consumer collapses the two patterns

### Requirement: Generated port schema is derived from the closed binding

Generated port schema SHALL describe the common envelope, closed kind variants, allowed directions, required
kind-specific fields, and strict additional-property behavior from the same closed binding used by runtime resolution.

Retired fields, aliases, runtime `type`/`data` envelopes, and top-level KV lanes SHALL be absent from the generated
schema.

#### Scenario: Schema and runtime accept the same declaration

- **GIVEN** a canonical port declaration
- **WHEN** it is checked against generated schema and decoded at runtime
- **THEN** both surfaces accept or reject it for the same kind, direction, required fields, and unknown fields

#### Scenario: Direction-specific JetStream requirements remain aligned

- **GIVEN** generated schema and runtime resolution derived from the closed JetStream binding
- **WHEN** a subject-only JetStream declaration is checked as an input and as an output
- **THEN** both schema and runtime reject the input because `stream_name` is required for that direction
- **AND** both accept the output without `stream_name`
- **AND** both require at least one non-empty subject in either direction

#### Scenario: Unbound kind is rejected

- **GIVEN** a kind that is absent from the closed port binding
- **WHEN** it is presented to generated schema or runtime decoding
- **THEN** both surfaces reject it
- **AND** the kind cannot become accepted through consumer-local logic

### Requirement: Graph-gateway shared-mux composition is explicit and breaking

The shared-mux graph-gateway SHALL declare no composition input.

`bind_address` SHALL apply only to standalone development or testing and SHALL NOT create a `NetworkPort` or
shared-mux composition input.

The graph-gateway SHALL declare exactly these three required `nats-request` outputs:

- `graph_queries` for subject family `graph.query.*`
- `graph_index_queries` for subject family `graph.index.query.*`
- `agentic_queries` for subject family `agentic.query.*`

Legacy `queries`, missing outputs, duplicate outputs, extra outputs, optional outputs, wrong-kind outputs, and malformed
subject families SHALL fail startup.

No output SHALL be autofilled, aliased, inferred, or supplied through a compatibility shim. A valid configured family
SHALL remain the runtime routing authority.

#### Scenario: Exact canonical gateway composition is accepted

- **GIVEN** a shared-mux graph-gateway with no input and exactly the three required canonical `nats-request` outputs
- **WHEN** its declaration is resolved
- **THEN** startup succeeds
- **AND** the resolved `graph_queries`, `graph_index_queries`, and `agentic_queries` subject families control their
  respective runtime routes

#### Scenario: Legacy gateway output fails startup

- **GIVEN** an external graph-gateway configuration using legacy output `queries`
- **WHEN** startup validation runs
- **THEN** startup fails
- **AND** the error identifies the required canonical output names and subject families
- **AND** no alias or compatibility shim is applied

#### Scenario: Incomplete or malformed gateway outputs fail startup

- **WHEN** a graph-gateway output is missing, duplicated, extra, optional, wrong-kind, or uses the wrong subject family
- **THEN** startup fails before route registration
- **AND** no missing output is autofilled

#### Scenario: Shared mux rejects composition input

- **GIVEN** a shared-mux graph-gateway declaration containing an input
- **WHEN** startup validation runs
- **THEN** startup fails
- **AND** `bind_address` does not cause an input port to be synthesized
