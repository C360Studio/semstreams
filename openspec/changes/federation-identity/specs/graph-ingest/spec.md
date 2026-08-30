## MODIFIED Requirements

### Requirement: Every graph boundary enforces the deployment's own authority unless the write arrives on a declared import lane

graph-ingest MUST read the deployment authority from `deps.Platform` at construction and MUST validate positions 1–2
of every final candidate **subject** entity ID through `pkg/types.ValidateEntityIDAuthority` on every lane —
Graphable fact arrival, every `graph.mutation.>` operation, and direct persistence — before any KV I/O and after
structural validation. `@id` objects MUST NOT be authority-checked; they keep canonical structural validation; no stub is created and an absent object is permitted
(the unmodified requirement "Relationship target absence creates no entity"), so a local subject may reference an
imported entity. An import is a read-only mirror: a mutation from any
non-import lane whose subject is a foreign-authority entity MUST be rejected with
`foreign_authority`, and every local fact about an imported entity MUST live on a local subject that references it. On a lane whose input port is not declared `"import": true`, a candidate whose
positions 1–2 differ from the deployment's MUST be rejected. On a declared import lane, a candidate whose positions
1–2 equal the deployment's MUST be rejected, and a foreign candidate MUST be persisted with its identity bytes
unchanged. Each rejection MUST be metered exactly once as `mutation_rejections{reason="authority_foreign"}` or
`{reason="authority_claimed"}` and MUST emit a loud log naming the lane and the segment index, never the identity.
This holds on the direct in-process lane too: it carries no NATS subject, so it names the lane rather than omitting
the record — `direct` in the metric's `subject` label, which the other lanes fill with their arrival subject, and in
the log's own `arrival` attribute.
No configuration MAY disable the check. The import declaration and the envelope `source` string are the only
provenance this requirement records; it authenticates nothing.

**An import is admitted under exactly one lane (ADR-102 decision 4).** When a foreign candidate is born through an
import lane, graph-ingest MUST stamp the mirror with the framework-owned birth predicate `entity.import.lane` whose
object is the arrival port's declared name, and MUST strip any `entity.import.lane` triple an arrival carries
before merging. A foreign candidate arriving on an import lane whose name differs from the stored
`entity.import.lane` MUST be rejected inside the same compare-and-swap closure that reads the resident state, with
code `entity_id_authority_invalid` and reason `import_collision`, metered exactly once as
`mutation_rejections{reason="authority_collision"}`, terminated (never redelivered), and logged naming both lane
names and never the identity. A re-arrival on the admitting lane merges as today. The mutation and direct lanes
carry no import declaration and are unaffected.

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

#### Scenario: a direct in-process rejection is metered and logged like any other

- **GIVEN** the same deployment and a foreign-authority entity ID
- **WHEN** it is used as the subject of an in-process `CreateEntity`, `MergeEntity`, hierarchy inverse-edge append,
  batch append, or revision-checked delete — no NATS request, no arrival subject
- **THEN** the caller receives the coded authority error AND
  `mutation_rejections{subject="direct",reason="authority_foreign"}` increments exactly once
- **AND** exactly one WARN is emitted for that call, naming the lane and the segment index and never the identity
- **AND** the test that verifies this is `TestAuthorityGateMetersDirectPersistenceRejectionsOnEveryDirectSeam`

#### Scenario: a foreign ID admitted through one import lane is refused on another

- **GIVEN** the same deployment with two import lanes named `peer_a` and `peer_b`
- **AND** `acme.dep2.src.git.commit.a1` was born through `peer_a` and carries `entity.import.lane` = `peer_a`
- **WHEN** a candidate with the same ID arrives on `peer_b`
- **THEN** it is rejected with reason `import_collision` inside the CAS closure, the resident revision is unchanged,
  the message is terminated, and `mutation_rejections{reason="authority_collision"}` increments exactly once
- **AND** the WARN names `peer_b` and `peer_a` and never the identity
- **AND** the test that verifies this is `TestImportCollisionRejectsSecondLane`

#### Scenario: the admitting lane re-admits its own mirror and a carried lane triple is stripped

- **GIVEN** the mirror above and a re-arrival on `peer_a` carrying its own `entity.import.lane` = `evil`
- **WHEN** the arrival is merged
- **THEN** the stored `entity.import.lane` is still `peer_a` and the merge applies the arrival's other predicates
- **AND** the test that verifies this is `TestImportLaneTripleIsFrameworkOwned`

### Requirement: Framework-minted runtime state carries the deployment's own authority and never writes to an imported firing entity

Every framework component that mints runtime state in reaction to an entity — including the rule engine's
`publish_agent` with `run_scope=new` — MUST take `org` and `platform` from `deps.Platform` and MUST NOT read them
back from the firing or triggering entity's ID. A component that receives no deployment authority MUST refuse to
construct rather than mint under an empty or foreign pair. The local run entity MUST carry
`agent.run.origin-entity-id` — a birth predicate naming the firing loop, whose object is that loop's canonical entity
ID — for every run, so the run→loop linkage has one home that never depends on writing the loop.

