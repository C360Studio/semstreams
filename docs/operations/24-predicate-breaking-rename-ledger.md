# Predicate Contract Breaking Rename Ledger

**Status:** Cutover review artifact. Reviewed against the SemStreams enforcement diff on 2026-07-14 and the
SemConnect owner cutover inventory on 2026-07-17.

This ledger records the predicate identity changes required by ADR-074. It is release documentation, not a
runtime alias table. No binary, configuration loader, query path, or migration tool may load this file to accept
an old identity. Incompatible beta graph state is wiped, the framework is restarted, and canonical owned sources are
reseeded as described in
[`17-predicate-cutover-clean-wipe.md`](17-predicate-cutover-clean-wipe.md).

## Review Rules

- Every producer, rule condition, rule action, substitution, schema default, exact query, and owned sister-repo
  consumer moves to the new identity in the same breaking release.
- A rename changes graph identity. It is not spelling normalization at read time.
- Namespace queries use the new three-part identity. No query searches both names.
- Test-only fixture normalization is recorded separately so it is not mistaken for a product migration.
- Generated substitutions such as `$entity.triple.<predicate>` follow the base predicate rename and are not
  separate predicate identities.

## Framework and Graph Predicates

| Previous identity | Canonical identity |
|---|---|
| `core.identity.referenced_by` | `core.identity.referenced-by` |
| `core.identity.stub_owner` | `core.identity.stub-owner` |
| `graph.rel.blocked_by` | `graph.rel.blocked-by` |
| `graph.rel.depends_on` | `graph.rel.depends-on` |
| `graph.rel.related_to` | `graph.rel.related-to` |
| `graph.rel.triggered_by` | `graph.rel.triggered-by` |
| `network.traffic.bytes.in` | `network.traffic.bytes-in` |
| `network.traffic.bytes.out` | `network.traffic.bytes-out` |
| `network.traffic.packets.in` | `network.traffic.packets-in` |
| `network.traffic.packets.out` | `network.traffic.packets-out` |
| `inferred.clustered_with` | `inferred.cluster.clustered-with` |
| `inferred.related_to` | `inferred.semantic.related-to` |
| `graph.community.member_of` | `graph.community.member-of` |
| `governance.injection.top_match_id` | `governance.injection.top-match-id` |
| `rule.spawned_task` | `rule.task.spawned` |

## Agentic Vocabulary and Runtime Predicates

| Previous identity | Canonical identity |
|---|---|
| `agent.action.executed_by` | `agent.action.executed-by` |
| `agent.delegation.valid_from` | `agent.delegation.valid-from` |
| `agent.delegation.valid_until` | `agent.delegation.valid-until` |
| `agent.execution.rate_limit` | `agent.execution.rate-limit` |
| `agent.identity.display_name` | `agent.identity.display-name` |
| `agent.loop.cost_usd` | `agent.loop.cost-usd` |
| `agent.loop.ended_at` | `agent.loop.ended-at` |
| `agent.loop.fetched_web` | `agent.loop.fetched-web` |
| `agent.loop.has_step` | `agent.loop.has-step` |
| `agent.loop.model_used` | `agent.loop.model-used` |
| `agent.loop.observed_web` | `agent.loop.observed-web` |
| `agent.loop.reply_to` | `agent.loop.reply-to` |
| `agent.loop.tokens_in` | `agent.loop.tokens-in` |
| `agent.loop.tokens_out` | `agent.loop.tokens-out` |
| `agent.loop.workflow_step` | `agent.loop.workflow-step` |
| `agent.model.endpoint_url` | `agent.model.endpoint-url` |
| `agent.model.input_price` | `agent.model.input-price` |
| `agent.model.max_tokens` | `agent.model.max-tokens` |
| `agent.model.output_price` | `agent.model.output-price` |
| `agent.model.rate_limit` | `agent.model.rate-limit` |
| `agent.model.supports_tools` | `agent.model.supports-tools` |
| `agent.run` | `agent.loop.run` |
| `agent.run.entity_id` | `agent.run.entity-id` |
| `agent.run.last_transition_at` | `agent.run.last-transition-at` |
| `agent.run.last_transition_from` | `agent.run.last-transition-from` |
| `agent.run.last_transition_note` | `agent.run.last-transition-note` |
| `agent.run.last_transition_source` | `agent.run.last-transition-source` |
| `agent.run.parent_entity_id` | `agent.run.parent-entity-id` |
| `agent.scratch.created_at` | `agent.scratch.created-at` |
| `agent.step.duration_ms` | `agent.step.duration-ms` |
| `agent.step.error_category` | `agent.step.error-category` |
| `agent.step.error_message` | `agent.step.error-message` |
| `agent.step.tokens_evicted` | `agent.step.tokens-evicted` |
| `agent.step.tokens_in` | `agent.step.tokens-in` |
| `agent.step.tokens_out` | `agent.step.tokens-out` |
| `agent.step.tokens_summarized` | `agent.step.tokens-summarized` |
| `agent.step.tool_name` | `agent.step.tool-name` |
| `agent.step.tool_status` | `agent.step.tool-status` |
| `agent.todo.updated_at` | `agent.todo.updated-at` |
| `agent.web.content_type` | `agent.web.content-type` |
| `agent.web.fetched_at` | `agent.web.fetched-at` |
| `agent.web.fetched_by` | `agent.web.fetched-by` |
| `agent.web.observed_at` | `agent.web.observed-at` |
| `agent.web.observed_by` | `agent.web.observed-by` |
| `agent.web.source_query` | `agent.web.source-query` |
| `agent.web.status_code` | `agent.web.status-code` |
| `coordinator.decision.next_action` | `coordinator.decision.next-action` |
| `coordinator.decision.sap_coerced` | `coordinator.decision.sap-coerced` |
| `ops.config.cost_per_task` | `ops.config.cost-per-task` |
| `ops.config.p95_latency` | `ops.config.p95-latency` |
| `ops.diagnosis.observed_role` | `ops.diagnosis.observed-role` |

