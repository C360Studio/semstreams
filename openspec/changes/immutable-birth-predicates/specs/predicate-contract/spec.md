# predicate-contract — delta (immutable-birth-predicates)

## ADDED Requirements

### Requirement: A predicate MAY be declared immutable, and the declaration MUST grant no authority

The vocabulary registry MUST accept an immutable classification on a registered canonical
predicate, entity-agnostic and product-neutral. Declaring a predicate immutable MUST grant the
declarer no write authority of any kind: mutation-lane access remains the trust boundary, and
which principals may perform the initial seed of an immutable predicate is host NATS ACL
policy, outside this contract. Documentation MUST state this split explicitly.

#### Scenario: Classification is declarable beside existing metadata

- **WHEN** a package registers a canonical predicate with the immutable classification
- **THEN** the registry records it and enforcement components can read it, exactly as with
  existing per-predicate classifications

#### Scenario: Declaration grants nothing

- **WHEN** a package declares a predicate immutable but holds no mutation-lane access
- **THEN** the declaration enables no write path; the package cannot seed or modify the
  predicate through any framework surface

#### Scenario: Late declaration is safe

- **WHEN** a predicate already stored on entities is later declared immutable
- **THEN** existing values freeze as resident and subsequent mutation attempts are governed
  by the immutability contract
