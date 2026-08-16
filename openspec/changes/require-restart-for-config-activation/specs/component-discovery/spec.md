## ADDED Requirements

### Requirement: Registry admission is boot-owned and runtime composition seals

Registry SHALL admit component declarations only as part of ComponentManager's validated boot composition. After the
boot set is sealed, Registry SHALL NOT create, replace, or remove a declaration generation in response to config,
model-registry, flow, rule, HTTP, or direct KV mutation.

Registry SHALL expose defensive declaration values and SHALL NOT expose runtime component handles or lifecycle
authority. Terminal shutdown MAY retire process-local state only after the owning ComponentManager has canceled and
joined the exact runtime generations; shutdown is not live reconfiguration.

#### Scenario: Post-boot desired edit cannot change Registry

- **GIVEN** Registry has sealed boot generation G for component A
- **WHEN** desired state adds, changes, disables, or removes A
- **THEN** Registry continues to expose complete generation G for the running process
- **AND** no candidate, transition, reservation, or partial declaration is published

#### Scenario: Rule hot reload does not create a component generation

- **GIVEN** a sealed Rule component generation
- **WHEN** its dedicated rule-definition capability activates a new rule-set generation
- **THEN** the Registry component generation and declaration remain unchanged

#### Scenario: Registry has no replacement capability

- **WHEN** a caller seeks to replace a running component generation
- **THEN** no supported Registry API provides that operation
- **AND** the caller must persist desired state and restart the process

## MODIFIED Requirements

### Requirement: Registry retains one accepted declaration per component generation

For each successful boot admission, Registry SHALL retain one immutable generation record containing validated factory
identity, cloned effective input ports, cloned effective output ports, normalized facts derived from those exact
clones, exclusive-resource facts, and a process-local generation identifier. It SHALL NOT retain or expose a runtime
component handle, lifecycle state, readiness, or availability.

Registry SHALL capture each port declaration exactly once for the admitted boot generation and SHALL publish no
generation for an absent or disabled component, failed construction/declaration validation, conflict, or admission
failure. A later component Start failure MAY leave the honestly admitted shape inspectable, but declaration presence
SHALL NOT imply readiness.

#### Scenario: Successful admission captures declaration without a handle

- **GIVEN** an enabled component factory whose declaration is valid and conflict-free
- **WHEN** ComponentManager admits its boot generation
- **THEN** Registry captures each port method exactly once
- **AND** the retained factory identity, cloned ports, normalized facts, resources, and generation describe one
  admission
- **AND** no Registry value contains the runtime component

#### Scenario: Failed admission publishes no record

- **GIVEN** a disabled component or a component with invalid or conflicting declarations
- **WHEN** boot admission is attempted
- **THEN** Registry exposes no component generation or partial declaration/resource projection

#### Scenario: Start failure does not imply readiness

- **GIVEN** a component was admitted successfully and its later boot Start fails
- **WHEN** a reader inspects Registry
- **THEN** the admitted declaration remains honest process-local shape
- **AND** no Registry field or presence claim reports the component ready

### Requirement: Registry reads and observation expose defensive complete snapshots

Registry SHALL return defensive clones for individual and complete-set reads. Its process-local observer SHALL deliver
one complete current set initially, including an empty set, and SHALL deliver the newest complete set after successful
boot admission. After composition seals, no configuration mutation SHALL change that set.

Observation SHALL be latest-state and coalescing, SHALL NOT block Registry mutation, and SHALL release observer
resources on cancellation.

The observer SHALL be an internal framework-only API. It SHALL NOT establish an accepted cross-repo API or contract
and SHALL NOT imply an ADR commitment. Observation SHALL remain in process memory and SHALL NOT use KV, JetStream, or
other durable storage, nor provide durable replay or recovery.

#### Scenario: Reader mutation cannot alter Registry

- **GIVEN** a reader receives a generation snapshot
- **WHEN** the reader mutates its returned ports or facts
- **THEN** a subsequent Registry read remains unchanged

#### Scenario: Observer starts empty and coalesces boot admission

- **GIVEN** an empty Registry and a new observer
- **WHEN** boot generations are admitted faster than the observer consumes notifications
- **THEN** the observer first receives the complete empty set
- **AND** later receives a complete newest set rather than an event-log guarantee
- **AND** Registry mutation does not block

#### Scenario: Sealed Registry has no mutation event

- **GIVEN** boot composition has sealed
- **WHEN** desired component configuration changes
- **THEN** the observer receives no replacement or removal event
- **AND** the complete process-local set remains unchanged

#### Scenario: Observer cancellation releases resources

- **WHEN** an observer is cancelled
- **THEN** Registry releases its delivery resources
- **AND** no further delivery is required

#### Scenario: Observation remains an internal process-local view

- **WHEN** a framework consumer observes Registry generations
- **THEN** delivery uses only the internal process-local observer
- **AND** no KV, JetStream, durable store, durable replay, cross-repo contract, or ADR promise is created

### Requirement: Every admitted generation has validated factory identity

`CreateComponent` SHALL be the sole production component-admission path during validated boot composition. Every
admission SHALL require validated factory identity and SHALL perform declaration capture, validation, conflict, and
atomic-admission checks.

No identity-free admission alias, prepared-replacement path, inference, deprecated path, or compatibility shim SHALL
exist.

#### Scenario: Identity-free admission is unavailable

- **GIVEN** a caller has only an instance name and component reference
- **WHEN** it attempts production admission without validated factory identity
- **THEN** no supported Registry API admits the generation

#### Scenario: Post-seal admission is unavailable

- **GIVEN** validated boot composition has sealed
- **WHEN** a caller seeks to admit another component generation
- **THEN** no supported Registry API admits it into the running process

### Requirement: Shared runtime consumers use the retained generation

Conflict reporting, capability publication, flowgraph, management responses, and message-logger declaration discovery
SHALL consume defensive Registry snapshots and SHALL NOT call component port methods or resolve definitions again.

Asynchronous capability publication SHALL capture its defensive snapshot before starting the goroutine. The sealed
boot generation SHALL remain valid for the process lifetime.

#### Scenario: Asynchronous publication uses the captured boot generation

- **GIVEN** capability publication captures boot generation G
- **WHEN** asynchronous publication runs after desired component configuration changes
- **THEN** it publishes the internally consistent captured generation G snapshot
- **AND** it does not resolve or recapture a next-boot declaration
