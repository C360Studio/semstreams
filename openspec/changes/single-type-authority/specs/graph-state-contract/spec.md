## ADDED Requirements

### Requirement: The canonical codec and the boot sweep never consult the payload registry

`message_type` on a stored entity MUST be recorded from a registered key at write time and MUST be interpreted as provenance
only: the canonical decoder, `ValidateEntityStateContract`, the boot snapshot sweep, the Graphable merge path, and every
authoritative reader MUST NOT consult the payload registry, so an entity persisted under a key that is later unregistered stays readable, is never
inventoried as poison, and remains mutable through must-exist operations.

#### Scenario: a resident entity with an unregistered stamp is not poison

- **GIVEN** an `ENTITY_STATES` value whose `message_type` is registered in no binary
- **WHEN** graph-ingest boots and sweeps
- **THEN** the entity has no poison inventory entry
- **AND** an exact read returns it with the stamp unchanged

#### Scenario: must-exist mutations ignore the stamp

- **GIVEN** the same entity
- **WHEN** a `triple.append` targets it
- **THEN** the append is evaluated on the entity's current revision without consulting the registry