Every rule-engine constructor that can hold the triple mutator — the capability both framework writes below require
— MUST take the deployment authority as a constructor parameter rather than a setter, including the exported
convenience constructors, because the caller of an exported constructor is an adopter outside this repository who is
in no review here. Where the authority is nonetheless absent, the foreign-vs-local decision MUST fail CLOSED: an
executor holding no pair cannot establish that any entity is local, so every firing entity reads as foreign and every
framework write to it is skipped, counted and logged. A decision that answers "local" for an unknown authority
retires the guard rather than tightening it.

`agentrun.Mint(ctx, mgr, org, platform, originEntityID)` MUST refuse an empty origin and MUST derive the run
entity's instance through the agent-run identity family from the origin's full canonical ID
(`entity-id-contract`), so two origins never share a run. On an already-exists result it MUST compare the STORED
`agent.run.origin-entity-id` with the requested one and refuse a mismatch with a classified error that names the
conflict and neither identity. The run entity ID MUST be carried — as `RunEntityID` on the spawned `TaskMessage`,
the loop's `AGENT_LOOPS` record, the four loop events, tool metadata `agent.run_entity_id`, and the loop's
`agent.run.entity-id` triple — and MUST NOT be recomputed by any consumer; `RunID` keeps naming the root loop's
bare identifier and its `AGENT_LOOPS` record. A `TaskMessage` carrying `RunID` without `RunEntityID` MUST fail
validation.

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
skipped.

#### Scenario: a rule firing on an imported loop links the local run without writing to the import

- **GIVEN** a deployment with authority `acme`/`dep1` and an imported entity `foreign.dep9.agentic-loop.agent.execution.<uuid>` (a peer deployment's own loop
  execution — `run_scope=new` requires the loop-execution family)
- **WHEN** a rule with `run_scope=new` fires on it
- **THEN** a run entity `acme.dep1.chain.agent.execution.<64 hex>` is minted carrying `agent.run.origin-entity-id` =
  the imported entity, and the spawned task carries `RunID` = `<uuid>` and `RunEntityID` = that run entity
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
- **AND** those 3 increments describe ONE declined entity, not three: all 3 dispatched tasks carry the same `run_id`
  and the same `run_entity_id`, both derived from the firing entity, so the firing entity is invariant across the
  fan-out and the counter MUST NOT be read as a count of distinct peer entities
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
- **AND** the loop carries `agent.loop.run` = `<uuid>` and `agent.run.entity-id` = the run entity the mint returned,
  not a value recomputed from `<uuid>`
- **AND** the test that verifies this is `TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin`

#### Scenario: an executor built through an exported constructor cannot silently disable the guard

- **GIVEN** an `ActionExecutor` built through the exported `NewActionExecutorFull`, which holds both a mutator and a
  publisher and can therefore reach both framework writes
- **WHEN** it is constructed with the deployment's authority and a `publish_agent` action fires on a foreign entity
- **THEN** `rule.task.spawned` is not written and the skip is counted once
- **AND WHEN** it is constructed with an EMPTY authority and the same action fires on a LOCAL entity
- **THEN** the write is still skipped and counted — the unknown authority fails closed, it does not read as local
- **AND** the test that verifies this is
  `TestPublishAgentThroughExportedFullConstructorSkipsForeignSpawnedTask`

#### Scenario: two origins at one instance segment get a refusal, not each other's run

- **GIVEN** a deployment `acme`/`ops` and two imported loops with distinct authorities and the same instance
  segment — `peerone.dep1.agentic-loop.agent.execution.a1b2c3d4` and
  `peertwo.dep9.agentic-loop.agent.execution.a1b2c3d4`
- **WHEN** a run is minted from the first and then from the second
- **THEN** two DISTINCT run entities exist, each carrying its own origin, and neither mint returned the other's run
  (the scenario title is retained because openspec refuses renames in a MODIFIED block; the refusal it once named is
  no longer reachable, since the instance is a digest of the full origin)
- **AND** an empty requested origin is refused, and a stored run whose origin differs from the requested one is
  refused with an error that names neither identity
- **AND** the tests that verify this are `TestMint_TwoOriginsAtOneInstanceMintDistinctRuns`,
  `TestMint_RefusesEmptyOrigin` and `TestMint_StoredOriginMismatchIsRefusedWithoutNamingIt`

#### Scenario: the run entity is carried, never recomputed

- **GIVEN** a loop spawned into a run
- **WHEN** its `LoopCreatedEvent`, `LoopCompletedEvent`, tool-call metadata and `agent.run.entity-id` triple are produced
- **THEN** each carries the run entity ID exactly as `Mint` returned it
- **AND** no production code path composes a `chain.agent.execution` identity from a bare `RunID`
- **AND** the tests that verify this are `TestRunEntityIDIsCarriedOnEveryLoopSurface` and
  `TestAuditFlagsDerivedFamilyComposedOutsideItsHome`
