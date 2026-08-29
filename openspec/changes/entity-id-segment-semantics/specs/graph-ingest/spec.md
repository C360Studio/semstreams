## ADDED Requirements

### Requirement: Every graph boundary enforces the deployment's own authority unless the write arrives on a declared import lane

graph-ingest MUST read the deployment authority from `deps.Platform` at construction and MUST validate positions 1–2
of every final candidate **subject** entity ID through `pkg/types.ValidateEntityIDAuthority` on every lane —
Graphable fact arrival, every `graph.mutation.>` operation, and direct persistence — before any KV I/O and after
structural validation. `@id` objects MUST NOT be authority-checked; they keep canonical structural validation; no stub is created and an absent object is permitted
(the unmodified requirement "Relationship target absence creates no entity"), so a local subject may reference an
imported entity. An import is a read-only mirror: a mutation from any
non-import lane whose subject is an already-persisted foreign-authority entity MUST be rejected with
`foreign_authority`, and every local fact about an imported entity MUST live on a local subject that references it. On a lane whose input port is not declared `"import": true`, a candidate whose
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

#### Scenario: a local lane cannot reconcile an imported entity

- **GIVEN** an imported entity `acme.dep2.src.git.commit.a1` persisted through an import lane
- **WHEN** an `entity.reconcile` request from a non-import lane names it as subject
- **THEN** the request is rejected with code `entity_id_authority_invalid` and reason `foreign_authority`
- **AND** the imported entity's revision is unchanged
- **AND** the test that verifies this is `TestAuthorityGateRejectsReconcileOfImportedSubject`

#### Scenario: the refusal is decided from the identity alone, never from the entity's stored state

- **GIVEN** a foreign-authority entity ID `acme.dep2.src.git.commit.zz` that was never persisted
- **WHEN** an `entity.reconcile` request from a non-import lane names it as subject
- **THEN** the request is rejected with code `entity_id_authority_invalid` and reason `foreign_authority`, NOT with
  `entity_not_found` — the verdict does not depend on whether the entity exists
- **AND** the identical request against a never-persisted LOCAL id `acme.dep1.src.git.commit.zz` IS rejected with
  `entity_not_found`, so absence IS reachable and IS reported on this lane, and the foreign answer is not
  "no entity here" under another code
- **AND** the test that verifies this is `TestAuthorityGateRefusesForeignReconcileRegardlessOfExistence`

#### Scenario: a local lane cannot delete an imported entity

- **GIVEN** the same imported entity
- **WHEN** an `entity.delete` request from a non-import lane names it as subject, at its current revision
- **THEN** the request is rejected with code `entity_id_authority_invalid` and reason `foreign_authority`
- **AND** the entity still exists at that revision — a mirror is not local property to reclaim
- **AND** the test that verifies this is `TestAuthorityGateRejectsDeleteOfImportedSubject`

#### Scenario: a local lane cannot annotate an imported entity

- **GIVEN** the same imported entity `acme.dep2.src.git.commit.a1`
- **WHEN** a `triple.append` request from a non-import lane targets it as subject
- **THEN** the request is rejected with code `entity_id_authority_invalid` and reason `foreign_authority`
- **AND** the imported entity's revision is unchanged
- **AND** the test that verifies this is `TestAuthorityGateRejectsAnnotationOfImportedSubject`

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

### Requirement: Framework-minted runtime state carries the deployment's own authority and never writes to an imported firing entity

Every framework component that mints runtime state in reaction to an entity — including the rule engine's
`publish_agent` with `run_scope=new` — MUST take `org` and `platform` from `deps.Platform` and MUST NOT read them
back from the firing or triggering entity's ID. A component that receives no deployment authority MUST refuse to
construct rather than mint under an empty or foreign pair. The local run entity MUST carry
`agent.run.origin-entity-id` — a birth predicate naming the firing loop, whose object is that loop's canonical entity
ID — for every run, so the run→loop linkage has one home that never depends on writing the loop.

