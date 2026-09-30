# Recurrence inventory review, round one

Reviewer: semstreams-reviewer, /root/gh1421_inventory_review, 2026-09-29.
Reviewed inventory SHA-256: 04577036c2e8f51a11fef48ff17b2f5da7d0fede37c23c41397adb20b7ff123a.

INVENTORY CHANGES REQUESTED. One blocking omission: SDK resetOrderedConsumer asynchronously invokes
DeleteConsumer at js.go:2279, and KV watchers explicitly select OrderedConsumer at jetstream/kv.go:1305.
The inventory must distinguish this deletion origin from synchronous terminal deletion; a delete request alone
cannot establish an added return-path wait.

All other inspected facts matched: exact recurrence bytes and source, fifth attempt before concurrency, 17-goroutine
post-return snapshot and its limits, discarded Stop result, and reachable-but-unattributed five-plus-five timing.
No tests, edits or mutations were performed by the reviewer. Root materialized the narrow source-confirmed correction
before re-review.
