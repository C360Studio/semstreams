# Graph-query test cleanup ownership

## Why

Issue #1433 executes the next reviewed package batch under #1417. Main at `41d6236a` retains 39 graph-query
cleanup-baseline identities across five files, within 273 legacy entries and 94 reviewed resolutions. A terminal
cleanup call without finite authority can obscure the original failure behind the package alarm. The baseline
identifies liabilities; it does not prove that every listed site has hung.

## Authorized scope

Inventory the exact identities and related setup helpers, callers, Start/operation authority, terminal behavior,
join ownership and substrate lifetime. Preserve deliberate lifecycle probes. Any implementation follows independent
inventory and design review, adopting existing shared test-support patterns where their contract fits.

No production behavior change, new testing framework, broad serialization, weaker assertion or new guard approval
is authorized by this package batch. #1421 remains open after the separate #1432 repair and one-PR waiver.
#1293 owns broader coverage/common-gate gaps; Claude's E2E claims remain separate.

## Status

Initial claim only. Inventory, design and validation remain unperformed for this batch. The subsequent reviewed
inventory will define the precise change and spec delta. No cleanup count reduction or merge-readiness is claimed.
