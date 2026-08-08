## ADDED Requirements

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
