# Configurable EntityID virtual edges in community detection (gh#461)

## Why

`graph-clustering` runs community detection (LPA) over an edge set that is **not**
just the explicit graph edges: `initProviderAndDetector`
(`processor/graph-clustering/component.go:825`) wraps the KV adjacency provider in
`clustering.NewEntityIDProvider(..., clustering.DefaultEntityIDProviderConfig(), ...)`
with **hardcoded** defaults — sibling edges ON (`SiblingWeight 0.7`, `MaxSiblings
10`) and system-peer edges ON (`SystemPeerWeight 0.3`, `MaxSystemPeers 15`). These
synthesize *virtual* edges between every entity sharing the 5-part type prefix
(`org.platform.domain.system.type`) or the same system.

For a **homogeneous entity family whose explicit relationships already encode the
topology** — flocks of same-type entities linked by `flock.neighbor`, sensor
meshes, robot swarms — those virtual edges bridge everything and LPA collapses to
**one community regardless of the explicit structure**. There is no way to turn
them off.

Verified (gh#461, against beta.133 and beta.135): two fully-connected 4-boid
cliques with zero cross-cluster edges yield **2** level-0 communities when
`clustering.NewLPADetector` is driven directly with the raw adjacency, but **1**
community (all 8 members) through the full component — the only difference being
the `EntityIDProvider` wrapper. The heuristic is valuable when explicit edges are
sparse (entities still cluster near their family); it is destructive when explicit
topology *is* the signal.

The library already supports the off-switch: `NewEntityIDProvider`'s zero-value
defaulting touches only the numeric fields, so `IncludeSiblings:false` /
`IncludeSystemPeers:false` are honored verbatim
(`graph/clustering/entityid_provider.go:126-127`). The gap is purely that the
component never exposes them.

## What Changes

- **Expose EntityID virtual-edge synthesis through the graph-clustering component
  config.** An operator can disable sibling and/or system-peer virtual edges (and
  tune weights/caps), so community detection over a homogeneous family runs on the
  explicit topology alone.
- **Defaults preserve today's behavior (heuristics ON).** This is the load-bearing
  invariant: an operator config that omits the new block MUST behave exactly as
  today (`DefaultEntityIDProviderConfig`). A naive value-typed struct would make an
  omitted block a zero-value struct → `IncludeSiblings:false` → silently disabling
  the heuristics for every existing clustering deploy — a regression worse than the
  bug. The config shape MUST be tri-state (omitted → on; explicit false → off); see
  `design.md`.
- **Schema regen + JSON round-trip tests** for every new operator-reachable field
  (house rule: operator-configurable surface needs a JSON round-trip test; no
  shadow structs).

## Capabilities

### New Capabilities
- `graph-clustering` — seeded with the edge-set + configurable virtual-edge
  synthesis facet this change touches (community detection runs over explicit +
  optional EntityID-derived virtual edges; the synthesis is operator-configurable
  with defaults preserving current behavior). Distilled from code, not backfilled.

### Modified Capabilities
- None (no existing spec covers graph-clustering yet).

## Impact

- `processor/graph-clustering/component.go`: new nested config field + wiring in
  `initProviderAndDetector` (replace the hardcoded `DefaultEntityIDProviderConfig()`
  with the operator-resolved config) + defaulting in `ApplyDefaults` (`:136`).
- `graph/clustering/entityid_provider.go`: `EntityIDProviderConfig` currently has
  no JSON tags — the component owns a JSON-tagged config shape (or tags are added)
  rather than marshaling the bare library struct.
- Schema: `task schema:generate` will add the new field to the component schema
  (expected drift, committed).
- **Consumers:** semboids (reported — flock coloring by LPA community over live
  `flock.neighbor` edges); any homogeneous-family clustering use.

## Non-goals

- **Adaptive per-entity edge synthesis** (skip virtual edges when an entity's
  explicit degree ≥ k) — a smarter default than a global toggle, floated in gh#461;
  deferred as a follow-up refinement. The toggle unblocks the reported use now.
- **Changing the defaults** — heuristics stay ON by default; this only adds the
  ability to turn them off.
- **Retiring EntityID virtual edges** — they remain valuable for sparse-explicit
  graphs; this is about configurability, not removal.

## Consumers

`processor/graph-clustering` (framework component); semboids (reported consumer).
The knob is generic — any homogeneous entity family where explicit relationships
encode the real topology (sensor meshes, swarms, flocks) — so it belongs in the
framework, not a product.
