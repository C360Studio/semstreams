# Workload observer corrected source review

Canonical independent reviewer: `semstreams-reviewer` (`gh1421_inventory_review`).
Disposition: **APPROVE source**, conditional native admission after current focused race checks, sensitivity evidence
and command-budget confirmation. This is not execution-evidence acceptance or merge readiness.

Reviewed source at local HEAD `5a1e5531bf47b36854125510efba30ca30fa5a09` plus the implementation diff:

- Observer: `c0edd1a466c447fe10b4eff1d34cb396e10b736ce35df055182e0bdd05090dcd`.
- Harness: `8bed916461903bd0e2d769cc51159a217a4c0d0bf89159ed6bcdf95445818834`.

All four original findings are resolved: once-only lexical callback ownership before construction, coherent
post-capture marker/timestamp observation, a completed-Stop awaiting-return state, and ordinary unit-lane fake proofs.
ExactActivation remains beside the integration harness.

During correction review, the reviewer identified a scheduling race in the FailNow proof. It could exit before the
callback began, or observe a snapshot before the expected return marker. The corrected proof witnesses callback entry
at a held before-snapshot boundary before invoking the fatal assertion. It then witnesses finalizer entry and releases
and joins the callback independently, including on assertion failure. The reviewer accepted the corrected ordering.

No remaining source defect blocks the single planned native pass once the focused evidence and execution budget pass.
The accepted design's workload, native-channel ownership, deadline and semantic limits remain in force.