The former `agent.capability.oasf_class` has no canonical SemStreams replacement. Its briefly renamed
`agent.capability.oasf-class` form was also removed by ADR-075's clean transfer of OASF taxonomy and projection
ownership to SemTeams; see the [framework package boundary inventory](27-framework-package-boundary-clean-break.md).

Related-loop lineage is a semantic migration, not a general textual alias. Genuine sibling-role lineage uses
exactly `agent.lineage.<role-key>`, where the role key is one static lower-kebab segment of at most 64 bytes. Historical
run anchors move to the already-declared `agent.loop.run` and `agent.run.entity-id` predicates instead of being
reminted beneath `agent.lineage`. The former untyped `lineage.*` namespace has no alias or dual-read path.

## Research Runtime and Reference Rules

| Previous identity | Canonical identity |
|---|---|
| `loop.role` | `agent.loop.role` |
| `research.budget_tokens` | `research.request.budget-tokens` |
| `research.classify.candidate_count` | `research.classify.candidate-count` |
| `research.execute.evidence_count` | `research.execute.evidence-count` |
| `research.has_evidence` | `research.evidence.present` |
| `research.hint` | `research.request.hint` |
| `research.loop_id` | `research.loop.id` |
| `research.max_iterations` | `research.request.max-iterations` |
| `research.parent_loop` | `research.parent.loop` |
| `research.parent_role` | `research.parent.role` |
| `research.requested` | `research.request.received` |
| `research.search_result.complete` | `research.search-result.complete` |
| `research.search_result.ref` | `research.search-result.ref` |
| `research.status` | `research.state.status` |
| `research.topic` | `research.request.topic` |

The research rule pack also changes subjects and substitutions containing these identities. For example,
`component.nl_classify.$entity.triple.research.loop_id` becomes
`component.nl_classify.$entity.triple.research.loop.id`. Those strings are consumers of `research.loop.id`, not
additional stored predicate identities.

## Workflow, Mission, and Reference Configuration

| Previous identity | Canonical identity |
|---|---|
| `agentic.tool.file_operation` | `agentic.tool.file-operation` |
| `alert.active` | `alert.state.active` |
| `gather.completed_child` | `gather.child.completed` |
| `workflow.phase` | `workflow.state.phase` |
| `workflow.status` | `workflow.state.status` |
| `mission.command` | `mission.command.requested` |
| `mission.last_transition_at` | `mission.transition.at` |
| `mission.last_transition_from` | `mission.transition.from` |
| `mission.last_transition_note` | `mission.transition.note` |
| `mission.last_transition_source` | `mission.transition.source` |
| `mission.note` | `mission.state.note` |
| `mission.owner_org_id` | `mission.owner.org-id` |
| `mission.phase` | `mission.state.phase` |
| `entity.type` | `entity.identity.type` |

