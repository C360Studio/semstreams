# Integration runner signal reliability (#1397)

This is the implementation record for the owner-approved #1397 test repair. The owner approved proceeding on
2026-09-28 after the bounded inventory and focused plan passed independent review.

## Scope and ownership

Codex owns `test/testinfra` runner synchronization, owned-process cleanup and the relevant testing examples.
Claude may independently claim #1283, which owns production rule shutdown. Its runtime-command fence defect and
observed cron settlement failure require distinct causal evidence. PR #1404 retains its boot-fixture ownership.
Heavy integration and E2E validation remains serialized across the shared host.

No runner production behavior, CI pipeline, generic testing framework, broad cleanup census or new exported API is
planned here. The test must preserve exact reap-before-lock-release proof. Removing a three-second scheduling guess
must not make the package alarm the first effective failure boundary.

The broader audit is retained as historical decision evidence, not additional implementation scope. #1064 owns the
repository-wide cleanup guard, #736 owns container pressure, #1349 owns KV fake consolidation, and #1293 retains
pipeline follow-ups at rc.1. The owner requested those gaps recorded rather than absorbed into this PR.

## Accepted evidence

- [Complete inventory checkpoint](inventory-checkpoint.md), baseline `3dc4ccbef32e87096c6d998fde7e76e896cf2f3c`,
  SHA-256 `1577ec4358d996d68ad0b90f74ee1ea9711d76826f36396af46319c06d658d1c`.
- [Independent inventory review](inventory-review.md) and [adopter inventory](adopter-inventory.md), with its
  [independent addendum review](adopter-inventory-review.md).
- [Focused repair plan](repair-plan.md) and [independent design review](design-review.md).

These are exact historical copies; absolute scratch paths inside them identify the reviewed originals. This directory
preserves their contents beyond the scratch-directory lifetime. The pre-owner status in those records is superseded by
this session's explicit owner approval to implement #1397, with #1283 as Claude's independent lane.

## Verification obligations

1. Healthy delayed progress passes; missing signal and premature child exit fail diagnostically within a declared bound.
2. Cleanup terminates and reaps owned processes; a live descendant retaining output cannot turn cleanup into a hang.
3. Premature lock release and broken reaping remain detected through the real runner script and controlled toolchain.
4. Focused race checks and applicable mutation sensitivity precede required final gates.

The numerical containment bound needs a recorded basis before implementation acceptance. Named deterministic schedule
examples address these ordering obligations; broader state histories require revisiting the existing PBT decision.
No implementation or new verification result is claimed by the initial draft-PR claim commit.

## Implementation and review

The [implementation evidence](evidence/implementation-evidence.md) records the controlled delayed-progress regression,
terminal-signal cases, cleanup ownership checks, race/lint commands and mutation limitations. The final source reviewed
on 2026-09-28 has MD5 `87186d432ecdb43e0462b87b9974aea7`; production runner behavior is unchanged.

Independent review found and corrected terminal waits, incomplete-join assumptions, a replacement scheduling guess and
unsafe numeric-PID cleanup authority. See [first review](implementation-review-1.md),
[second review](implementation-review-2.md), [third review](implementation-review-3.md),
[convergence check](convergence-check.md) and [final probe approval](implementation-review-4.md).
The final review accepts the explicitly recorded unsafe-signaling mutation deferral; it does not claim that experiment
ran. The 35-second fixture ceiling and separate six-second cleanup ceiling contain failures without relying on the
package alarm. The [final local gate passed](verification.md); hosted results for the committed candidate belong
in the PR record.

## Record choice

This is a test-only correction to evidence of an existing runner contract. Under
`docs/contributing/06-openspec-change-discipline.md`, the issue and this proof record carry the work; no runtime or
adopter-visible contract delta is proposed for OpenSpec.
