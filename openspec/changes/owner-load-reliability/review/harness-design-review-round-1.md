# Harness design review — round 1

Reviewer: semstreams-reviewer, /root/gh1421_inventory_review, 2026-09-29.
Reviewed SHA-256: a91795b8f8a453dcf853b2dc753a35500c4b2ee82fc610b1433ea2ff9551bffc.

HIGH design.md:89 — Terminal-expiry contract lacks a proof and fixture recovery. Add one cancellation-resistant
callback with independently releasable gate and short private proof budget. Observe original sentinel plus bound
error, named unresolved ownership, and no successor admission; pre-registered fixture recovery releases/joins
that callback. Read owned state only after completion.

HIGH design.md:104 — First-error cancellation mutant can be rescued by lexical finalizer cancellation and remain
behaviorally equivalent. Instead omit one owner's join while the proof holds it after cancellation; assert the
helper cannot return before completion. Fixture recovery must release/join it; require assertion failure, not timeout.

Dispatcher closure is compatible with keyed_dispatcher.go:49–52 and :59–60. Clarify exact single closure after sole
Submit loop ceased, no close/Submit race or instance reuse. Drain published results after done before missing-result
classification. Private helper scope, preserved framework ceiling and local diagnostics are otherwise sound.
PBT named schedules suffice after terminal-expiry proof is added. Historical cause remains unresolved.

DESIGN CHANGES REQUESTED — the two proof mechanics above. No source edits or tests ran in review.
