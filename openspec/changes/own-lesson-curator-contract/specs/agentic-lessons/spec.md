## ADDED Requirements

### Requirement: External lesson composition uses the framework-owned contract snapshot

The framework MUST expose a purpose-scoped function returning an independent copy of the canonical lesson-record
projection contract. A product composition root MUST be able to include that snapshot in its local projection mutation
client without reproducing the contract name, lifecycle group name, entity pattern, predicate membership, or
birth-versus-mutable classification.

`LessonCurator` MUST continue to depend on the narrow `PredicateReconciler` and `AuthoritativeReader`
capabilities. The framework MUST NOT reintroduce the retired `NewNATSLessonCurator` helper.

The snapshot path MUST NOT introduce a bespoke agent, LLM persona, prompt role, or framework agent type.

#### Scenario: External composition uses the canonical lesson contract

- **GIVEN** first-party vocabulary is registered and a connected NATS client is available
- **WHEN** a product includes `LessonProjectionContract()` in its local mutation-client contract set
- **THEN** construction validates the framework-owned canonical lesson contract
- **AND** the product supplies no copied lesson-contract literals
- **AND** the product injects only reconciler and authoritative-reader capabilities into `LessonCurator`

#### Scenario: Contract snapshots are independent

- **WHEN** a caller modifies the contract or nested predicate slices returned by `LessonProjectionContract()`
- **THEN** a later call returns the unchanged canonical lesson contract

#### Scenario: Canonical lifecycle transition preserves birth facts

- **GIVEN** a lesson record contains every framework-declared birth predicate and a valid lifecycle state
- **WHEN** a curator composed from `LessonProjectionContract()` promotes, retires, or supersedes the lesson
- **THEN** every birth predicate retains its prior object set
- **AND** the lifecycle predicate group equals the complete desired state for that transition

#### Scenario: Retired NATS helper remains absent

- **WHEN** standard lesson composition is inspected
- **THEN** the mutation client is constructed at the product composition root
- **AND** `LessonCurator` receives only its narrow reconciler and authoritative-reader capabilities
- **AND** no `NewNATSLessonCurator` production helper is exposed
