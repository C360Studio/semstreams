# Change: Audit and guard lifecycle test cleanup roots

## Why

Unbounded terminal cleanup can turn an assertion failure into a long package timeout. Issue #1064 requires a
current, type-aware census and a regression guard so cleanup debt is explicit and cannot silently grow.
The owner placed this work in v1.0.0-beta.163 on 2026-09-28:
https://github.com/C360Studio/semstreams/issues/1064#issuecomment-5873261389.

## Scope under investigation

- Enumerate lifecycle Stop calls and their cleanup ownership, receiver identity, and context provenance.
- Distinguish unbounded cleanup, bounded cleanup, deliberate API-contract calls, unrelated Stop methods,
  and cases that need human review.
- Design a guard that rejects new unreviewed debt and stale baseline entries using stable site identities.
- Plan separately reviewable package repairs preserving component-before-substrate teardown and Stop errors.

This is the initial claim, not an accepted analyzer design. The historical textual counts are hypotheses to
re-measure. The inventory, independent review, and design review precede implementation and spec deltas.

## Boundaries

No blanket timeout substitution, weakened assertions, production lifecycle changes, or generic watchdog is
selected here. A deadline context is a cooperative bound; it does not prove Stop returns before a wall-clock limit.
Existing related work in #1293, #1411, and #1412 retains its scope and placement.
