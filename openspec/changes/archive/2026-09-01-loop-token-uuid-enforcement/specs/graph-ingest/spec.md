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

`agentrun.Mint` MUST refuse a firing-loop instance token that is not in canonical UUID form before constructing
the run's entity ID, MUST refuse an empty origin, and on an already-exists result MUST compare the STORED
`agent.run.origin-entity-id` with the requested one and refuse a mismatch with a classified error, using the record
that path already fetches. The run entity ID derives from the loop's instance segment alone, so two loops that
different deployments name with the same instance derive one local run ID; the loop-token UUID contract
(entity-id-contract) makes an accidental shared instance a collision-math impossibility, and the origin comparison
remains the loud backstop for a copied or replayed token. A stored run carrying no origin is refused, not adopted
— an empty value cannot establish that the stored run is this caller's.

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

- **GIVEN** a deployment `acme`/`ops` and two imported loops with distinct authorities and the same canonical UUID
  instance segment — `peerone.dep1.agentic-loop.agent.execution.7c9e6679-7425-40de-944b-e07fc1f90ae7` and
  `peertwo.dep9.agentic-loop.agent.execution.7c9e6679-7425-40de-944b-e07fc1f90ae7`
- **WHEN** a run is minted from the first and then from the second
- **THEN** the first mint succeeds carrying its own origin, and the second is REFUSED with a classified invalid error
  rather than returning the first origin's run
- **AND** the first run's stored origin is unchanged
- **AND** an empty requested origin, and a stored run carrying no origin, are refused the same way
- **AND** the tests that verify this are `TestMint_TwoOriginsAtOneInstanceAreRefusedNotAliased`,
  `TestMint_RefusesEmptyOrigin` and `TestMint_LegacyOriginlessStoredRunIsRefused`

#### Scenario: a non-UUID firing-loop instance is refused before the run entity is built

- **GIVEN** a firing loop entity whose instance segment is `workflow-7`
- **WHEN** `agentrun.Mint` is called with that instance as the root loop ID
- **THEN** it returns a classified invalid error naming the loop-token contract, before any entity ID is
  constructed and before any store call
- **AND** the dispatch degrades as it does for any Mint failure: the task spawns without a run association and the
  failure is logged as an error
- **AND** the test that verifies this is `TestMint_NonUUIDRootLoopIDIsRefused`
