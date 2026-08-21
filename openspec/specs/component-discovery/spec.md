# component-discovery Specification

## Purpose
Defines the single normalized port-fact projection used by component discovery, composition, schema, and provisioning.
## Requirements
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

### Requirement: Registry is the sole declaration-derived resource admission owner

Registry SHALL validate declaration conflicts and exclusive-resource facts during boot admission. All shared
declaration consumers SHALL read the retained defensive values rather than call component port methods or resolve
factory definitions again.

ComponentManager SHALL NOT retain a parallel resources map, conflict classifier, registration/unregistration
bookkeeping, or component port re-read path.

Asynchronous consumers SHALL capture their defensive snapshot before starting work. The captured boot declaration
set SHALL remain valid for the process lifetime.

#### Scenario: Conflict is rejected by one owner

- **GIVEN** an admitted boot declaration already claims an exclusive resource
- **WHEN** another boot declaration declares the conflicting resource
- **THEN** Registry rejects the admission
- **AND** neither Registry nor ComponentManager exposes any partial second claim

#### Scenario: Asynchronous publication uses captured boot declarations

- **GIVEN** a consumer captures the sealed declaration set
- **WHEN** desired component configuration changes before asynchronous publication completes
- **THEN** the consumer publishes the internally consistent captured set
- **AND** it does not resolve or recapture next-boot declarations

### Requirement: Graph query request composition uses one versioned operation family

Graph-query SHALL declare exactly one required input named `graph_queries` with direction `input`, kind
`nats-request`, subject family `graph.query.*`, interface type `graph.query`, and interface version `v1`. Every admitted
graph-query responder SHALL derive its exact one-token operation suffix from that resolved family. A handler SHALL NOT
subscribe through a separate literal that bypasses the declaration.

Graph-gateway SHALL retain exactly its three required query-family outputs. Its existing `graph_queries` output SHALL
use `graph.query.*` and interface `graph.query` version `v1` and SHALL cover the fourteen admitted GraphQL operations
without creating fourteen ports.

Research classify SHALL declare one required `searchGraph` output. Research execute SHALL declare four required
outputs for `batch`, `relationships`, `temporal`, and `searchGraph`. Each SHALL be kind `nats-request`, use its exact
`graph.query.<operation>` subject, and carry interface `graph.query` version `v1`. Agentic-tools SHALL declare no output
for the deleted search or summary wrappers.

Libraries and E2E harnesses SHALL NOT synthesize component ports. Slice E adds no port or configuration requirement for
`pkg/fusion/fusionnats.Client` because no current in-repo component constructs it. Research classify and execute retain
their already-admitted exact graph-query outputs.

#### Scenario: canonical graph-query provider and gateway compose

- **GIVEN** graph-query's required `graph.query.*` input and graph-gateway's required matching family output
- **WHEN** Registry resolves their normalized facts
- **THEN** direction, kind, subject containment, required state, interface type, and version compose
- **AND** all sixteen provider subjects derive from the one resolved input family

#### Scenario: research declares only the operations it requests

- **GIVEN** research classify and execute are admitted
- **WHEN** their effective output declarations are inspected
- **THEN** classify declares only its required `searchGraph` query dependency
- **AND** execute declares its required `batch`, `relationships`, `temporal`, and `searchGraph` dependencies
- **AND** no general query-client or wildcard dependency is invented

#### Scenario: undeclared or mismatched request fails before execution

- **GIVEN** an embedded component requests an operation without its exact output, or provider and consumer disagree on
  family, kind, required state, interface type, or interface version
- **WHEN** production factory and Registry validation run
- **THEN** admission fails before subscription or request handling
- **AND** no literal bypass, alias, autofill, library port, or compatibility shim repairs it

#### Scenario: shipped configuration census stays mechanically complete

- **WHEN** all twenty-one shipped configurations load through production factories and Registry
- **THEN** the effective set contains eleven graph-query, eight graph-gateway, two research-classify, two
  research-execute, and nine agentic-tools instances with the exact declarations above
- **AND** raw counts are `395/243/54`, effective counts are `571/378/69`, and the raw-to-effective delta remains
  `176/135/15`

### Requirement: Registry admission is boot-owned and seals

Registry SHALL admit validated component declarations only while ComponentManager constructs the fixed boot
composition. ComponentManager SHALL seal Registry when that composition is complete. After sealing, no supported API
SHALL admit, replace, remove, or mutate a declaration in response to configuration, model-registry, Flow, Rule, HTTP,
or direct KV changes.

Registry SHALL NOT own component lifecycle. ComponentManager SHALL remain the sole owner of concrete runtime component
handles.

#### Scenario: Boot admission succeeds before sealing

- **GIVEN** an enabled component with validated factory identity and valid declarations
- **WHEN** ComponentManager constructs the boot composition
- **THEN** Registry admits one complete declaration value for that component
- **AND** ComponentManager retains the concrete runtime handle

#### Scenario: Post-seal admission is rejected

- **GIVEN** ComponentManager has sealed Registry
- **WHEN** any caller attempts to admit another component or replace an admitted declaration
- **THEN** Registry rejects the operation
- **AND** the running component set remains unchanged

#### Scenario: Later configuration write does not mutate Registry

- **GIVEN** Registry contains the sealed boot declarations
- **WHEN** component or model-registry configuration changes
- **THEN** Registry continues to expose the same boot declarations
- **AND** it publishes no replacement or removal transition

### Requirement: Registry exposes defensive declaration values without handles

Registry SHALL expose immutable defensive copies of admitted declaration values. A declaration value MAY contain
validated factory identity, cloned input and output ports, normalized facts, and exclusive-resource facts. It SHALL
NOT contain or return a runtime component handle, lifecycle authority, readiness, or availability.

Registry SHALL capture each component's declaration once during successful boot admission. Failed, disabled, invalid,
or conflicting components SHALL publish no partial declaration. Declaration presence SHALL NOT imply successful
component Start.

#### Scenario: Reader mutation cannot alter Registry

- **GIVEN** a reader receives a declaration snapshot
- **WHEN** the reader mutates returned ports or facts
- **THEN** a later Registry read is unchanged

#### Scenario: No supported read returns a component handle

- **WHEN** a caller reads one declaration or the complete Registry snapshot
- **THEN** the result contains declaration values only
- **AND** no supported Registry API returns the runtime component

#### Scenario: Failed admission publishes nothing

- **GIVEN** a disabled component or a component with invalid or conflicting declarations
- **WHEN** ComponentManager attempts boot admission
- **THEN** Registry contains no partial record for that component

#### Scenario: Start failure does not imply readiness

- **GIVEN** declaration admission succeeded and the later component Start fails
- **WHEN** a reader inspects Registry
- **THEN** the admitted declaration remains an honest description of boot shape
- **AND** no Registry field or presence claim reports the component ready
