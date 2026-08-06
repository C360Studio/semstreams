# Sister-repo cutover checklist — pre-v1 breaking wave

This checklist coordinates one migration target and one destructive graph-state cutover per product. The beta pins
below were observed in local checkouts on 2026-07-17. They describe audit starting points, not approved migration
targets. Re-read each repository's dependency file and record its commit before relying on a pin.

The migration target is the single breaking SemStreams tag containing all of the following:

- predicates PR #532;
- entity-ID/KV PRs #534 and #536;
- package boundaries PR #535;
- lineage, watcher, and event-identity PRs #537–539;
- graph-index replacement semantics for NAME, PREDICATE, and source-owned INCOMING rows; and
- the approved predicate-representation decision and its selected implementation.

## Coordinated release order

1. Merge the identity and package-boundary wave through PR #539.
2. Approve and land graph-index replacement semantics.
3. Approve the predicate-representation decision and land the selected raw-key or documented fallback
   implementation.
4. Pass the combined framework release gates, then cut one breaking tag containing all three bodies of work.
5. Migrate every product to that tag before its single stop, wipe, restart, and canonical reseed.

The index work does not ship on a later routine bump. It consumes the same pre-v1 wipe window as the identity
changes. A missed window requires a separate migration proposal; it must not create a second undeclared wipe.

## Universal product procedure

1. Record the observed source pin and commit separately from the common breaking-tag migration target.
2. Bump to the breaking tag and build before touching persisted state.
3. Run the shared entity-ID and predicate audits over source, configs, schemas, fixtures, and seed data. Fix every
   finding: canonical bounded six-part IDs, canonical three-part predicates, and no legacy `lineage.*` or
   `reply_to` forms. Note: `triple.append` rejections for a non-canonical predicate carry
   error code `structural_invalid` (class `invalid`, do-not-retry) — branch on the class, not the code string, per
   ADR-060.
4. Give every rule-processor config an explicit `pack_id`. It must be 1–246 ASCII bytes matching
   `[A-Za-z0-9_=-]+`, contain no dot, and be unique across the enabled composition. Set
   `enable_graph_integration` explicitly because its default changed from `true` to `false`.
5. Stop every writer against the target NATS account and capture the rendered deployment configuration.
6. Derive the deletion set from that configuration and the framework bucket inventory. Remove `ENTITY_STATES`,
   graph-ingest guard buckets, and every enabled framework-derived graph bucket under its resolved name. Do not
   remove unrelated product, operational, workflow, or upstream source-system buckets. Never apply a copied
   default list or wildcard deletion to a shared account. Follow
   [29 — entity-ID contract clean cutover](29-entity-id-contract-clean-cutover.md) for the current inventory.
7. Start only migrated producers, reseed from canonical owned sources, wait for index readiness, prove query parity,
   and restart once without another write to prove replay parity.
8. Run the affected product E2E suites and complete the evidence envelope below.

There is no compatibility reader, in-place beta-state migration, or rollback. The destructive scope is the
deployment-derived graph state, not every NATS resource in the account.

## Required evidence envelope

Create one immutable record per product. A cross-product summary may link these records but may not replace them.

| Field | Required evidence |
|---|---|
| Product identity | Repository, owner, clean migration commit, and evidence timestamp. |
| Dependency transition | Observed beta tag and commit; common breaking target tag and SemStreams commit. |
| Deployment identity | Environment, composition/config commit, NATS context/account, and rendered bucket names. |
| Corpus gates | Exact audit commands and versions, scope, zero legacy/unclassified findings, and manifest review. |
| Composition | Component/payload/tool inventory; every `pack_id`; graph-integration mode; uniqueness result. |
| Package cutover | Removed imports/facades, replacement owner packages, registrations, and generated-artifact diff. |
| Wipe | Writers stopped; exact buckets removed; intentionally retained product buckets; operator and timestamp. |
| Reseed/rebuild | Canonical source and version, counts, readiness target/revision, query parity, and replay parity. |
| Event consumer | Bounded audit result or named consumer with first-create and repeated-replacement proof. |
| Verification | Exact test/E2E commands, environment, result, artifact link, and product-owner sign-off. |
| Exceptions | Open blockers or `none`; no silent waiver or compatibility shim. |

Evidence collected from a dirty tree, a different deployment revision, or before the final combined framework
commit is diagnostic only and must not be promoted to release evidence.

## Per-repo specifics (heaviest first)

### semconnect — observed beta.141 — HEAVY

- 45 Go files import the removed OGC bundle (`message/oms`, `parser/sensorml`, `pkg/swecommon`, and
  `vocabulary/{csapi,oms,sosa,swe}`). Self-host the bundle per the ADR-075 owner inventory in
  [27 — framework package boundary clean break](27-framework-package-boundary-clean-break.md): equivalent packages,
  tests, canonical fixtures, vocabulary, and payload registration.
