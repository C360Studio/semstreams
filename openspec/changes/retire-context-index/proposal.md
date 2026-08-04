## Why

`CONTEXT_INDEX` is maintained on every graph-index reconciliation and participates in required
failure/readiness handling, but neither SemStreams production code nor scanned sister-repository
production Go code reads it. The provenance fact already lives on `Triple.Context` inside
authoritative `ENTITY_STATES`.

Maintaining a second unconsumed durable spelling increases write volume, failure surface, tests,
bucket-catalog size, and adopter confusion. A failed context write can currently withhold
graph-index readiness from query views that do have production consumers.

## What Changes

- Remove `CONTEXT_INDEX` from the framework KV catalog and graph-index lifecycle.
- Remove its key codec, storage handle, plan/apply/delete/reconcile logic, metrics, and physical
  representation tests.
- Preserve `Triple.Context` unchanged in authoritative entity state.
- Replace E2E physical-index checks with an authoritative hierarchy-provenance assertion.
- Assert that a fresh stack does not create `CONTEXT_INDEX`.
- Update current specs, product-boundary inventory, configuration/docs examples, generated
  artifacts, and the clean-wipe runbook.

## Non-goals

- No general materialized-view runtime.
- No new production provenance query.
- No change to `Triple.Context` or graph mutation semantics.
- No `STRUCTURAL_INDEX` removal.
- No `graph/query` client or mutation-subject removal.

## Breaking release

This is a pre-v1 breaking change. Fresh beta state is wiped and reseeded. No legacy bucket alias,
reader, translation, dual write, online migration, or compatibility layer is provided. The
`e2e:structural` tier must be green before merge because it covers hierarchy ingestion and the
provenance assertion moved to authority.

## Impact

- **Affected specs:** `graph-index`, `graph-retention`, and the project product-boundary inventory.
- **Affected runtime:** graph-index writes, deletion cleanup, metrics, and readiness failure scope.
- **Affected tests:** graph-index physical-format tests and E2E diagnostic readers/scenarios.
- **Preserved contract:** `message.Triple.Context` remains in `ENTITY_STATES`.
