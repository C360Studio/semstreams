# Rule test cleanup

## Why

Issue #1428 executes the next test-only package batch under #1417. Merged main
`7a47c7c60d40345aa1c7ca8bbaddd9d3348b8b27` contains 24 rule cleanup baseline entries across ten files, within
297 total entries and 90 reviewed resolutions. These are existing cleanup liabilities, not reproduced hang counts.
The owner authorized continuing this package batch after graph-ingest #1424 merged.

## Scope and evidence gates

This is an inventory-phase claim. Enumerate exact baseline identities, helper ownership transfers and callers,
Start/operation authority, setup exits, native terminal contracts, joins and substrate lifetime. Preserve deliberate
contract probes and the #1062/#1283 regressions. Read the existing shared-support and graph-ingest patterns as
hypotheses for adoption, not proof that every rule owner has the same cleanup contract.

Target-state design, spec deltas and implementation tasks follow independent INVENTORY PASS. Design review and
coordinator acceptance within the authorized test-only scope precede implementation. No target-state design or
cleanup-safety result is claimed at this checkpoint.

## Boundaries

No production lifecycle redesign, new exported test framework, watchdog, blanket serialization, gate bypass or
assertion weakening. Guard removals require exact reviewed source/proof reconciliation. A finite terminal context
alone cannot establish native wall-clock termination or completed joining. #1417 remains the parent tracker;
#1293 and #1411/#1412 keep their existing gate and production scopes. Claude owns #1426 / #1427's E2E assertion work.
