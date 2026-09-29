# Inventory review — round 1

Reviewer: semstreams-reviewer, /root/gh1421_inventory_review, 2026-09-29.
Base: e811399e950d4eda4bed9f140a0ff73fa6882001.
Inventory SHA-256: 940c06d1538914e9b2a1b6b2d973fcca4588279adc51667c9b13b13998fd851b.
Independent blind record: inventory-blind-review.md, SHA-256
ce0c3955b533318eabf7e391225bb3b439654d310126eff01240765175265f47.

## Blocking finding

BLOCKING inventory.md:381 — Closest existing watcher stop-and-drain owner omitted.

Section 7 enumerates generic lifecycle, Subscription and readiness shapes but misses
processor/graph-ingest/component.go:1307–1323, stopEntityStateGuardWatcher. It already handles the
watched-channel backpressure class under investigation: Stop, then continue reading Updates to closure to
release a callback holding the watcher mutex. Calls are at 1280 (cancellation) and 1297 (snapshot marker).
The reviewer independently verified gopls references at 1314:21 with full cache access: those two callers.

Add an owner-table row and line pins for Stop at 1315, draining at 1317 and both callers. State that it is
private, contextless and unbounded, not automatically reusable or a proven #1421 repair. Preserve the distinction
between KeyLister.Keys and watcher.Updates. This finding does not require migrating that existing owner.

## Nonblocking completeness notes

1. Owner harness 450–453 polls Info(ctx) under the 15-minute parent and discards its error. The five-second
   Eventually timer does not bound/join callback IO. The observed incident passed this phase.
2. Harness 485/530 compares lengths only, while graph-index spec 201–204 calls for exact match sets and exact
   convergence. The sibling predicate smoke test 456–459 sorts and compares seeded truth. Record this proof gap
   without expanding #1421 into unrelated coverage work; the inventory's existing word “counts” is truthful.

## Accepted evidence limits

The inventory accurately bounds causal unknowns, native Unsubscribe versus connection-drain behavior, unchanged
incident paths, the CAS-only MaxRetries option and the prior owner ruling. The reviewer independently verified
that git diff --stat 49a540d6 e811399e is empty for the seven named incident paths. No evidence calls for a new
production primitive, retry or timeout inflation. No tests or mutations ran during this review.

INVENTORY CHANGES REQUESTED — the missing same-class owner above is the sole blocker. Recheck the narrow addendum
against the revised artifact; no broad re-enumeration is needed.
