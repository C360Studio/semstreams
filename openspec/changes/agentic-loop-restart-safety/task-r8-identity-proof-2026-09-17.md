# R8 identity proof-only slice

Baseline: `68c14c8eb25c512e988f740cbf7ea14b6815976f`.
Authority: owner comment `5712921768`, accepted decision `52742eff…` and inventory `fb937d0e…`.
Architect handoff, materialized by the root. No production change is proposed.

## Ownership and scope

Developer changes tests only in the existing dispatch/loop recovery suites. No production fixes, public test seams,
new clock, storage, or admission implementation. Preserve unrelated work and the reviewed nil-publisher slice.

## Dispatch mapping

Reuse `TestIntegrationUserMessageReplayAfterTaskCommitKeepsOneLogicalTask` in
`processor/agentic-dispatch/restart_identity_integration_test.go`.

It already withholds source ACK, replaces the connection, redelivers the same source stream sequence, and republishes
task bytes after the duplicate window. Extend its measured evidence to actual source/evidence stream policy and
publication timestamps/order. Use DiscardNew for the accepted-policy case; preserve the matching-evidence identity
assertion.

For a retention counterexample, demonstrate evidence expiry through declared MaxAge while the original source remains
retained and redeliverable. Do not purge/delete it or inject arbitrary absence. Observe typed absence rather than
sleeping an assumed expiry duration. Keep source/evidence overrides distinct.

This fixture manually creates its source consumer and calls the production handler. Its claim is real-server
redelivery plus production identity behavior, not startup-admission coverage. An unsafe-policy case demonstrates the
obligation the unfinished admission must enforce; it does not by itself prove the final admitted contract defective.

## Loop task authority and request

Start at unit tier using existing `settlementBucket`, `settlementEvidence`, and `handleTaskMessage` in
`processor/agentic-loop/settlement_recovery_test.go`.

Preserve the partial-birth control `TestColdTaskRedeliveryWithoutRequestRebuildsFromTaskAndPreservesLoop`. Add only the
missing contrasts: retained terminal authority suppresses work; progressed nonterminal authority must not silently
become a reconstructed initial request merely because required request evidence is absent. Observe
publication/request identity, not private helper counts alone.

Connect any failing absence case to actual production retention and internal-republication ordering. Use existing
replacement/native seams only where server behavior is necessary to establish reachability. A fake absent key proves
the branch, not that production TTL can cause it. Do not shorten loop TTL below the enforced 24h and call that a
production-admissible configuration. A measured publication-order/expiry relationship can establish the temporal
counterexample without a 24-hour test.

## Evidence classification and stop

Report each result as: existing control passes; demonstrated production counterexample; unfinished admission
requirement; or unproven reachability.

A counterexample identifies the original source sequence, production handler path, actual resolved storage
identities/policies, internal publication ordering, and harmful outcome. Administrative purge, deleted buckets, and
arbitrary expired caller resubmission do not satisfy that definition.

Run focused race tests first. Reuse existing native infrastructure only when necessary; no broad E2E or new container
family for this diagnostic slice. Stop at the first concrete production counterexample requiring a design choice.
Return evidence before modifying runtime behavior.

Any additional native-container starts or expanded baseline runtime require explicit owner/reviewer approval before
execution; otherwise extend/reuse the existing fixture at unchanged startup cost.

The architect's structural test-discovery attempt through gopls failed on sandboxed Go-cache access. Bounded text
fallback located the named existing tests, which were read before the handoff. The architect ran no tests or edits.
