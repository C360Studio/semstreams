# Repair design review, round 1

Mode: pre-owner design review. Baseline: `81a4deb8`.
Reviewed artifact: `/tmp/gh1421-filtered-repair-design.md`.
SHA-256: `a72430cef29f2765ffe6044616362dea7f802d1a9d5cd331d069cf4fcc05c627`.
Reviewer: `semstreams-reviewer`, gh1421_inventory_review.

DESIGN CHANGES REQUESTED: one MEDIUM semantic conflict. Lines 50–51 preserve no-watcher constructor
`ErrNoKeysFound` success, while lines 67 and 149–154 require snapshot completion and Updates closure for all success.
Specify the exact exception and cancellation precedence in the design, normative delta and focused examples.

Otherwise the private WatchFiltered path, synchronous Stop followed by finite drain, minimal-reader boundary,
observer reconciliation and owning-result-path RED/mutation were accepted. Updates closure establishes delivery
completion, not native goroutine joining or consumer deletion. Contextless Stop remains outside the drain bound.
Use the existing `testing/synctest` pattern for ordinary expiry histories; native validation remains real time.
