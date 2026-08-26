## ADDED Requirements

### Requirement: Every graph boundary enforces the deployment's own authority unless the write arrives on a declared import lane

graph-ingest MUST read the deployment authority from `deps.Platform` at construction and MUST validate positions 1–2
of every final candidate **subject** entity ID through `pkg/types.ValidateEntityIDAuthority` on every lane —
Graphable fact arrival, every `graph.mutation.>` operation, and direct persistence — before any KV I/O and after
structural validation. `@id` objects MUST NOT be authority-checked; they keep canonical structural validation; no stub is created and an absent object is permitted
(the unmodified requirement "Relationship target absence creates no entity"), so a local subject may reference an
imported entity. Mutation of an already-persisted
foreign-authority subject from a non-import lane follows owner item O-12; until it is ruled, this requirement's
rejection applies to it. On a lane whose input port is not declared `"import": true`, a candidate whose
positions 1–2 differ from the deployment's MUST be rejected. On a declared import lane, a candidate whose positions
1–2 equal the deployment's MUST be rejected, and a foreign candidate MUST be persisted with its identity bytes
unchanged. Each rejection MUST be metered exactly once as `mutation_rejections{reason="authority_foreign"}` or
`{reason="authority_claimed"}` and MUST emit a loud log naming the lane and the segment index, never the identity.
No configuration MAY disable the check. The import declaration and the envelope `source` string are the only
provenance this requirement records; it authenticates nothing.

#### Scenario: a foreign write on a local lane never reaches ENTITY_STATES

- **GIVEN** a deployment with authority `acme`/`dep1` and a Graphable whose ID is `acme.dep2.src.git.commit.a1`
- **WHEN** it arrives on a JetStream input port not declared as an import lane
- **THEN** no `ENTITY_STATES` key is created and no derived-index write follows
- **AND** `mutation_rejections{reason="authority_foreign"}` increments exactly once
- **AND** the test that verifies this is `TestAuthorityGateRejectsForeignOnFactLane`

#### Scenario: an import lane accepts foreign identity unchanged and refuses a local claim

- **GIVEN** the same deployment and a port declared `"import": true`
- **WHEN** `acme.dep2.src.git.commit.a1` arrives on it
- **THEN** it is persisted under exactly those bytes
- **AND WHEN** `acme.dep1.src.git.commit.a1` arrives on the same port
- **THEN** it is rejected with reason `local_authority_claimed`
- **AND** the test that verifies this is `TestImportLaneAcceptsForeignRejectsLocalClaim`

#### Scenario: a mutation reply carries the coded authority error

- **GIVEN** a `graph.mutation.>` request targeting an entity under a foreign authority on a non-import lane
- **WHEN** the reply is decoded into a fresh value
- **THEN** it carries code `entity_id_authority_invalid` with reason `foreign_authority`
- **AND** the structural code `entity_id_invalid` is not reported for a structurally valid candidate
- **AND** the test that verifies this is `TestAuthorityGateRejectsForeignOnMutationLane`

#### Scenario: a local subject may reference an imported entity

- **GIVEN** an imported entity `acme.dep2.src.git.commit.a1` persisted through an import lane
- **WHEN** a local entity `acme.dep1.agentic-loop.agent.execution.<uuid>` is created carrying an `@id` triple whose
  object is `acme.dep2.src.git.commit.a1`
- **THEN** the create is accepted and the reference is persisted unchanged
- **AND** the test that verifies this is `TestAuthorityGateAllowsForeignReferenceObject`

### Requirement: Hierarchy inference skips foreign-authority entities

Hierarchy inference MUST NOT mint a container entity, a membership triple, or an inverse sibling edge for an entity
whose positions 1–2 differ from the deployment's authority, on any lane including a declared import lane. The
framework never mints or mutates under a foreign authority; the imported entity is persisted without hierarchy
triples and no warning is logged for the skip.

#### Scenario: an imported entity receives no hierarchy triples

- **GIVEN** a deployment with authority `acme`/`dep1`, `enable_hierarchy: true`, and an import lane
- **WHEN** `acme.dep2.src.git.commit.a1` arrives on the import lane
- **THEN** no `acme.dep2.src.git.commit.group` or other container entity is created
- **AND** the persisted entity carries no `hierarchy.*` triple
- **AND** the test that verifies this is `TestHierarchySkipsForeignAuthority`

### Requirement: Framework-minted runtime state carries the deployment's own authority

Every framework component that mints runtime state in reaction to an entity — including the rule engine's
`publish_agent` with `run_scope=new` — MUST take `org` and `platform` from `deps.Platform` and MUST NOT read them
back from the firing or triggering entity's ID. The firing entity MUST remain referenced as provenance (parent or
related-loop linkage), never as the minting authority.

#### Scenario: a rule firing on an imported entity mints local runtime state locally

- **GIVEN** a deployment with authority `acme`/`dep1` and an imported entity `foreign.dep9.agentic-loop.agent.execution.<uuid>` (a peer deployment's own loop
  execution — `run_scope=new` requires the loop-execution family)
- **WHEN** a rule with `run_scope=new` fires on it
- **THEN** the stamped run entity ID begins with `acme.dep1.`
- **AND** the imported entity is referenced as the run's parent
- **AND** the run anchor written to the imported firing entity follows owner item O-12 (rejected and logged until
  ruled)
- **AND** the test that verifies this is `TestRunScopeNewMintsUnderDeploymentAuthority`
