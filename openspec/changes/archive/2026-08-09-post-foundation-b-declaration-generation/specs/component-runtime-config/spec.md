## MODIFIED Requirements

### Requirement: A runtime config change is applied via any supported reconfig contract

The ComponentManager config API MUST hot-apply a `PUT config/<component>` update to a running component that
implements either supported component-side contract: `UpdateConfig(ctx, json.RawMessage)` or the anonymous method pair
`ValidateConfigUpdate(map[string]any)` plus `ApplyConfigUpdate(map[string]any)`.

The manager MUST probe the anonymous method pair directly and MUST NOT require or consult any service runtime-config
interface. A component implementing only the method pair, including rule processor, MUST be reached rather than
silently skipped. When a component implements both contracts, `UpdateConfig` MUST be used.

#### Scenario: a method-pair component is hot-applied over HTTP

- **GIVEN** a running component that implements the reconfig method pair but not `UpdateConfig`
- **WHEN** a valid `PUT config/<component>` request is received
- **THEN** the manager calls the component's `ValidateConfigUpdate` then `ApplyConfigUpdate`
- **AND** the running component reflects the change without a restart

#### Scenario: an UpdateConfig component keeps its existing path

- **GIVEN** a running component that implements `UpdateConfig(ctx, json.RawMessage)`
- **WHEN** a valid `PUT config/<component>` request is received
- **THEN** the manager applies the change via `UpdateConfig`
- **AND** it does not additionally invoke the anonymous method pair

## ADDED Requirements

### Requirement: Declarations are immutable within a generation

Before any component or retained-config mutation, a declaration-neutral live update SHALL prove exact normalized-fact
equality with the retained generation. A neutral update SHALL retain the current generation.

A declaration-affecting update SHALL either return typed `declaration_change_requires_replacement` before mutation or
prepare a complete replacement generation off-Registry. No path SHALL mutate a live component and then recapture its
declaration.

#### Scenario: Declaration-neutral update retains generation

- **GIVEN** a proposed live update whose normalized port facts equal the retained generation
- **WHEN** validation and application succeed
- **THEN** the component may update
- **AND** the retained generation identity and declaration remain unchanged

#### Scenario: Port change refuses before mutation

- **GIVEN** a proposed live update whose normalized port facts differ
- **WHEN** no prepared replacement path is used
- **THEN** the update returns `declaration_change_requires_replacement`
- **AND** the component and retained config remain unchanged

#### Scenario: Mutate then recapture is forbidden

- **GIVEN** a declaration-affecting update
- **WHEN** the runtime evaluates it
- **THEN** no path first mutates the live component and later recaptures ports

### Requirement: Replacement publishes one atomic generation

A failed replacement preparation SHALL leave the old component, retained configuration, generation record, and
resource projections unchanged and SHALL expose no partial new record.

A successful replacement SHALL assign a new local generation and atomically replace component, factory identity,
declaration, and resource projections as one Registry-visible mutation.

#### Scenario: Failed prepared replacement changes nothing

- **GIVEN** a current admitted generation and a replacement that fails preparation or conflict validation
- **WHEN** replacement is attempted
- **THEN** every read still returns the old complete generation
- **AND** no new resource fact is visible

#### Scenario: Successful replacement is observed as one set

- **GIVEN** a valid prepared replacement
- **WHEN** Registry commits it
- **THEN** readers and observers see either the old complete generation or the new complete generation
- **AND** no mixed component/declaration/resource state is visible

### Requirement: Removal deletes one complete generation record

Removal SHALL delete the component reference, factory identity, declaration, normalized facts, and resource
projections together.

#### Scenario: Removal has no residual declaration

- **GIVEN** an admitted component generation
- **WHEN** it is removed
- **THEN** the component and every declaration/resource view disappear in the same Registry mutation
