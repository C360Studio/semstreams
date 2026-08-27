## ADDED Requirements

### Requirement: Harness births carry the registered lifecycle type

Every entity the lifecycle `Manager` births MUST be stamped `lifecycle.harness.v1`, and that type MUST be registered by the
framework builtin payload set as a Graphable carrier (`lifecycle.HarnessEntity`) with floor `control`, so a harness birth
passes graph-ingest's registered-type gate and a harness entity can arrive on the fact lane as itself. Registering a carrier
makes the fact-lane merge path reachable for lifecycle entities (the same class as `storage.stored.v1`): a marshalled harness
entity arriving on a Graphable input merges by predicate replacement like any other Graphable. Per-workflow contracts remain
with `Manager.Register`; the type registers no contract.

#### Scenario: a workflow birth passes the registered-type gate

- **GIVEN** the framework builtin payload set is registered in graph-ingest's registry
- **WHEN** `Manager.Create` births a participant
- **THEN** the entity is created with `message_type` `lifecycle.harness.v1`
- **AND** `mutation_rejections_total{reason="message_type_unregistered"}` does not increment
- **AND** the test that verifies this is `TestHarnessBirthPassesRegisteredTypeGate` (integration: `Manager.Create` against a
  real graph-ingest holding the builtin set; the counter unchanged); `TestManager_RoundTripCreateGetTransition` pins the
  stamp on the captured create request

