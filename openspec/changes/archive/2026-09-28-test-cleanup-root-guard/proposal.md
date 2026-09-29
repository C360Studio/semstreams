# Change: Audit and guard lifecycle test cleanup roots

## Why

Unbounded terminal cleanup can turn an assertion failure into a long package timeout. Issue #1064 requires a
current, type-aware census and a regression guard so cleanup debt is explicit and cannot silently grow.
The owner placed this work in v1.0.0-beta.163 on 2026-09-28:
https://github.com/C360Studio/semstreams/issues/1064#issuecomment-5873261389.

## What Changes

- Enumerate lifecycle Stop calls and their cleanup ownership, receiver identity, and context provenance.
- Distinguish unbounded cleanup, bounded cleanup, deliberate API-contract calls, unrelated Stop methods,
  and cases that need human review.
- Guard against new unreviewed debt and stale baseline entries using stable site identities.
- Plan separately reviewable package repairs preserving component-before-substrate teardown and Stop errors.

The inventory and independent design review established the bounded analyzer contract before implementation.
The final census and exact reviewed manifest supersede the historical textual estimates.

## Boundaries

No blanket timeout substitution, weakened assertions, production lifecycle changes, or generic watchdog is
selected here. A deadline context is a cooperative bound; it does not prove Stop returns before a wall-clock limit.
Existing related work in #1293, #1411, and #1412 retains its scope and placement.

## Completion evidence

The census, exact manifest, implementation, mutation evidence, and full local preflight passed independent
review. This archive synchronizes the seven `test-cleanup-policy` requirements; the retained evidence and
remediation plan record measured costs and the existing cleanup debt that remains for separate repairs.
Hosted CI remains a separate merge gate.
