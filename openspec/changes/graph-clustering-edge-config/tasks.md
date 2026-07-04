# Tasks — Configurable EntityID virtual edges (gh#461)

> Scoping change (Proposed). Tasks unchecked; implementation follows approval.

## 1. Component config shape

- [x] 1.1 Added component-owned, JSON-tagged `EntityIDEdgesConfig` (tri-state
      toggles `include_siblings *bool` / `include_system_peers *bool` + value-typed
      numerics). Field `EntityIDEdges *EntityIDEdgesConfig` on `Config` with
      `schema:"type:object,...,category:advanced"`. Does NOT marshal the bare
      `clustering.EntityIDProviderConfig` (no JSON tags on it).
- [x] 1.2 Final shape = pointer bools (design.md Option 1). For reviewer confirm.

## 2. Defaulting + wiring (the load-bearing invariant)

- [x] 2.1 `Config.ApplyDefaults()` calls `EntityIDEdges.resolve()` → concrete
      `clustering.EntityIDProviderConfig` over a `DefaultEntityIDProviderConfig()`
      baseline (nil receiver / unset toggle / zero numeric → default). Stored on
      private `Config.entityIDEdges`.
- [x] 2.2 `initProviderAndDetector` consumes `c.config.entityIDEdges` instead of
      `clustering.DefaultEntityIDProviderConfig()`. Verified ordering:
      `ApplyDefaults` (factory :431) precedes `initProviderAndDetector` (Start
      :676); no reconfig path re-inits the provider.

## 3. Schema

- [x] 3.1 `task schema:generate` → `schemas/graph-clustering.v1.json` gains
      `entity_id_edges`; drift committed.

## 4. Tests

- [x] 4.1 JSON round-trip: pointer toggles + numerics survive; absent block → nil
      → resolves to defaults. (`TestConfig_JSONRoundTrip_EntityIDEdges`)
- [x] 4.2 Default-preservation: nil / omitted → resolves to exactly
      `DefaultEntityIDProviderConfig()`, incl. through `ApplyDefaults`.
      (`TestEntityIDEdgesConfig_Resolve_NilKeepsDefaults`,
      `TestApplyDefaults_ResolvesEntityIDEdges`)
- [x] 4.3 Behavioral (gh#461) at the GetNeighbors altitude (the exact mechanism;
      LPA correctness + `DisabledSiblings` already covered in the library): two
      disjoint same-type cliques → default config synthesizes cross-clique sibling
      edges (bug), disabled config yields explicit intra-clique edges only (fix).
      (`TestEntityIDEdges_ResolvedConfigControlsVirtualEdges`)
- [x] 4.4 (review HIGH) No-silent-drop guard: strict-decode `entity_id_edges`
      with `DisallowUnknownFields` in the factory (`rejectUnknownEntityIDEdgeKeys`,
      mirrors the `anomaly_config` ADR-054 guard) so a toggle typo fails loudly
      instead of silently leaving synthesis ON. Factory-wire test
      (`TestCreateGraphClustering_RejectsUnknownEntityIDEdgeKey`) + schema
      descriptions on the nested fields (review nit).
- [ ] 4.5 (review LOW, deferred) A component-level assertion that
      `initProviderAndDetector` passes `c.config.entityIDEdges` to the provider
      needs the integration harness (live KV buckets). resolve→field is covered by
      4.2; field→provider is a single verified-by-inspection read. Conscious gap.

## 5. Spec + close

- [ ] 5.1 `openspec validate --strict`; gates green (`go test -race`,
      `-tags=integration` for `processor/graph-clustering`, `task lint`, schema
      committed); semstreams-reviewer; archive → promote `graph-clustering` into
      `openspec/specs/`.
- [ ] 5.2 Confirm the diagnosis + fix back to semboids on gh#461; note the config
      example (`{"entity_id_edges": {"include_siblings": false, "include_system_peers": false}}`).
- [ ] 5.3 File the adaptive-synthesis follow-up (skip virtual edges when explicit
      degree ≥ k) referencing this change.
