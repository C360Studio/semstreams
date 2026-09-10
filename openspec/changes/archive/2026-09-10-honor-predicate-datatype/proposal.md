# Change: Honor the registry's declared predicate DataType (and decide Units/Range) — closed datatype vocabulary, typed export, contract guard

Closes #1267. Claim: PR on `claude/gh1267-honor-predicate-datatype`, own worktree. Premises pinned at `main@797d294a`
(re-measure at design time). **Design status: NOT STARTED — this proposal is the claim commit only.** The mandatory
first deliverable (a file:line surface inventory via `semstreams-explorer`, then the architect's design with its
adopter-seam inventory) is owed by the Opus session that picks this up. Nothing here is a design decision.

**Owner ruling (2026-09-07, in session, verbatim on #1267):** "agree - we need to honor them. and now is the time to
establish best practices with our vocab/datatype." Decision A: honor. `horizon:pre-v1`; milestone owed.

**Sequencing:** measured 2026-09-07 against every open draft PR's full file list (#1262, #1254, #1156, #1159, #1141,
paginated): zero paths under `vocabulary/`, `test/contract/`, `docs/basics/04-vocabulary.md`, ADR-074, or a
`predicate-contract` delta. No claim on #1142. One forward seam: #1261 (PR #1262, design-phase) will READ the registry
to expose a tool schema; this change defines what it reads — land this first, small, so #1261 consumes typed constants.

## Why

`vocabulary.PredicateMetadata.{DataType,Units,Range}` (`vocabulary/predicates.go:360-366`) are set by
`WithDataType/WithUnits/WithRange` (`vocabulary/registry.go:112-128`) at 200 / 12 / 11 in-repo call sites and by five
sisters, and **read by nothing** — in-repo (`git grep -n -w -E '\.(DataType|Units|Range)' -- '*.go' ':!vocabulary/**'
':!*_test.go'` → empty) or in any sister. `vocabulary/export` types objects by Go type inference
(`vocabulary/export/object.go:67-81`; `float64` → `xsd:double` unconditionally at `:106-114`), so a declared
`WithDataType("int")` has no effect; a KV-read number is `float64` after `encoding/json`, so it exports as
`"5.0"^^xsd:double`. `DataType` is free-form with eight spellings (`string` 141, `float64` 20, `int` 14, `time.Time`
10, `timestamp` 8, `bool` 4, `int64` 1, `entity_ref` 1). The export package has zero production callers in this repo;
the family's only production RDF emitter is semconnect's CS API gateway. `vocabulary` is Tier 1
(`release/tier1-packages.txt:95`): a write-only field frozen at 1.0 is a promise to honor nothing, and a free-form
string frozen at 1.0 cannot be tightened later.

## What changes (scope recorded on #1267; the design answers HOW)

1. A closed `DataType` vocabulary with typed constants; the eight legacy spellings map onto it; `Register` rejects an
   unknown value. `WithDataType` keeps its `string` parameter (sister source- and apidiff-compatibility); tightening is
   by validation, not signature. One table renders three ways: Go type, XSD IRI, JSON Schema type.
2. `vocabulary/export` honors the declaration, inference only as fallback. Natural home for #1142 (`entity_ref`/IRI
   datatype → resource, not literal) — ride or precede, architect's call.
3. A `test/contract/` drift guard: every registered predicate carries a `DataType` from the closed set.
4. Readers wired by file: export; the #1261 tool schema; #1264 affordance property shapes.
5. Docs: ADR-074 gains the datatype rule as a decision; `vocabulary/README.md` and `docs/basics/04-vocabulary.md`
   show the one right way to declare a predicate.
6. `Units` / `Range`: honored on this pass or decided explicitly — never left write-only.
7. Architect fork (not pre-decided): ingest-time coercion under graph-ingest's single-writer authority vs export-time
   only.

## Expected first-push red

Per `.agents/protocol.md` § claim: this change has no spec delta yet, so the Lint job's last step,
`Validate OpenSpec changes and specs (strict)`, is EXPECTED red until the first delta to
`openspec/specs/predicate-contract/spec.md` lands. Any other red on that run is real.

## Adopter seam (to be answered by the design)

A sister author registering a predicate: what must they know — ideally only "pick one of these constants"; what
happens if they do nothing — their existing string literal keeps compiling and either maps or is rejected at
`Register` with the constant named; where they find out — the doc comment on `WithDataType` and ADR-074.
