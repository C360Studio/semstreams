## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Registry is the sole declaration-derived resource admission owner

Registry SHALL validate declaration conflicts and exclusive-resource facts during boot admission. All shared
declaration consumers SHALL read the retained defensive values rather than call component port methods or resolve
factory definitions again.

Asynchronous consumers SHALL capture their defensive snapshot before starting work. The captured boot declaration
set SHALL remain valid for the process lifetime.

#### Scenario: Asynchronous publication uses captured boot declarations

- **GIVEN** a consumer captures the sealed declaration set
- **WHEN** desired component configuration changes before asynchronous publication completes
- **THEN** the consumer publishes the internally consistent captured set
- **AND** it does not resolve or recapture next-boot declarations

## REMOVED Requirements

The headings below quote the exact legacy requirement names being removed so OpenSpec can match the baseline. Their
replacement-oriented terminology is historical and is not part of the new Registry contract.

### Requirement: Registry retains one accepted declaration per component generation

**Reason**: runtime replacement is retired. Registry retains one sealed boot declaration per admitted component,
without a replacement identity or lifecycle protocol.

**Migration**: read defensive boot declaration values; persist configuration changes and reboot to compose a new
process.

### Requirement: Every admitted generation has validated factory identity

**Reason**: validated factory identity remains required by the simpler boot-admission requirement above, but no
runtime replacement identity survives.

**Migration**: admit validated declarations during boot through the single ComponentManager-owned path.

### Requirement: Admission snapshots are group-neutral shape

**Reason**: complete defensive boot declaration values replace the former replacement-oriented snapshot contract.

**Migration**: consume the sealed declaration values without inferring lifecycle or replacement state.

### Requirement: Shared runtime consumers use the retained generation

**Reason**: shared consumers use the sealed boot declaration set; no runtime replacement identity is required.

**Migration**: capture defensive boot declarations before asynchronous use.