**No framework write reaches a foreign firing entity.** The run-anchor pair (`agent.loop.run`,
`agent.run.entity-id`) and the `rule.task.spawned` back-reference MUST be written on the firing entity only when it
carries the deployment's own authority. When the firing entity is a foreign-authority import the rule action MUST
detect that before any write and MUST issue no mutation request targeting the foreign subject — not even one
graph-ingest would reject. The decision MUST be recorded once per DISPATCH as
`rule_foreign_firing_writes_skipped_total{reason="foreign_authority"}` with ONE Info log per dispatch naming EVERY
write that dispatch skipped, not only the writes decided at the first skip point: a counted skip, never a rejection,
and one increment for the whole dispatch rather than one per omitted write. The counted unit is one `publish_agent` dispatch — one (firing entity, `for_each` item) pair — and MUST NOT be
read as one distinct declined entity: `publish_agent` fans out over `for_each` with the firing entity held constant,
so an action fanning out over N items on a single imported firing entity MUST report N skips for that ONE entity.
The counter is named for the writes it covers rather than for the run anchor, because
under `run_scope` `inherit` or `none` no anchor is in play and only the `rule.task.spawned` back-reference is
skipped. Issue #1096 is complete only when this path is implemented and tested.

#### Scenario: a rule firing on an imported loop links the local run without writing to the import

- **GIVEN** a deployment with authority `acme`/`dep1` and an imported entity `foreign.dep9.agentic-loop.agent.execution.<uuid>` (a peer deployment's own loop
  execution — `run_scope=new` requires the loop-execution family)
- **WHEN** a rule with `run_scope=new` fires on it
- **THEN** a run entity `acme.dep1.chain.agent.execution.<uuid>` is minted carrying `agent.run.origin-entity-id` =
  the imported entity
- **AND** no mutation request targets `foreign.dep9.agentic-loop.agent.execution.<uuid>` and its revision is unchanged
- **AND** no mutation request carries `rule.task.spawned` for that subject either
- **AND** `rule_foreign_firing_writes_skipped_total{reason="foreign_authority"}` increments exactly once and
  `mutation_rejections` does not
- **AND** the test that verifies this is `TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite`

#### Scenario: the skip counter counts dispatches, so a for_each fan-out on one import reports N

- **GIVEN** the same deployment and the same single imported loop
  `foreign.dep9.agentic-loop.agent.execution.<uuid>`
- **WHEN** one `publish_agent` action with `run_scope=new` fans out over a `for_each` list of 3 items on it
- **THEN** 3 tasks are dispatched, `rule_foreign_firing_writes_skipped_total{reason="foreign_authority"}` reads 3,
  and 3 Info lines are emitted — the log's unit is the counter's unit
- **AND** those 3 increments describe ONE declined entity, not three: all 3 dispatched tasks carry the same `run_id`,
  which is derived from the firing entity, so the firing entity is invariant across the fan-out and the counter MUST
  NOT be read as a count of distinct peer entities
- **AND** no mutation request targets the import and its revision is unchanged
- **AND** the test that verifies this is `TestRunScopeNewForEachOnOneImportCountsPerDispatchNotPerEntity`

#### Scenario: the Info line names every write the dispatch declined

- **GIVEN** the same deployment and the same imported loop, fired by a rule with `run_scope=new`
- **WHEN** the dispatch declines both the run-anchor pair and the `rule.task.spawned` back-reference
- **THEN** exactly one Info line is emitted for that dispatch, at level Info and with reason `foreign_authority`
- **AND** its `skipped` field names `agent.loop.run`, `agent.run.entity-id` AND `rule.task.spawned` — all three are
  declined on that one dispatch
- **AND** it names `agent.run.origin-entity-id` as where the linkage went instead, and never names the imported
  identity
- **AND** the test that verifies this is `TestForeignFiringSkipLogNamesEveryDeclinedWrite`

#### Scenario: a rule firing on a local loop stamps the anchor pair and the origin

- **GIVEN** the same deployment and a local loop `acme.dep1.agentic-loop.agent.execution.<uuid>`
- **WHEN** a rule with `run_scope=new` fires on it
- **THEN** the run entity is minted under `acme.dep1.` carrying `agent.run.origin-entity-id` = the local loop
- **AND** the loop carries `agent.loop.run` and `agent.run.entity-id`
- **AND** the test that verifies this is `TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin`
