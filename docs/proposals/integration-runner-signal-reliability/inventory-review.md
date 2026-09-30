# Independent inventory review

Mode: inventory review.

**INVENTORY PASS — bounded incident triage and immediate repair planning.**

Reviewed complete artifact `/private/tmp/semstreams-test-audit-20260928/inventory-checkpoint.md`, SHA-256 `1577ec4358d996d68ad0b90f74ee1ea9711d76826f36396af46319c06d658d1c`, against baseline `3dc4ccbef32e87096c6d998fde7e76e896cf2f3c`. All five component artifact hashes match. Independently ran source-pin verification: runner 88/88, async 59/59, rule-hang 88/88, policy-bound 8/8; 243/243 valid.

The inventory is sufficient to begin this bounded design because it distinguishes:

- Scheduler-sensitive synchronization limits in #1397 from actual production shutdown defects.
- The runtime-command fence defect described by #1283 from the observed cron settlement stack in CI run `36343596313`. The saved log directly shows the 1,200-second package timeout, `startedRuleProcessor` cleanup, `Processor.cleanup:1394`, and `CronScheduler.Stop:500`. It does not establish that the runtime-command fence receive caused that run.
- Test lifecycle ownership and cleanup bounds from runner containment.
- Ordinary performance cost from failed or cancelled runs. The cancelled `rule.test` process remains unattributed.
- Existing ownership under #1064, #736, #1293, #1222/PR #1406, and #1349. The saved #1293 owner comment explicitly retains rc.1 and its bounded plumbing scope.

Source inspection confirms the policy tension: narrow diagnostic observation and cleanup budgets exist, while `untilTestDeadline` inherits the package execution deadline. A larger deadline alone is not evidence of faster failure.

## Accepted inventory addendum — existing test helper surface

- `testutil/nats.go:224` declares `WaitForMessage`; it polls the existing mock client with a timeout.
- `testutil/nats.go:248` declares `WaitForMessageCount`; it polls the existing mock client and reports the observed count on timeout.
- `testutil/doc.go:121` documents a `testutil.WaitForMessage` call.
- Independent searches for `testutil.WaitForMessage` and `WaitForMessageCount(` found that documentation and the latter declaration, respectively. These searches do not establish complete caller absence: aliased imports, unqualified calls, and external adopters remain outside that conclusion.
- These helpers belong in the foundation inventory. Their existence does not establish that they satisfy current testing policy or provide a general asynchronous-test abstraction.

This pass does **not** certify an exhaustive helper inventory, typed classification of all test files, complete caller enumeration, or repository-wide compliance. My corrected `gopls references` attempt also failed with the recorded workspace-load problem and `no package metadata`; structural completeness remains unverified. The broad lexical counts remain candidate populations, not defect counts.

No new primitive is proposed, so no primitive collision verdict is needed at this stage. A later proposal introducing one must expand the owner inventory first.

No tests, code edits, or GitHub mutations were performed. This verdict authorizes proceeding to design review; it is neither a design recommendation nor owner approval.