The `mission.*` rows above are the production e2e mission contract. Lifecycle package tests use deliberately
different example semantics (`mission.lifecycle.phase`, `mission.identity.owner-org-id`,
`mission.annotation.note`, and `mission.assignment.drone`). Those are fixture vocabulary, not aliases for the e2e
mission contract.

## Gated DAG Defaults

| Previous identity | Canonical identity |
|---|---|
| `gateddag.claim` | `gateddag.unit.claim` |
| `gateddag.completed` | `gateddag.unit.completed` |
| `gateddag.depends_on` | `gateddag.unit.depends-on` |
| `gateddag.dirtied` | `gateddag.unit.dirtied` |
| `gateddag.failed` | `gateddag.unit.failed` |

## OMS, SensorML, and Connected Systems API

| Previous identity | Canonical identity |
|---|---|
| `oms.observation.hasFeatureOfInterest` | `oms.observation.has-feature-of-interest` |
| `oms.observation.hasSimpleResult` | `oms.observation.has-simple-result` |
| `oms.observation.observedProperty` | `oms.observation.observed-property` |
| `oms.observation.phenomenonTime` | `oms.observation.phenomenon-time` |
| `oms.observation.resultTime` | `oms.observation.result-time` |
| `oms.observation.usedProcedure` | `oms.observation.used-procedure` |
| `sensorml.component.isHostedBy` | `sensorml.component.is-hosted-by` |
| `sensorml.process.attachedTo` | `sensorml.process.attached-to` |
| `sensorml.process.hasSubSystem` | `sensorml.process.has-sub-system` |
| `sensorml.process.usedProcedure` | `sensorml.process.used-procedure` |
| `csapi.command.partOfControlStream` | `csapi.command.part-of-control-stream` |
| `csapi.controlstream.commandSchema` | `csapi.controlstream.command-schema` |
| `csapi.controlstream.controlsSystem` | `csapi.controlstream.controls-system` |
| `csapi.datastream.phenomenonTimeRange` | `csapi.datastream.phenomenon-time-range` |
| `csapi.datastream.producedBy` | `csapi.datastream.produced-by` |
| `csapi.datastream.resultSchema` | `csapi.datastream.result-schema` |
| `csapi.datastream.resultTimeRange` | `csapi.datastream.result-time-range` |
| `csapi.datastream.resultType` | `csapi.datastream.result-type` |
| `csapi.systemevent.forSystem` | `csapi.systemevent.for-system` |

These standard-facing local names are SemStreams predicate identities. Their external RDF/JSON-LD IRI mappings
remain a separate vocabulary-registration concern and must be regression tested after the rename.

## SemConnect-Owned Connected Systems Predicates

These mappings were discovered in the SemConnect owner inventory after review of the SemStreams enforcement diff.
They are release mappings for SemConnect's gateway-local vocabulary, not aliases or a transfer of runtime ownership
back to SemStreams.

| Previous identity | Canonical identity |
|---|---|
| `cs-api.deployment.deployedSystems` | `cs-api.deployment.deployed-systems` |
| `cs-api.samplingfeature.hostedProcedure` | `cs-api.samplingfeature.hosted-procedure` |

SemConnect must update its producers, registrations, exact queries, RDF/JSON-LD mappings, fixtures, and reseed source
in the same coordinated release. The old mixed-case properties fail the canonical lower-kebab parser and have no
dual-read path.

## Examples and Documentation Vocabulary

| Previous identity | Canonical identity |
|---|---|
| `maintenance.work.completion_date` | `maintenance.work.completion-date` |
| `observation.record.observed_at` | `observation.record.observed-at` |
| `rdf.type` | `rdf.type.class` |

## Contract Restrictions That Are Not Renames

Two changes in the enforcement diff cannot be represented honestly as one-to-one renames:

1. The IoT sensor example no longer derives `sensor.measurement.<unit>` from arbitrary input. It accepts only
   `celsius`, `fahrenheit`, `percent`, and `hpa`, each mapped to its existing declared predicate. Unsupported units
   are rejected. This narrows the producer contract without renaming the supported identities.
2. The inference default set `member_of`, `part_of`, `located_in`, and `belongs_to` is replaced as a set by
   `hierarchy.type.member`, `hierarchy.system.member`, `hierarchy.domain.member`, and `graph.rel.contains`.
   There is no reviewed one-to-one semantic mapping between the old and new positions. Treat the old custom
   configuration values as removed and select a canonical predicate based on intended semantics.

