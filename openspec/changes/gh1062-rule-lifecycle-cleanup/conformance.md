# Implementation conformance: Controlled Rule cleanup and abort observation

Status: mandatory-review findings corrected; replacement focused evidence passed on its first attempt.

Accepted authority:

- Compact inventory: `docs/proposals/gh1062-compact-lifecycle-lane-inventory.md`, independently reviewed
  `INVENTORY PASS` at SHA-256 `f33782076205b58504124ddfe2fb391cc70073a92f6c2b17f6c99bebac5820ed`.
- Compact design: `design.md`, independently reviewed and owner accepted on 2026-08-23 at SHA-256
  `10fae45ccada66c38092b02dafe6f72b6081b54e4d6c18b290c8ee3d3e21a809`.
- The earlier nil-abort lifecycle amendment is historical evidence only and is not authority for this target.

## Ruling-to-implementation map

| Ruling | Implementation evidence | Result |
|---|---|---|
| Controlled shutdown keeps Start and NATS live through Stop | Both controlled readiness cleanups use registration LIFO and assert authority/substrate before bounded Stop. | CONFORMS |
| Controlled Stop returns nil | Both readiness cleanups treat any Stop error as a test failure. | CONFORMS |
| Abort Stop is synchronous bounded best effort | Standard lifecycle and Rule abort proofs call Stop directly under fresh five-second contexts and permit nonnil results. | CONFORMS |
| Abort preserves accurate deadline identity | Both proofs require `errors.Is(stopErr, stopCtx.Err())` whenever their exact Stop context has ended; the Rule proof has no text gate. | CONFORMS |
| Abort makes no full-join or leak-freedom promise after expiry | GoDoc and component-lifecycle truth state the narrower lane explicitly. | CONFORMS |
| No replacement authority, detached cleanup, retry, or second rejoin | Production cleanup receives no new context, task, timer, retry, or lifecycle state. | CONFORMS |
| No abort-to-nil production behavior | Rule uses its direct runtime-command barrier result and direct native watcher Stop outcomes; the temporary interpreter is removed. | CONFORMS |
| Preserve #1048 evidence | The completed archived change remains unchanged and current component-lifecycle truth retains its projection. | CONFORMS |

## Superseded evidence

The prior abort-to-nil exploration established only that abort cleanup can accurately return native terminal and
deadline errors. Those outcomes are permitted observations, not conformance failures.

The previously recorded abort 20/20 gate used a flawed proof: the body and `t.Cleanup` could invoke Stop twice, and
deadline identity was checked only when error text contained a deadline string. Its result and the dependent focused
gate ledger are superseded and are not final evidence. The replacement proof owns one synchronous Stop operation
through `sync.Once` and checks exact caller-context identity unconditionally whenever that context has ended.

## Replacement evidence

All replacement commands below passed on their first attempt after the mandatory-review correction.

1. Focused portable lifecycle and Rule unit/race gate:

   ```text
   go test -race ./component ./gateway/http ./input/udp ./processor/rule \
     -run '^(TestHTTPGateway_ComprehensiveLifecycle|TestUDPInput_ComprehensiveLifecycle)/PortableFloor/AcceptedStartParentCancellation$|^(TestAwaitEntityBorrowSettlement_CallerCancellationCancelsStartAuthority|TestRuleRuntimeCoordinatorUsesExactStartContextAndFences|TestConfigManagerStopDisposesSuccessfulWatcherReturnedAfterCancellation)$' \
     -count=1 -timeout=60s
   ```

   Result: PASS in 6.623 s wall time. Package results were `component` 1.450 s (no selected tests),
   `gateway/http` 1.665 s, `input/udp` 1.453 s, and `processor/rule` 1.589 s.

2. Integration-tagged GraphIndex standard abort proof:

   ```text
   go test -race -tags=integration ./processor/graph-index \
     -run '^TestGraphIndex_ComprehensiveLifecycle/PortableFloor/AcceptedStartParentCancellation$' \
     -count=1 -timeout=60s
   ```

   Result: PASS in 4.890 s wall time; Go reported 2.280 s.

3. Controlled Rule real-NATS proofs:

   ```text
   go test -v -race -tags=integration ./processor/rule \
     -run '^(TestIntegration_RuleReadiness_EmptyReplayIsAuthoritativelyNothingToDo|TestIntegration_RuleReadiness_NonEmptyReplayReportsScope)$' \
     -count=1 -failfast -timeout=60s
   ```

   Result: PASS in 5.812 s wall time; Go reported 2.744 s. Both controlled proofs kept Start authority and NATS live
   through Stop and observed nil Stop results.

4. Corrected Rule accepted-parent abort observation:

   ```text
   go test -v -race -tags=integration ./processor/rule \
     -run '^TestIntegration_RuleStopAfterAcceptedStartParentCancellation$' \
     -count=20 -failfast -timeout=90s
   ```

   Result: PASS, 20/20, in 10.721 s wall time; Go reported 9.681 s. Observed native outcome classes were nil,
   `nats: invalid subscription`, `context canceled`, and joined `context canceled` plus
   `nats: invalid subscription`. Every iteration preserved a live NATS substrate, remained synchronous and bounded,
   and neither panicked nor lost caller-context identity.

5. Strict OpenSpec validation:

   ```text
   openspec validate gh1062-rule-lifecycle-cleanup --strict --no-interactive
   openspec validate --all --strict --no-interactive
   ```

   Result: PASS on first attempt after the correction. The focused change was valid in 0.125 s; the all-artifact
   gate reported 57 passed and 0 failed in 0.175 s.

6. Tracked whitespace/diff validation:

   ```text
   git diff --check
   ```

   Result: PASS with no output in less than 0.001 s. A final post-ledger rerun also passed with no output.

No repository-wide Go gate, hosted-CI, tag-readiness, or merge-readiness claim is made.
