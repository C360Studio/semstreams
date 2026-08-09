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

### Requirement: Registry retains one accepted declaration per component generation

For each successful component admission, Registry SHALL retain one immutable generation record containing validated
factory identity, component reference, cloned effective input ports, cloned effective output ports, normalized facts
derived from those exact clones, exclusive-resource facts, and a process-local generation identifier.

Registry SHALL call `InputPorts` and `OutputPorts` exactly once for that generation and SHALL publish no generation for
an absent or disabled component, failed declaration validation, conflict, or other admission failure.

A later component Start failure MAY leave the honestly admitted shape inspectable, but declaration presence SHALL NOT
imply readiness.

#### Scenario: Successful admission captures ports once

- **GIVEN** an enabled component factory whose declaration is valid and conflict-free
- **WHEN** Registry admits the component
- **THEN** Registry calls each port method exactly once
- **AND** the retained component, factory identity, cloned ports, normalized facts, resources, and generation describe
  that one admission

#### Scenario: Failed admission publishes no record

- **GIVEN** a disabled component or a component with invalid or conflicting declarations
- **WHEN** admission is attempted
- **THEN** Registry exposes no component generation or partial declaration/resource projection

#### Scenario: Start failure does not imply readiness

- **GIVEN** a component was admitted successfully and its later Start fails
- **WHEN** a reader inspects Registry
- **THEN** the admitted declaration remains honest process-local shape
- **AND** no Registry field or presence claim reports the component ready

### Requirement: Registry is the sole declaration-derived resource admission owner

Registry SHALL validate declaration-derived exclusive-resource conflicts and SHALL publish component and resource
state as one admission mutation.

ComponentManager SHALL NOT retain a parallel resources map, conflict classifier, registration/unregistration
bookkeeping, or component port re-read path.

#### Scenario: Conflict is rejected by one owner

- **GIVEN** an admitted generation already claims an exclusive resource
- **WHEN** another generation declares the conflicting resource
- **THEN** Registry rejects the admission
- **AND** neither Registry nor ComponentManager exposes any partial second claim

### Requirement: Registry reads and observation expose defensive complete snapshots

Registry SHALL return defensive clones for individual and complete-set reads. Its process-local observer SHALL deliver
one complete current set initially, including an empty set, and SHALL deliver the newest complete set after successful
add, replacement, or removal.

Observation SHALL be latest-state and coalescing, SHALL NOT block Registry mutation, and SHALL release observer
resources on cancellation.

The observer SHALL be an internal framework-only API. It SHALL NOT establish an accepted cross-repo API or contract
and SHALL NOT imply an ADR commitment. Observation SHALL remain in process memory and SHALL NOT use KV, JetStream, or
other durable storage, nor provide durable replay or recovery.

#### Scenario: Reader mutation cannot alter Registry

- **GIVEN** a reader receives a generation snapshot
- **WHEN** the reader mutates its returned ports or facts
- **THEN** a subsequent Registry read remains unchanged

#### Scenario: Observer starts empty and coalesces latest state

- **GIVEN** an empty Registry and a new observer
- **WHEN** generations are added, replaced, or removed faster than the observer consumes notifications
- **THEN** the observer first receives the complete empty set
- **AND** later receives a complete newest set rather than an event-log guarantee
- **AND** Registry mutation does not block

#### Scenario: Observer cancellation releases resources

- **WHEN** an observer is cancelled
- **THEN** Registry releases its delivery resources
- **AND** no further delivery is required

#### Scenario: Observation remains an internal process-local view

- **WHEN** a framework consumer observes Registry generations
- **THEN** delivery uses only the internal process-local observer
- **AND** no KV, JetStream, durable store, durable replay, cross-repo contract, or ADR promise is created

### Requirement: Every admitted generation has validated factory identity

`CreateComponent` SHALL be the sole production component-admission path. Any internal prepared-replacement or test
helper SHALL require validated factory identity and SHALL perform the same declaration capture, validation, conflict,
and atomic-admission checks.

No identity-free admission alias, inference, deprecated path, or compatibility shim SHALL exist.

#### Scenario: Identity-free admission is unavailable

- **GIVEN** a caller has only an instance name and component reference
- **WHEN** it attempts production admission without validated factory identity
- **THEN** no supported Registry API admits the generation

### Requirement: Admission snapshots are group-neutral shape

A Registry generation SHALL contain no enabled, started, healthy, ready, provider-phase, group, cohort, or
orchestration field. A complete Registry snapshot set SHALL be an admission census, not an inferred atomic cohort.

Independently valid subsets SHALL remain visible. Capability-specific completeness SHALL remain at the capability's
composition boundary.

#### Scenario: Independent subset remains visible

- **GIVEN** one valid component generation is admitted while a related capability member is absent
- **WHEN** the Registry complete set is read
- **THEN** the valid generation is visible
- **AND** Registry does not withhold it or infer capability completeness

### Requirement: Shared runtime consumers use the retained generation

Conflict reporting, capability publication, flowgraph, management responses, and message-logger declaration discovery
SHALL consume defensive Registry snapshots and SHALL NOT call component port methods or resolve definitions again.

Asynchronous capability publication SHALL capture its defensive snapshot before starting the goroutine.

#### Scenario: Asynchronous publication survives replacement

- **GIVEN** capability publication captures generation N and the instance is then replaced by generation N+1
- **WHEN** the asynchronous publication runs
- **THEN** it publishes the internally consistent captured generation N snapshot
- **AND** it does not re-read the replaced component