## Test-Fixture Normalization Seen in the Diff

These values occur only in tests, smoke fixtures, or example assertions in the reviewed diff. They are listed to
make the diff inventory complete, but they are not product migration promises.

| Previous fixture | Canonical fixture |
|---|---|
| `child.isHostedBy` | `test.edge.is-hosted-by` |
| `claimed.edge` | `test.edge.claimed` |
| `coh.added` | `test.coherence.added` |
| `coh.marker` | `test.coherence.marker` |
| `empty.subject.files.onto.primary` | `test.subject.empty-primary` |
| `evidence.note` | `evidence.note.value` |
| `flock.neighbor` | `flock.relation.neighbor` |
| `foreign.b` | `test.foreign.b` |
| `foreign.d` | `test.foreign.d` |
| `harness.phase` | `lifecycle.state.phase` |
| `inferred.parent` | `inferred.hierarchy.parent` |
| `merge.kind` | `test.merge.kind` |
| `mission.assigned_drone` | `mission.assignment.drone` |
| `mission.command` | `mission.control.command` |
| `mission.note` | `mission.annotation.note` |
| `mission.owner_org_id` | `mission.identity.owner-org-id` |
| `mission.phase` | `mission.lifecycle.phase` |
| `order.seq` | `order.sequence.value` |
| `own.a` | `test.own.a` |
| `own.c` | `test.own.c` |
| `own.p` | `test.own.fact` |
| `p` | `test.edge.p` |
| `phaseonly.phase` | `phaseonly.lifecycle.phase` |
| `pred` | `test.value.predicate` |
| `real.label` | `entity.identity.label` |
| `rel.hosts` | `graph.rel.hosts` |
| `rel.isHostedBy` | `graph.rel.is-hosted-by` |
| `robotics.battery.batteryId` | `robotics.battery.battery-id` |
| `robotics.battery.systemId` | `robotics.battery.system-id` |
| `robotics.command` | `robotics.command.requested` |
| `robotics.description` | `robotics.asset.description` |
| `robotics.drone.systemId` | `robotics.drone.system-id` |
| `robotics.status` | `robotics.state.status` |
| `sensorml.label` | `sensorml.process.label` |
| `sensorml.uid` | `sensorml.process.uid` |
| `skos.core.prefLabel` | `skos.core.pref-label` |
| `smoke.observation.observedProperty` | `smoke.observation.observed-property` |
| `smoke.observation.resultTime` | `smoke.observation.result-time` |
| `smoke.platform.hasDeployment` | `smoke.platform.has-deployment` |
| `smoke.sensor.madeObservation` | `smoke.sensor.made-observation` |
| `some.predicate` | `some.test.predicate` |
| `system.label` | `test.system.label` |
| `system.note` | `test.system.note` |
| `system.uid` | `test.system.uid` |
| `test.added` | `test.entity.added` |
| `test.attr` | `test.entity.attribute` |
| `test.creator` | `test.entity.creator` |
| `test.drop` | `test.entity.drop` |
| `test.field` | `test.entity.field` |
| `test.intermediate` | `test.entity.intermediate` |
| `test.keep` | `test.entity.keep` |
| `test.kind` | `test.entity.kind` |
| `test.marker` | `test.entity.marker` |
| `test.member` | `test.entity.member` |
| `test.newlines` | `test.value.newlines` |
| `test.predicate` | `test.entity.predicate` |
| `test.property.with.dots` | `test.property.quoted-value` |
| `test.status` | `test.entity.status` |
| `test.unicode` | `test.value.unicode` |
| `test.zero_rev.added` | `test.zero-rev.added` |
| `unclaimed.edge` | `test.edge.unclaimed` |
| `wire.status` | `wire.state.status` |
| `x.edge` | `test.edge.unclaimed` |
| `x.phase` | `workflow.lifecycle.phase` |

## Release Review Findings

- The e2e mission contract and lifecycle test vocabulary intentionally map the same old strings to different new
  identities. Release notes must present only the production e2e mapping; the fixture mapping must not become a
  compatibility promise.
- The inference default-set replacement needs semantic owner sign-off because it is not a mechanical rename.
- All exact-query consumers and RDF/JSON-LD mappings must be tested with the canonical identities before release.
- Sister-repository occurrences consume this ledger and may not invent a third identity for an existing contract.
  An owner-approved mapping absent from the initial framework diff is added as an explicit owner-scoped section, as
  with the SemConnect rows above; it never becomes a runtime alias.
