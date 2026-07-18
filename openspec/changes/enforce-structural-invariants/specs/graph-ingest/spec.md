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

### Requirement: The structural gate MUST be unconditionally fail-closed with no bypass configuration

The handler-level structural gate MUST be unconditionally fail-closed: no bypass configuration exists
that lets a structurally-invalid predicate pass the gate. Every violation is metered
(`mutation_rejections{reason="structural_predicate_invalid"}`), logged loudly, and rejected with a
classified validation error. Behind the gate, the authoritative persistence seam — the entity-state
contract validation every `ENTITY_STATES` write path calls (`graph.MarshalEntityState` /
`ValidateEntityStateContract`) — independently rejects structurally-invalid predicates, so the gate and
the seam are two fail-closed layers and no configuration can weaken either. (An observe-only escape
hatch was prototyped during this change and removed pre-release as provably inert: the seam's
unconditional rejection meant the hatch could only swap the caller-visible error code, never permit
persistence.)

#### Scenario: No configuration can weaken the gate
- **WHEN** a mutation carries a non-3-part predicate on the `triple.add` or `triple.add_batch` lane,
  under any component configuration
- **THEN** the gate rejects the mutation with the classified structural code before any KV I/O
- **AND** the `mutation_rejections{reason="structural_predicate_invalid"}` metric increments and a log
  names the token
- **AND** nothing from the mutation is written to `ENTITY_STATES`
