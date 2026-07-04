# Design — Configurable EntityID virtual edges (gh#461)

## The decision

Expose the EntityID virtual-edge synthesis through the component config with a
**tri-state shape** so that an *omitted* config reproduces today's behavior
(heuristics ON) and an operator can *explicitly* disable siblings and/or
system-peers. Wire the resolved config into `initProviderAndDetector` in place of
the hardcoded `DefaultEntityIDProviderConfig()`.

## The load-bearing invariant

**Omitted config → current behavior (siblings + system-peers ON).** This is not a
nicety — the current default is ON, and community detection over heterogeneous /
sparse-explicit graphs relies on it. If the config shape makes "omitted" resolve
to OFF, the change silently regresses every existing clustering deploy the moment
the field exists. So the shape must distinguish three states for each toggle:
*unset* (→ default), *explicit true*, *explicit false*.

Go's `bool` zero value is `false`, so a plain value-typed `bool` cannot express
*unset* vs *explicit false*. That is the entire design problem.

## Options for the toggle shape

1. **Pointer bools, positive framing (recommended).**
   `IncludeSiblings *bool` / `IncludeSystemPeers *bool` on a nested config; `nil`
   → default (on), `&true`/`&false` honored. Matches gh#461's suggested JSON
   (`{"include_siblings": false}`) exactly and the library field names. Cost:
   pointer bools need a JSON round-trip test covering absent / `true` / `false` /
   `null` (house rule anyway).

2. **Inverted value booleans.** `DisableSiblings bool` / `DisableSystemPeers bool`;
   zero (`false`) → enabled = current behavior, `true` → off. No pointers, zero
   value is safe by construction. Cost: double-negative field names; can't tune
   the numeric weights via the same mechanism without more fields.

3. **Presence-tracked nested block.** `EntityIDEdges *EntityIDEdgesConfig` where a
   `nil` block → `DefaultEntityIDProviderConfig()`. Solves the *whole-block*
   absent case, but a *present* block still can't tell an omitted `include_siblings`
   from an explicit `false` — so the per-field toggles inside it collapse back to
   Option 1 (pointer bools) or Option 2 (inverted).

**Recommendation: Option 1** (pointer bools, positive framing) for the two
toggles, on a nested optional block (`entity_id_edges`), with the numeric fields
(`sibling_weight`, `max_siblings`, `system_peer_weight`, `max_system_peers`)
value-typed and defaulted-when-zero (the library already does this at
`entityid_provider.go:108-120`). Positive framing matches the operator mental
model and the issue; pointers give the honest tri-state; numeric defaulting reuses
the existing library behavior. If the reviewer prefers to avoid pointer bools,
Option 2 is the fallback (safer zero value, uglier names).

## Where the defaulting lives

`Config.ApplyDefaults()` (`component.go:136`) is the existing defaulting seam.
Resolve the operator config there into a concrete `clustering.EntityIDProviderConfig`
(unset toggles → `true`; unset numerics → library defaults) and have
`initProviderAndDetector` consume the resolved value instead of calling
`DefaultEntityIDProviderConfig()` directly. Keeping resolution in `ApplyDefaults`
(not scattered at the call site) makes the "omitted = on" invariant testable in
one place.

## JSON-tag note

`clustering.EntityIDProviderConfig` has **no** JSON tags today. Do NOT marshal the
bare library struct into operator config (it would expose Go field names and no
tri-state). Define a component-owned, JSON-tagged config type (e.g.
`EntityIDEdgesConfig`) and map it to the library struct in `ApplyDefaults` — no
shadow struct that silently drops fields (house rule).

## Tests

- JSON round-trip for every operator-reachable field, asserting the tri-state:
  absent block → resolves to defaults-on; `{"include_siblings": false}` → siblings
  off, system-peers still on; explicit numerics honored.
- A behavioral regression mirroring gh#461: two disjoint same-type cliques through
  the component with `include_siblings:false, include_system_peers:false` yield 2
  communities (not 1). (Can reuse the library-level `NewLPADetector` proof + the
  component path; keep it deterministic, no wall-clock.)
- A default-preservation test: omitted block → the provider is constructed with
  the same values as `DefaultEntityIDProviderConfig()`.

## Non-goals / follow-up

- **Adaptive synthesis** (skip virtual edges when explicit degree ≥ k) — a better
  default than a global switch, but a separate change; this one is the operator
  escape hatch. Record it as the natural next step.
