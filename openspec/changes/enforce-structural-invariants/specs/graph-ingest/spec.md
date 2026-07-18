## ADDED Requirements

### Requirement: Ingest MUST reject structurally-invalid entity IDs and predicates

graph-ingest — the sole writer to `ENTITY_STATES` — MUST validate every mutation's entity ID and every
triple predicate against the structural-identity contract (entity ID = exactly 6 non-empty parts;
predicate = exactly 3 non-empty parts) before persistence. A mutation carrying any structurally-invalid
token MUST be rejected in full with a classified validation error, MUST NOT be persisted (not the bad
token, not the rest of the mutation), and MUST emit a loud (WARN or ERROR) log naming the offending
token, its kind (entity-id vs predicate), the source (rule/caller/subject), and the reason. Enforcement
is fail-closed: the write boundary is the single choke point, so a non-conforming token cannot enter the
graph regardless of its producer (framework, rule-stamped, product, or agent-authored).

#### Scenario: A mutation with a non-3-part predicate is rejected
- **WHEN** a graph mutation carries a triple whose predicate is `agent.role` (two parts)
- **THEN** the mutation is rejected with a classified validation error
- **AND** nothing from the mutation is written to `ENTITY_STATES`
- **AND** a loud log names the predicate, that it is a predicate, the source, and the reason

#### Scenario: A mutation with a non-6-part entity ID is rejected
- **WHEN** a graph mutation targets entity ID `acme.ops.robotics.gcs.drone` (five parts)
- **THEN** the mutation is rejected with a classified validation error and is not persisted

#### Scenario: A fully-conforming mutation is persisted unchanged
- **WHEN** a graph mutation targets a 6-part entity ID and carries only 3-part predicates
- **THEN** it passes the structural gate and is persisted with existing merge semantics intact

### Requirement: The structural gate MUST support an observe-only dry-run before fail-closed enforcement

Before enforcement rejects live writes, the structural gate MUST support an observe-only mode that
validates every entity ID and predicate and increments a rejection metric
(`graph_ingest_structural_rejects_total{kind,reason}`) WITHOUT rejecting the mutation, so a real violator
surfaces as a counted, logged event rather than a silent reject. Fail-closed enforcement MUST NOT be
enabled until the audit over the reference-config/vocabulary corpus and live ingest is clean.

#### Scenario: Observe-only mode counts but does not reject
- **WHEN** the gate is in observe-only mode and a mutation carries a structurally-invalid token
- **THEN** the `graph_ingest_structural_rejects_total` metric increments with the kind and reason
- **AND** the mutation is still persisted (dry-run does not change behavior beyond metrics/logs)
