# projection-mutation-client — delta (immutable-birth-predicates)

## MODIFIED Requirements

### Requirement: Create-only birth predicates are not graph-enforced immutable facts

`Contract` MUST expose optional `BirthPredicates`. Every birth predicate MUST be a registered canonical exact
predicate. Duplicates and overlap with any `replace-owned`, `cas-transition`, or `append-evidence` group in the same
contract MUST be rejected.

Birth predicates MUST derive no ownership or foreign-edge claim, MUST NOT participate in a replacement removal set,
and MUST NOT authorize append. A contract containing only birth predicates MUST be valid.

A birth predicate MAY equal a foreign-edge predicate because foreign edges apply to a different subject lane.
Create-only MUST describe authorization through this client only. Graph-ingest MUST NOT be represented as enforcing
write-once behavior for these predicates by virtue of this declaration alone. A nonconforming writer using another
accepted mutation lane MAY change or remove them — unless the predicate additionally carries the vocabulary
immutable classification, which is the server-enforced mechanism (see the predicate-contract and graph-ingest
capabilities). A contract author who needs the guarantee to hold against every mutation lane MUST declare the
predicate immutable in the vocabulary; `BirthPredicates` alone MUST NOT be represented as providing it.

#### Scenario: Valid birth-only contract

- **WHEN** a contract declares at least one valid birth predicate and no groups or foreign edges
- **THEN** contract validation succeeds and ownership derivation produces no claim for those predicates

#### Scenario: Birth predicate overlaps mutable group

- **WHEN** a predicate appears in both `BirthPredicates` and any predicate group
- **THEN** contract validation fails before ownership registration

#### Scenario: Birth predicate is not canonical

- **WHEN** a birth predicate is undeclared, empty, duplicated, or contains a predicate wildcard
- **THEN** contract validation fails before ownership registration

#### Scenario: Birth predicate with vocabulary immutability is server-enforced

- **WHEN** a birth predicate also carries the vocabulary immutable classification and a writer on any accepted
  mutation lane attempts to change or remove its seeded value
- **THEN** graph-ingest refuses the attempt under the immutability contract, independent of this client
