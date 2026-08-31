## MODIFIED Requirements

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
- **AND** the loop carries `agent.loop.run` = `<uuid>` and `agent.run.entity-id` = the run entity the mint
  returned, not a value recomputed from `<uuid>`
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
  (the scenario title is retained because openspec refuses renames in a MODIFIED block; the refusal it once named
  is no longer reachable, since the instance is a digest of the full origin)
- **AND** an empty requested origin is refused, and a stored run whose origin differs from the requested one is
  refused with an error that names neither identity
- **AND** the tests that verify this are `TestMint_TwoOriginsAtOneInstanceMintDistinctRuns`,
  `TestMint_RefusesEmptyOrigin` and `TestMint_StoredOriginMismatchIsRefusedWithoutNamingIt`

#### Scenario: the run entity is carried, never recomputed

- **GIVEN** a loop spawned into a run
- **WHEN** its `LoopCreatedEvent`, `LoopCompletedEvent`, tool-call metadata and `agent.run.entity-id` triple are
  produced
- **THEN** each carries the run entity ID exactly as `Mint` returned it
- **AND** no production code path composes a `chain.agent.execution` identity from a bare `RunID`
- **AND** the tests that verify this are `TestRunEntityIDIsCarriedOnEveryLoopSurface` and
  `TestAuditFlagsDerivedFamilyComposedOutsideItsHome`
