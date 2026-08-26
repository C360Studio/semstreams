## ADDED Requirements

### Requirement: Harness births carry the registered lifecycle type

Every entity the lifecycle `Manager` births MUST be stamped `lifecycle.harness.v1`, and that type MUST be registered by the
framework builtin payload set as a Graphable carrier (`lifecycle.HarnessEntity`) with floor `control`, so a harness birth
passes graph-ingest's registered-type gate and a harness entity can arrive on the fact lane as itself. Per-workflow contracts
remain with `Manager.Register`; the type registers no contract.

#### Scenario: a workflow birth passes the registered-type gate

- **GIVEN** the framework builtin payload set is registered in graph-ingest's registry
- **WHEN** `Manager.Create` births a participant
- **THEN** the entity is created with `message_type` `lifecycle.harness.v1`
- **AND** `mutation_rejections_total{reason="message_type_unregistered"}` does not increment