- Explicitly register payload `ogc.oms.v3` in every binary that decodes it. Ambient registration is removed.
- Verify that every OGC-derived CS API entity ID satisfies the bounded six-part grammar before reseeding.
- Rename `cs-api.deployment.deployedSystems` to `cs-api.deployment.deployed-systems` and
  `cs-api.samplingfeature.hostedProcedure` to `cs-api.samplingfeature.hosted-procedure`. The old mixed-case writes
  fail closed after migration. The mappings are release rows in
  [24 — predicate breaking rename ledger](24-predicate-breaking-rename-ledger.md).
- Transferred backlog is filed in SemConnect: #69 (swecommon Phase 2), #70 (Feasibility vocabulary promotion),
  and #71 (association/composition predicates and case migration).

### semteams — observed beta.115 — HEAVY (drift and ownership transfer)

- The checkout was 31 betas behind the observed framework pin; budget for that drift plus this wave.
- Remove `oasf-generator`, `directory-bridge`, and `a2a-adapter` from `configs/flow-bootstrap.json` and
  `configs/e2e-flow-bootstrap.json`. Delete stale `a2a-adapter.v1`, `slim-bridge.v1`, `oasf-generator.v1`, and
  `directory-bridge.v1` schemas.
- Re-home OASF projection and AGNTCY directory registration as owner. Do not copy the deleted A2A/SLIM facades.
- `pack_id: "semteams"` was present and grammar-safe; prove composition uniqueness at the migration commit.

### semspec — observed beta.134 — MEDIUM

- Four of 17 observed rule-processor configs lacked `pack_id` and will fail startup until updated.
- Regenerate `ui/src/lib/types/semstreams.generated.ts` from the reduced OpenAPI. Prove the catalog omits
  `a2a-adapter.v1`, `directory-bridge.v1`, `github_webhook.v1`, `oasf-generator.v1`, and `slim-bridge.v1`.
- Thirteen observed configs set `enable_graph_integration` explicitly. Keep the setting explicit and re-audit all
  configs at the migration commit.

### semdev — observed beta.146 — LIGHT-MEDIUM

- Three files imported `semstreams/input/github-webhook`. Re-home them behind `internal/boot` per ADR-075 and own
  the GitHub executors, webhook types, and workflow/rule policy.
- `pack_id: "semdev"` was present and grammar-safe, and graph integration was explicit. Re-prove both at the
  migration commit.
- Transferred backlog is filed in SemDev: #2 (comment parent number/id) and #3 (specific added/removed label).

### semdragon — observed beta.135 — LIGHT

- One observed rule-processor config needs `pack_id` and an explicit `enable_graph_integration` value.

### semboids — observed beta.146 — LIGHT

- One observed rule-processor config needs `pack_id`.
- Re-run load instrumentation against the new watcher ordering. Per-entity serialization and coalescing may shift
  throughput. File verified issues for regressions.

### semops — observed beta.145 — LIGHT, functional check required

- The source grep found no direct migration hit, but the ops role reads alert entities. Legacy `alert_...` IDs become
  `semstreams.framework.graph.rules.alert.<sha256>`, with one entity per occurrence. Verify diagnosis queries and
  aggregations against the new identity and cardinality behavior.

### semlink — observed beta.141 — LIGHT

- Classify the one observed `alert_*` hit, which may be a config-key false positive. Otherwise apply the universal
  procedure.

### semsource — observed beta.145 — TRIVIAL

- Correct contributor documentation that says `federation.*` types come from SemStreams. Apply the universal
  procedure.

### semstreams-ui — observed dependency pin not captured

- Capture the actual dependency and commit at migration time. Regenerate clients from the breaking target and prove
  that removed schemas, predicates, exact IDs, and package facades are absent.

## Cross-cutting findings

- The alert/trigger identity change affects every product that queries rule-derived entities. A producer-only grep
  is not proof of absence; inspect query and aggregation paths.
- A bounded 2026-07-17 audit of the owned repositories listed here found no behavior-bearing consumer of
  `graph.events.*`. This is not a global claim: it excludes unowned/private repositories, runtime-only subscriptions,
  and deployments not represented by the audited commits. Record the exact scan scope and commit in each product's
  evidence envelope.
- Current graph-event normative text requires a named consumer to prove first-trigger create/upsert and
  repeated-trigger replacement. The bounded no-consumer result cannot satisfy that wording. Release sign-off remains
  open until the normative change is amended and approved to accept the bounded no-consumer outcome, or an owned
  consumer is identified and supplies the required proof.
- No product may begin consuming `graph.events.*` while that outcome is unresolved. Producers currently emit
  update-shaped trigger events; must-exist update would reject the first trigger, while append would violate the
  stable-entity contract.

## See also

- [24 — predicate breaking rename ledger](24-predicate-breaking-rename-ledger.md)
- [25 — predicate corpus audit and release gate](25-predicate-corpus-audit-release-gate.md)
- [27 — framework package boundary clean break](27-framework-package-boundary-clean-break.md)
- [29 — entity-ID contract clean cutover](29-entity-id-contract-clean-cutover.md)
- [30 — rule-event identity clean cutover](30-rule-event-identity-clean-cutover.md)
