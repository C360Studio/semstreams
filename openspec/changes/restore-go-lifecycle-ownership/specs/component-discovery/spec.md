## MODIFIED Requirements

### Requirement: Registry retains one accepted declaration per component generation

For each successful admission, Registry SHALL retain one immutable generation record containing validated factory
identity, cloned effective ports, normalized facts derived from those clones, exclusive-resource facts, and a
process-local generation identifier. It SHALL NOT retain or expose a runtime component handle, lifecycle state,
readiness, or availability.

Registry SHALL call port declaration methods exactly once during opaque manager-authorized preparation. The prepared
candidate and declaration proof SHALL be consumable only by ComponentManager and Registry admission respectively.
Registry SHALL publish no generation for failed preparation, validation, conflict, or admission.

Registry `Component`, `ListComponents`, deprecated `GetComponent`, handle-returning `CreateComponent` and
`ReplaceComponent`, construction-capability-returning `GetFactory`, and component references in generation snapshots
SHALL be retired without aliases. `RegisterFactory` MAY remain registration input and `ListFactories` MAY remain
value-only observation. Every other exported Registry method SHALL be value-only.

#### Scenario: Successful preparation captures declaration once

- **GIVEN** an enabled factory whose declaration is valid and conflict-free
- **WHEN** ComponentManager invokes the opaque authorized prepare operation
- **THEN** port declarations are captured exactly once
- **AND** Registry can admit the complete declaration proof without retaining the runtime handle

#### Scenario: Failed preparation publishes nothing

- **GIVEN** a factory fails construction, declaration validation, or conflict validation
- **WHEN** preparation returns
- **THEN** Registry exposes no generation or partial declaration/resource projection
- **AND** ComponentManager exposes no runtime entry

#### Scenario: Declaration presence does not imply availability

- **GIVEN** a declaration generation is admitted and its runtime later becomes Transitioning or Failed
- **WHEN** a declaration reader inspects Registry
- **THEN** the declaration remains identity-only
- **AND** no Registry field claims that its runtime handle is ready or available

### Requirement: Registry reads and observation expose defensive declaration snapshots

Registry SHALL return defensive clones for individual and complete-set declaration reads. Its process-local observer
SHALL deliver one complete current declaration set initially, including an empty set, and the newest complete set after
successful add, replacement commit, or removal.

During replacement, readers and observers SHALL see only the complete old or new declaration generation. They SHALL
never see a mixed record, runtime component reference, lifecycle phase, readiness, or availability evidence.

Observation SHALL be latest-state and coalescing, SHALL NOT block Registry mutation, and SHALL release resources on
cancellation. It SHALL remain internal, process-local, non-durable, and unusable as runtime access authority.

#### Scenario: Reader mutation cannot alter Registry

- **GIVEN** a caller receives a declaration snapshot
- **WHEN** it mutates returned ports or facts
- **THEN** a subsequent Registry read remains unchanged

#### Scenario: Replacement retains complete declaration identity

- **GIVEN** generation N is being replaced by prepared generation N+1
- **WHEN** a declaration reader or observer samples Registry
- **THEN** it sees complete N until commit or complete N+1 after commit
- **AND** neither snapshot contains a runtime handle or availability evidence

#### Scenario: Observer starts empty and coalesces

- **GIVEN** an empty Registry and a new observer
- **WHEN** declarations change faster than the observer consumes notifications
- **THEN** it first receives the complete empty set and later the newest complete set
- **AND** Registry mutation does not block

#### Scenario: Observer cancellation releases resources

- **WHEN** an observer is canceled
- **THEN** Registry releases delivery resources
- **AND** no further delivery is required
