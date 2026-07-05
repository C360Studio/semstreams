# Fusion

> Delta for gh#463 (ADR-071). ADDs the NL-scope contract to the `fusion` capability
> seeded by gh#475. Verified against `pkg/fusion` code.

## ADDED Requirements

### Requirement: A request MAY scope NL retrieval to entity-ID prefixes

`fusion.Request` MUST support an optional `Scope` — a list of dot-delimited entity-ID
prefixes. When non-empty, the engine MUST constrain NL seed resolution to entities
whose ID matches at least one prefix (OR-matched), so a lens instance over a shared
embedding index retrieves only its domain and is not diluted by a larger co-resident
domain. An empty/absent `Scope` MUST behave exactly as today (no filter). Matching is
by leading prefix on a dot boundary, not glob, and the scope MUST be applied at the
candidate source (before ranking), not as a post-retrieval trim, so a small domain
is never crowded out of the ranked window.

The scope MUST be threaded to the retrieval client via a struct parameter
(`ResolveQuery{Query, Mode, Scope, Limit}`) rather than a positional argument, so the
NL-only scope does not force symbol/prefix callers to pass an ignored value.

#### Scenario: a scoped NL query retrieves only the in-scope domain

- **GIVEN** a shared embedding index holding a large `code` domain and a small `docs`
      domain
- **WHEN** `Fuse` runs an NL request whose `Scope` names the docs ID prefix
- **THEN** the resolved seeds are docs entities only
- **AND** the small domain is not out-ranked by the larger one

#### Scenario: an empty scope is a no-op

- **GIVEN** an NL request with an empty/absent `Scope`
- **WHEN** `Fuse` resolves it
- **THEN** retrieval is identical to the unscoped behavior (byte-identical request)
