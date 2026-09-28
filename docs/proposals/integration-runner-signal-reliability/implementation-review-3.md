# Final implementation review

Mode: implementation/merge re-review. Reviewer: `semstreams-reviewer`, configured `gpt-6-astra`.
Reviewed on 2026-09-28. Source MD5: `21fe5623fbd1eb47f4411264d3adf007`.
Unchanged production runner MD5: `ef0dde707bec1ab6a39d5ad7293896fe`.

**APPROVE: no remaining blocking or high findings.**

The reviewer read the frozen source, current implementation evidence, shared-waiter callers and cleanup paths.
`git diff --check` passed. The reviewer performed no tests or mutations.

## Resolved findings

- Terminal controls share one context boundary, verify it was not exhausted and keep the relevant resource held until
  the result arrives. The independent one-second scheduling assertions are gone.
- EOF and unexpected early owner exit produce terminal diagnostics. Buffered final acknowledgement is a distinct
  permitted case.
- Mutable process state is inspected only after waiter completion. Shared cleanup callers report incomplete joins.
- Cleanup releases fixture-owned gates, joins the runner owner and observes helper absence. It refuses to signal an
  ownership-unproven numeric PID. The mismatched-PID control verifies that the unrelated held process survives, then
  its actual owner releases and joins it.

The six-second cleanup allowance is separate from 35-second fixture containment. Failure to establish absence remains
an explicit unresolved-cleanup error rather than a shutdown success claim.

## Mutation deferral

**Accepted, with limited remaining risk.** Automatic approval review rejected temporarily restoring raw-PID
`Process.Kill` because it could terminate an unrelated process. The patch was neither applied nor executed. A later
non-signaling mutation was restored without execution. Neither experiment was requested or run by the reviewer.

Acceptance rests on removal of numeric-PID signaling from the inspected cleanup path, the safe mismatched-PID
behavioral control and the recorded restriction. Sensitivity to reintroducing unsafe signaling remains unproven.
The earlier explicit-shell-`wait` survivor remains a limited observation, not proof of equivalent reaping.

Named ordering examples remain appropriate for this repair. Evidence records focused race verification passing in
14.414 seconds and lint passing. Required full and hosted gates remain with the PR owner; this approval does not
claim those gates passed.
