## 1. Contract and Inventory

- [x] 1.1 Generate the complete local predicate corpus from Go producers/constants, configuration ASTs,
      lifecycle/ownership/projection declarations, schemas, tools, and reference deployments
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

- [ ] 4.1 Rename all first-party and owned sister-repository producers, rules, schemas, tools, and exact queries
- [x] 4.2 Publish the reviewed rename ledger as breaking release documentation, not a runtime alias table
- [x] 4.3 Make update/replace validate the complete candidate before any destructive removal
- [x] 4.4 Add graph-ingest and graph-index replay validation that independently blocks readiness on invalid state
- [x] 4.4a Use the shared typed decoder in spatial, temporal, embedding, clustering, rule, lifecycle, OASF, and
      direct-query ENTITY_STATES consumers; poison projection readiness and forbid partial output
- [x] 4.5 Document optional export followed by bucket reset and canonical-source reingest
- [ ] 4.6 Prove clean restart/replay and exact/namespace query fixtures after reset/reingest

## 5. Enforcement and Release Gates

- [ ] 5.1 Run local, owned sister-repository, and reference-design audits to zero violations
- [ ] 5.2 Run reset/reingest with expected query-result fixtures against representative beta state
- [ ] 5.3 Seed invalid preexisting state in real NATS and prove every graph-index/query consumer stays not-ready
- [x] 5.4 Run lint, race, schema no-drift, contract, real-NATS integration, and affected e2e suites
- [x] 5.5 Publish breaking upgrade, export/reset/reingest, and rejection runbooks
- [x] 5.6 Verify no alias, dual-read/write, permissive-mode, or in-process migration code remains
- [ ] 5.7 Archive the OpenSpec change so the predicate and graph-ingest deltas become current truth
