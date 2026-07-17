## 1. Contract and Inventory

- [x] 1.1 Generate the bounded local production predicate corpus from non-test Go producers/constants, configuration
      ASTs, lifecycle/ownership/projection declarations, schemas, tools, and reference deployments. This completed
      evidence deliberately excludes `*_test.go` and `testdata`; task 1.1a owns that complementary corpus
- [x] 1.1a Generate and classify the complete local predicate corpus from tracked `*_test.go` files and structured
      artifacts beneath every `testdata` directory. Distinguish canonical positive fixtures, exact intentional
      negatives, and unrelated strings; do not treat an entire file or directory as an invalid-fixture allowance.
      Evidence: `task predicate:test-audit` passes with 1,811 candidates and 123 exact classifications
- [x] 1.2 Validate the strict lower-kebab grammar against the corpus and produce the exact breaking rename ledger
- [x] 1.3 Record the grammar, domain/domain-category delegation, and registration relationship in a new ADR
- [x] 1.4 Implement one typed parser and table/fuzz tests for valid, malformed, wildcard, Unicode, and length cases
- [ ] 1.5 Add owned sister-repository inventory commands and a coordinated zero-violation release gate

## 2. Declarative and Agent Authoring Gates

- [x] 2.1 Validate vocabulary registration and reject invalid registered constants at startup
- [x] 2.2 Parse rule condition/action predicates and substitutions through a real configuration AST audit
- [x] 2.3 Validate gated-DAG defaults, lifecycle tags, ownership/projection contracts, and schema defaults
- [x] 2.4 Generate agent/tool predicate enums or delegated-namespace constraints from declared vocabulary
- [x] 2.5 Add reference-config tests that cover direct predicates, inline rules, and generated schemas
- [x] 2.6 Replace unchecked `lineage.<role-key>` construction with an error-returning public helper for the exact
      `agent.lineage.<role-key>` namespace. Validate one lower-kebab property token through
      `vocabulary.ParsePredicate`, record the trusted `agent.lineage` authoring delegation, and retain no unchecked
      prefix helper or normalization path
- [x] 2.7 Validate `related_loops` keys and non-empty source values across every startup/hot-reload action list,
      including disabled and cron rules; reject the field on non-`publish_agent` actions. Validate the fully
      substituted TaskMessage metadata and validate the decoded intake map before `HandleTask`. Construct and
      preflight the complete prospective lineage graph batch before `HandleTask`, `WriteSpawnIdentity`, persistent
      loop creation, or graph birth; repeat batch preflight at the graph writer, propagate typed failure instead of
      log-and-ack success, and make malformed keys, non-string/empty loop IDs, invalid predicates, or invalid subjects
      reject the whole batch. Record exactly one bounded lane/reason rejection metric and no business, success, or
      publication metric/counter

## 3. Authoritative Persistence Gate

- [x] 3.1 Identify and consolidate every ENTITY_STATES create/update/CAS path behind one final-candidate commit seam
- [x] 3.2 Validate own and foreign triples after all normalization, merge, and framework injection
- [x] 3.3 Enforce the contract unconditionally with no permissive mode or compatibility escape hatch
- [x] 3.4 Emit one structured all-violations result per candidate and deduplicated bounded-label metrics
- [x] 3.5 Preserve ack/retry/quarantine semantics explicitly for malformed Graphable and mutation requests
- [x] 3.6 Cover Graphable, mutation RPC, direct adapter, inference, rule, batch, and repair write lanes
- [x] 3.7 Remove the dead `graph/datamanager` and `graph/messagemanager` parallel writer path
- [x] 3.8 Centralize framework-owned graph buckets and reject generic `update_kv` bypasses

## 4. Clean Beta Cutover

Tasks 1.5, 4.1a, 4.6b, 5.1b, and 5.7 are coordinated owned-product or final archive gates. They do not block the
reviewed SemStreams-local merge. Local tasks 4.6, 5.2, 5.3, and 5.6b remain open and must share one recorded
wipe/restart/reseed and real-NATS proof where their evidence overlaps the entity-ID change.

- [x] 4.1 Rename all SemStreams-local producers, rules, schemas, tools, exact queries, positive
      `*_test.go` fixtures, and structured `testdata`. Add the grammar-only `internal/semantictest` predicate fixture
      builder, delegate it without normalization to `vocabulary.ParsePredicate`, ban imports from production Go
      files, and do not add a graph-entity or triple factory
- [ ] 4.1a Rename every owned sister-repository producer, rule, schema, tool, exact query, positive fixture, and
      structured `testdata` artifact against the same breaking SemStreams version
- [x] 4.2 Publish the reviewed rename ledger as breaking release documentation, not a runtime alias table
- [x] 4.3 Make update/replace validate the complete candidate before any destructive removal
- [x] 4.4 Add graph-ingest and graph-index replay validation that independently blocks readiness on invalid state
- [x] 4.4a Use the shared typed decoder in spatial, temporal, embedding, clustering, rule, lifecycle, OASF, and
      direct-query ENTITY_STATES consumers; poison projection readiness and forbid partial output
