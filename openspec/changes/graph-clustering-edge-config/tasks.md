# Tasks — Configurable EntityID virtual edges (gh#461)

> Scoping change (Proposed). Tasks unchecked; implementation follows approval.

## 1. Component config shape

- [ ] 1.1 Add a component-owned, JSON-tagged nested config type (e.g.
      `EntityIDEdgesConfig`) with tri-state toggles `include_siblings *bool` /
      `include_system_peers *bool` and value-typed numerics `sibling_weight`,
      `max_siblings`, `system_peer_weight`, `max_system_peers`. Add the field to
      `graph-clustering` `Config` with a `schema:"type:object,...,category:advanced"`
      tag. Do NOT marshal the bare `clustering.EntityIDProviderConfig`.
- [ ] 1.2 Confirm the final toggle shape with the reviewer (pointer bools vs
      inverted booleans — design.md Option 1 vs 2).

## 2. Defaulting + wiring (the load-bearing invariant)

- [ ] 2.1 In `Config.ApplyDefaults()` (`component.go:136`), resolve the operator
      config into a concrete `clustering.EntityIDProviderConfig`: unset toggles →
      `true`; unset numerics → library defaults. An omitted block MUST resolve to
      exactly `DefaultEntityIDProviderConfig()`.
- [ ] 2.2 `initProviderAndDetector` (`component.go:825`) consumes the resolved
      config instead of calling `clustering.DefaultEntityIDProviderConfig()`.

## 3. Schema

- [ ] 3.1 `task schema:generate`; commit the expected component-schema drift.

## 4. Tests

- [ ] 4.1 JSON round-trip per operator-reachable field with tri-state assertions:
      absent block → defaults-on; `{"include_siblings": false}` → siblings off,
      system-peers on; explicit numerics honored. (No shadow struct; assert
      `reflect`-level shape where a wider destination type applies.)
- [ ] 4.2 Default-preservation: omitted block → provider constructed with the same
      values as `DefaultEntityIDProviderConfig()` (guards the regression the shape
      could introduce).
- [ ] 4.3 Behavioral regression (gh#461): two disjoint same-type cliques through
      the component with siblings+system-peers OFF yield 2 level-0 communities, not
      1. Deterministic; reuse the `NewLPADetector` proof + the component path.

## 5. Spec + close

- [ ] 5.1 `openspec validate --strict`; gates green (`go test -race`,
      `-tags=integration` for `processor/graph-clustering`, `task lint`, schema
      committed); semstreams-reviewer; archive → promote `graph-clustering` into
      `openspec/specs/`.
- [ ] 5.2 Confirm the diagnosis + fix back to semboids on gh#461; note the config
      example (`{"entity_id_edges": {"include_siblings": false, "include_system_peers": false}}`).
- [ ] 5.3 File the adaptive-synthesis follow-up (skip virtual edges when explicit
      degree ≥ k) referencing this change.
