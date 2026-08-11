## MODIFIED Requirements

### Requirement: Non-predicate codecs keep their recorded rationale

NAME MUST keep fixed-width hashed keys for its open-content name axis, with the original case and priority retained in
the small per-membership value. NAME and INCOMING MAY keep reversible `hex(predicate)` in their composite reverse-index
layouts so readers can reconstruct the accepted predicate without a catalog lookup.

`PREDICATE_INDEX` MUST NOT use either codec: its fixed nine-token membership key is the raw canonical three-part
predicate followed by the canonical six-part entity ID, and `PREDICATE_CATALOG` is absent. No codec, hash, or physical
key representation MAY be treated as acceptance authority for a predicate that violates the canonical grammar.
This requirement documents the shipped layout only and SHALL NOT trigger a runtime index migration.

#### Scenario: encoding cannot admit an invalid predicate

- **GIVEN** a predicate that could be hex-encoded or hashed into a KV-safe token but violates canonical syntax
- **WHEN** graph state or a derived membership is written
- **THEN** predicate validation rejects it before graph-index I/O

#### Scenario: each surviving codec stays local to its axis

- **WHEN** NAME, INCOMING, and PREDICATE membership layouts are inspected
- **THEN** NAME hashes its normalized name axis, NAME and INCOMING use reversible predicate hex, and PREDICATE uses the
  raw canonical predicate
- **AND** no shared predicate catalog or codec authority is inferred

### Requirement: Surviving index key axes are KV-safe and reconstructable

Every token of a sharded index key MUST be NATS-KV-safe and unambiguously reconstructable under its declared layout:

- **NAME open-vocabulary axis:** the normalized name value MUST hash to a fixed-width token so raw dotted values cannot
  collide with token positions under NATS prefix matching. Its composite membership key MAY retain the reversible
  predicate hex token; the per-key value MUST retain original case and priority.
- **INCOMING predicate axis:** the already-validated canonical predicate MAY use the reversible untagged hex token
  retained by that storage layout. Decoding reconstructs evidence; it does not authorize acceptance.
- **PREDICATE membership axis:** the raw canonical predicate MUST occupy exactly three leading tokens followed by the
  six-token entity ID, producing one fixed nine-token key per membership. No hash or catalog participates.
- **Entity-ID axes:** raw entity IDs MAY be used only after six-token validation. A writer MUST skip malformed values
  visibly and MUST NOT emit an empty token or mis-split key.

`INCOMING` keys on target ID with source-owner evidence represented in the remainder of its layout; `NAME` keys on the
name hash; `PREDICATE_INDEX` keys on raw predicate plus entity ID. Graph-index replay MUST visibly reject malformed
predicates or entity IDs before membership I/O and keep readiness honest under rejected current state.

#### Scenario: codec round-trip does not change predicate acceptance

- **GIVEN** arbitrary bytes are passed directly to the NAME/INCOMING predicate codec
- **WHEN** the encoded token is decoded
- **THEN** the codec reconstructs the exact original bytes
- **AND** that result does not authorize a graph or index write

#### Scenario: a noncanonical current predicate is rejected before index I/O

- **GIVEN** a current write candidate whose predicate has the wrong arity, whitespace, or a wildcard token
- **WHEN** it reaches the authoritative graph-write contract or graph-index replay validation
- **THEN** the candidate is rejected before membership or reverse-index I/O
- **AND** invalid preexisting replay state keeps readiness false

#### Scenario: a malformed entity ID is skipped, not indexed into a mis-split key

- **WHEN** a write path composes a key from an entity ID that is not a valid six-token ID
- **THEN** the write is skipped and logged
- **AND** no malformed key is stored

#### Scenario: predicate membership needs no catalog reconstruction

- **GIVEN** one current raw `PREDICATE_INDEX` membership key
- **WHEN** a reader reconstructs its semantic axes
- **THEN** the first three tokens yield the exact canonical predicate and the remaining six yield the entity ID
- **AND** no `PREDICATE_CATALOG`, hash lookup, or reversible predicate codec is consulted