- [x] 4.5 Document complete incompatible-resource wipe, restart, and canonical-source reseed with no export,
      inspection, preservation, or rollback procedure; migrate active ADR-074 and operations docs 17 and 25 so no
      authoritative operator guidance retains the former export/reset/reingest procedure
- [x] 4.6 Prove clean restart/replay and exact/namespace query fixtures after wipe/reseed. Evidence:
      `TestIntegration_PredicateCleanWipeReseedRestoresQueryParity` passes against real NATS and verifies the same
      exact and namespace results before and after a second clean restart
- [x] 4.6a Semantically migrate every SemStreams-local `lineage.*` producer, `$entity.triple.*` consumer, exact query,
      schema, and rule config: genuine sibling lineage becomes `agent.lineage.<role-key>`, while ADR-053 run anchors
      become `agent.loop.run` or `agent.run.entity-id`. Update the shipped `research_pipeline` role key to
      `research-pipeline`; migrate active ADRs/proposals, public API comments, examples, runbooks, schemas, and
      substitution guidance. Permit old spelling only in a record explicitly identified as historical; retain no
      alias, dual read/write, runtime rename table, or active unrelated-prose exception
- [ ] 4.6b Apply the same semantic lineage migration to every owned product/reference producer, consumer, exact query,
      schema, rule configuration, active document, and fixture before the v1 release/archive gate

## 5. Enforcement and Release Gates

- [x] 5.1 Run both SemStreams-local predicate audits to zero unexplained production, `*_test.go`, and
      structured-`testdata` violations. Every intentional invalid identifies one exact occurrence, value, contract
      kind, and authoritative reason; missing, stale, duplicate, broad, or reason-mismatched classifications fail the
      gate. Evidence: `task predicate:audit` passes with 467 candidates and `task predicate:test-audit` passes with
      1,811 candidates and 123 exact classifications
- [x] 5.1a Extend the source/config corpus to classify related-loop map keys, constructed lineage predicates, and
      lineage substitutions; prove every generated `agent.lineage.<role-key>` value is canonical and every remaining
      `lineage.*` occurrence is removed unless it belongs to a record explicitly identified as historical
- [ ] 5.1b Run the same production and fixture audits in every owned sister repository and reference design; require
      zero unexplained violations before the v1 release/archive gate
- [x] 5.2 Seed synthetic incompatible beta state, run the exact wipe/restart/reseed procedure, and prove expected
      query-result fixtures without exporting, inspecting, or preserving that state. Evidence:
      `TestIntegration_PredicateCleanWipeReseedRestoresQueryParity` deletes the graph-index-owned incompatible
      resource set before recreating and canonically reseeding `ENTITY_STATES`
- [x] 5.3 Seed invalid preexisting state in real NATS and prove every graph-index/query consumer stays not-ready.
      Evidence: `TestIntegration_PreexistingPredicatePoisonIsSticky`, the direct-query poison integration tests, and
      the full serialized integration gate pass the graph-index/query poison paths without partial results
- [x] 5.4 Run lint, race, schema no-drift, contract, real-NATS integration, and affected e2e suites for the first
      reviewed implementation slice. This historical evidence does not substitute for final recovery task 5.6b
- [x] 5.5 Publish breaking wipe/restart/reseed and rejection runbooks with no export, inspection, preservation, or
      rollback procedure, including corrected ADR-074 and operations docs 17 and 25
- [x] 5.6 Verify no alias, dual-read/write, permissive-mode, or in-process migration code remains
- [x] 5.6a Run the focused race and real-NATS lineage regression gates after the lineage correction; prove decoded
      validation precedes `HandleTask`, batch preflight precedes
      `WriteSpawnIdentity`, typed errors propagate, invalid config and tampered metadata have zero loop/graph/
      publication side effects, exactly one bounded rejection metric, and no business/success/publication metric
- [x] 5.6b After all recovered local changes are final, rerun lint, full race, schema no-drift, contract, and affected
      e2e gates; the focused 5.6a evidence does not substitute for this merge gate. Evidence: lint, full unit race,
      serialized real-NATS integration, schema generation with no drift, contract tests, agentic e2e, and structural
      e2e pass. The sole full-suite Docker inspect timeout passed immediately when its exact integration test reran
      in isolation
- [ ] 5.6c Before v1, close the explicit namespace-authority threat-model gap with a principal-bearing mutation
      envelope and seam-level denial of undeclared `agent.*` predicates on non-delegated graph-mutation lanes.
      Configuration-time rule/tool authoring checks are not runtime authorization, and raw NATS or graph-tool
      holders can currently mint syntactically valid lineage triples.
- [ ] 5.7 Archive the OpenSpec change so the predicate and graph-ingest deltas become current truth, only after local
      tasks 4.6, 5.2, 5.3, 5.6b, and 5.6c and coordinated owned-product tasks 1.5, 4.1a, 4.6b, and 5.1b are complete
