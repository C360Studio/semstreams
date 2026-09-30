# E2E required-check evidence

Issue: #1222. Milestone: v1.0.0-beta.165, #1134 work package C.
Baseline: fe9482b7f336e575317cfb45fd1ad7c40baf7904.

## Why

The owner approved an E2E coverage survey and reconciled #1222 as the existing home for truthful selected-check
outcomes and evidence. The issue records hypotheses requiring repository-first inventory: an assertion count may
include a stage that warning-skips its observation, and a suite name may imply more coverage than it selects.
Existing result writers, task selection, checks and release-evidence contracts must be measured before design.

## What Changes

This initial commit establishes the design-phase claim only. It changes no runner behavior, test, current spec,
CI workflow or claimed acceptance outcome. No target API, result representation or suite membership is selected.

The first deliverable is the surface and adopter inventory, with an exact baseline, source pins and search record.
Independent INVENTORY PASS precedes options and design. Independent design review and owner acceptance precede
behavioral deltas or implementation. The issue's bounded acceptance remains the scope authority.

## Impact

Inventory surfaces include cmd/e2e, test/e2e/scenarios and results, Taskfiles, workflow gates, the canonical testing
and E2E guides, and the existing release-candidate proof contract. This is an inspection boundary, not a write list.

#1117 retains semantic CI; #769/#1128 retain agentic/CRUD CI and persona proof; #1293 retains broader verification
plumbing. #1195/#1224/#1288 retain their capability-specific defects. No new test framework, all-tier-per-PR mandate,
model-quality threshold, fourth beta.165 package, or parallel implementation owner is implied.

## Parallel-work boundary

At claim preparation, #1402/#1403 own agentic recovery, and #1404 owns #1188 config namespacing. The #1188 worktree
has uncommitted edits in test/e2e/scenarios/agentic/scenario.go, test/e2e/config/tier_authority.go and its tests,
and test/e2e/scenarios/platform_identity.go. A remote PR file list alone did not expose those pending changes.

This claim starts with inventory/design only. Do not edit those shared scenario/authority files, runtime recovery,
or config implementation while the existing writer owns them. Reconcile the landed base and live claims before
implementation. Shared Docker/integration/E2E work remains serialized; no heavy host run is part of this claim.
