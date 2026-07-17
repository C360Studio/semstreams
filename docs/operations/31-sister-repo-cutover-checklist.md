# Sister-repo cutover checklist — pre-v1 breaking wave

Migration target: the breaking tag cut after PR #539 merges (predicates #532 + entity-ID/KV #534/#536 +
package boundaries #535 + lineage/watcher/event-identity #537–539). One migration, one wipe, per repo.
Grepped against local checkouts 2026-07-17; re-verify each repo's tree at migration time.

## Universal steps (every repo, in order)

1. Bump `c360studio/semstreams` to the breaking tag; build.
2. Run the shared corpus audits from the semstreams tooling: entity-ID audit + predicate audit over the
   repo's source, configs, schemas, fixtures, and seed data. Fix every finding (canonical 6-part bounded IDs;
   3-part predicates; no legacy `lineage.*` / `reply_to` forms).
3. Every rule-processor config declares an explicit `pack_id` (1–246 ASCII of `[A-Za-z0-9_=-]+` — NO dots),
   unique across the enabled composition, and sets `enable_graph_integration` EXPLICITLY (the default flipped
   true→false).
4. Wipe all NATS state for the deployment (authoritative + derived), restart, reseed from canonical owned
   sources. There is no migration path, compatibility reader, or rollback by design.
5. Product e2e green. Record the evidence for the coordinated release notes.

## Per-repo specifics (heaviest first)

### semconnect (pin beta.141) — HEAVY
- 45 Go files import the removed OGC bundle (`message/oms`, `parser/sensorml`, `pkg/swecommon`,
  `vocabulary/{csapi,oms,sosa,swe}`). Self-host the bundle per the ADR-075 owner inventory
  (semstreams docs/operations/27): equivalent packages, tests, canonical fixtures, vocabulary + payload
  registration.
- Explicitly register payload `ogc.oms.v3` in every binary that decodes it (no more ambient registration).
- CS API entity-ID contract: verify OGC-derived IDs satisfy the bounded 6-part grammar before reseeding.

### semteams (pin beta.115) — HEAVY (drift + ownership transfer)
- 31 betas of drift PLUS the wave; budget accordingly.
- Remove `oasf-generator` / `directory-bridge` / `a2a-adapter` entries from `configs/flow-bootstrap.json` and
  `configs/e2e-flow-bootstrap.json`; delete stale schemas (`a2a-adapter.v1`, `slim-bridge.v1`,
  `oasf-generator.v1`, `directory-bridge.v1`).
- Re-home OASF projection + AGNTCY directory registration as owner (per ADR-075); do NOT copy the deleted
  A2A/SLIM facades.
- `pack_id: "semteams"` already present and grammar-safe — verify composition uniqueness.

### semspec (pin beta.134) — MEDIUM
- 4 of 17 rule-processor configs missing `pack_id` → boot-fail until added.
- Regenerate `ui/src/lib/types/semstreams.generated.ts` from the reduced OpenAPI; prove the catalog no longer
  advertises `a2a-adapter.v1`, `directory-bridge.v1`, `github_webhook.v1`, `oasf-generator.v1`,
  `slim-bridge.v1`.
- 13 configs already set `enable_graph_integration` explicitly — protected from the default flip; keep them
  explicit.

### semdev (pin beta.146) — LIGHT-MEDIUM
- 3 files import `semstreams/input/github-webhook` — re-home behind `internal/boot` per the ADR-075
  inventory; own the GitHub executors, webhook types, and workflow/rule policy.
- `pack_id: "semdev"` present and grammar-safe; `enable_graph_integration` explicit.

### semdragon (pin beta.135) — LIGHT
- 1 rule-processor config: add `pack_id`; set `enable_graph_integration` explicitly.

### semboids (pin beta.146) — LIGHT
- 1 rule-processor config: add `pack_id`.
- Re-run load instrumentation against the new watcher ordering (per-entity serialization + coalescing may
  shift throughput characteristics); file verified gh issues if regressions appear.

### semops (pin beta.145) — LIGHT, functional check required
- No grep hits, BUT the ops role reads alert entities: legacy `alert_...` IDs are replaced by
  `semstreams.framework.graph.rules.alert.<sha256>` (occurrence-scoped — one entity per occurrence).
  Verify diagnosis queries/aggregations against the new identity scheme and cardinality behavior.

### semlink (pin beta.141) — LIGHT
- One grep hit to classify (likely an `alert_*` config-key false positive); otherwise universal steps only.

### semsource (pin beta.145) — TRIVIAL
- Correct the stale contributor docs claiming `federation.*` types come from semstreams (documentation
  release gate from ADR-075); universal steps only.

## Cross-cutting flags

- The alert/trigger identity change affects ANY repo that queries rule-derived entities — grep is not
  sufficient proof of absence; check query paths.
- `graph.events.*` currently has NO consumer anywhere; if a product plans to consume it, note the trigger
  entities are update-only (never created) and application semantics (replace vs append) are not yet pinned —
  coordinate with the graph-index replacement-semantics change before building on it.
- Index hardening (replacement semantics + predicate raw-key decision) lands AFTER the tag and is
  derived-state only: no second source migration, no second authoritative wipe. Deployments will see a
  derived-bucket rebuild behind readiness gates on a later routine bump.

## See also

- [27 — framework package boundary clean break](27-framework-package-boundary-clean-break.md) (per-repo owner inventories)
- [29 — entity-ID contract clean cutover](29-entity-id-contract-clean-cutover.md)
- 30 — rule-event identity clean cutover (lands with the identity slice PR)
