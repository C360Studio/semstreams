# Graph-query test cleanup ownership

## Why

Issue #1433 executes the next reviewed package batch under #1417. The incorporated main revision `1b1accf4` retains 39 graph-query
cleanup-baseline identities across five files, within 273 legacy entries and 96 reviewed resolutions. A terminal
cleanup call without finite authority can obscure the original failure behind the package alarm. The baseline
identifies liabilities; it does not prove that every listed site has hung.

## What Changes

Adopt the existing private fixture-owner pattern across the 39 entries and preserve adjacent lifecycle probes.
Register ownership before fallible setup, check one finite terminal attempt, and retain accepted Start authority
until that attempt finishes. Preserve operation authority and observe actual native completion where claimed.
Remove only the selected legacy identities and add the independently reviewed deferred-cancellation classification.

The scope is test-only. #1421 remains open after the separate #1432 and #1435 repairs and their individual merge
waivers; neither waiver transfers to this batch. #1293 owns broader coverage/common-gate gaps.

## Status

The test-only ownership changes received PACKAGE REVIEW PASS at
`80565d7b0cab337a8e750cc330aee4a4d4940d8d`; the full verdict is in `review/implementation-review.md`.
All 39 selected legacy identities are removed. The exact reviewed deferred-cancellation classification adds one
resolution; totals are 234 legacy entries / 97 resolutions. Existing resolutions remain unchanged.
Focused race/native checks, five mutation failures and the cleanup guard passed; `evidence/verification.md` records
their limits. Canonical `task check:push` failed during a Docker container-start timeout; earlier required stages
passed with unchanged source. Shared-host overlap was observed; causation is unproven. The separate canonical
integration-stage run passed on matching unchanged source. Required local stages have passing evidence across
the two recorded runs; the full-command failure remains recorded.
Final archive/spec review and hosted CI remain required. No merge readiness or transferable waiver is claimed.
