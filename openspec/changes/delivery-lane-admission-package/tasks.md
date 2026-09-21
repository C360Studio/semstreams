# Tasks — delivery-lane-admission-package (#1341)

Preconditions (not tasks): PR #1338 (L3) squash-merged to `main` — it is `3faca84f`; every pin in `inventory.md`
re-derived at that commit and dispatch's lane count re-measured (`design.md` § 9) — DONE, `inventory.md` re-pin note,
`scripts/inventory-verify.sh` `pins=272 ok=272 … EXIT=0`; pre-owner design review and owner acceptance recorded on
#1341; Q2 ruling on #1341 — RULED "amend" 2026-09-19 (`design-docket.md`), so #1249 consumes the package.

Implementation order (coordinator, 2026-09-21): **dispatch is converted FIRST**, not tools, because it is the widest
copy — the only one with both the refuse arm and a settlement lane, and the only one whose constructor is wider than
`(onFatal)`. A defect in the package shape surfaces there once instead of five times. The reviewer runs on
package + dispatch before tools/loop/model/governance are touched. Task numbering below is unchanged; 2.4 runs first.

Per-component gate, used by every 2.x commit: `task lint && go test -race ./processor/agentic-<c>/... && go vet
-tags=integration ./processor/agentic-<c>/...` — the tagged vet is what compiles the **five** `//go:build integration`
files that reference deleted symbols (dispatch `terminal_settlement_integration_test.go`, governance
`delivery_settlement_integration_test.go`, loop `delivery_settlement_integration_test.go`,
`partial_publish_settlement_integration_test.go` and `terminal_failure_record_integration_test.go`); untagged
`go test` never builds them. The fifth was added by L2/L3 after the design round and is re-measured in
`inventory.md` § 4.

## 1. Package

- [x] 1.1 Add `internal/deliverylane/deliverylane.go` from `design.md` § 5 verbatim (gofmt-clean at draft; revive
      `exported` and `package-comments` satisfied by the doc comments). DONE: 228 lines, `gofmt -l` clean,
      `task lint` clean. One deviation from § 5, recorded in `design.md` § 9b: the package doc comment applies
      `design-review-2.md` MEDIUM-A — it no longer asserts `agentic/agentrun` as a PRESENT consumer, it says
      "the five agentic components today, and agentic/agentrun once #1249 adopts it". No code line differs.
- [x] 1.2 Add `internal/deliverylane/deliverylane_test.go` proving I1–I9 (`design.md` § 7) with a local fake
      `jetstream.ConsumeContext` and a local fake `jetstream.Msg`, including `TestSettleRefusesWithoutInvokingWork`
      (M8's primitive detector), `TestDoneIsClosedWhenNoObserverRan` (I6), and `TestObserveRefusesNilReact`;
      `-race`; `// spec:` citations name the `jetstream-consumer-policy` requirements this change modifies.
      DONE: 12 test functions, all RUN and PASS under `go test -race -count=1` (no skips); every fatal fixture is
      built through the production settlement path (`natsclient.SettleDeliveryWithRetry`), never by constructing a
      `DeliveryResult`. Invariant → test: I1/I2 `TestLatchClosesOnFirstOwnerStopAndIgnoresEveryLaterResult`;
      I3 `TestHealthWriterCompletesBeforeTheResultIsBuffered`; I4 `TestDrainHappensAtMostOnceAcrossObserverAndStop`;
      I5 `TestClosedLaneTouchesNothingOnARefusedDelivery`; I6 `TestDoneIsClosedWhenNoObserverRan` +
      `TestObserverExitsOnCancellationWithoutDraining`; I7 `TestSettleQuarantinesPanickingWorkWithTheOwnerNamedCause`;
      I8 `TestTerminalMethodErrorAloneKeepsTheLaneOpen`; I9 `TestRefusalDeclarerSeesEverySubjectExactlyOnce`.
      `TestConsumeRunsTheTypedHeartbeatPathAndLatchesItsResult` covers the admitted half of `Consume` through
      `natsclient.ConsumeDeliveryWithHeartbeat`.
- [x] 1.2b Internal-review MEDIUM-1: `Settle`'s doc carries the bool contract and its consequence (the zero
      result's non-nil `Err()`), and `TestSettleRefusalReturnsAZeroResultWhoseErrIsNonNil` gates it. The package
      file is 238 lines (§ 4 measured 228 on the draft).
- [x] 1.2a PBT and fuzz decisions, recorded here because `design.md` § 8 did not take them (testing discipline
      § When to Use Property-Based Testing / § mandatory fuzz targets):
      **PBT applies** — shape 3, "stateful histories where outcomes depend on operation order"; I1 and I2 are
      order-dependent and the examples fix one order each. Added `internal/deliverylane/deliverylane_prop_test.go`,
      `TestPropFirstOwnerStopResultWins`: arbitrary histories of owner-stop / clean results, expectation derived
      from the drawn history (`firstFatal` is computed from the draw, never read back), boundaries by construction
      (length 0 and "first result is fatal" are ordinary draws). 100/100 pass.
      **Concurrency is NOT given to the property**: I4's obligation is a race between the observer's drain and
      Stop's, which a sequential rapid state model cannot reach; it stays an explicit eight-goroutine `-race` test.
      **Fuzz is inapplicable**: the package parses and validates no external bytes or strings — `owner` is a
      caller-supplied literal used only in an error string, and every byte slice is handed to caller-supplied work
      unread. The grammar surfaces it composes (`jetstream.Msg`, `DeliveryResult`) are `natsclient`'s, which owns
      their fuzz obligation. No gap issue is owed because no new parsing surface is added.

## 2. Call sites (one commit per component; each green under the per-component gate above)

- [ ] 2.1 agentic-tools: replace the four calls; `recordDeliveryOwnerFatal` stays; reaction closure captures the
      observer `ctx` for `recordHandlerError`; delete `delivery_owner.go`; `consumers []*deliverylane.Binding`; Stop
      uses `Drain`/`Closed`/`Done` and drops the `done != nil` guard.
- [ ] 2.2 agentic-model: as 2.1; move `recordDeliveryOwnerFatal` (`delivery_owner.go:50-58`) into `component.go`;
      add `reactDeliveryFatal` (the existing "Model delivery ownership lost" log line).
- [ ] 2.3 agentic-loop: heartbeat lanes → `Consume`; the settlement shape → `Settle(..., settleRetry, admission,
      "loop", settleHandlerFn)`; move `recordDeliveryOwnerFatal` (`:65-72`) into `component.go`; delete
      `runLoopDeliveryWork`. **Settlement guard form (MEDIUM-2, ruled): EARLY RETURN** —
      `result, admitted := deliverylane.Settle(...)` then `if !admitted { return }`, leaving the existing branches
      byte-identical. Not the conjunct dispatch used: the early return is today's `if !admission.admit() { return }`
      shape and stays correct when L4 adds a branch below it.
- [x] 2.4 agentic-dispatch (three lanes at `3faca84f`) — FIRST, per the order above: two terminal lanes →
      `Consume` with the refuse arm; the one settlement lane (`user.message`) →
      `Settle(..., natsclient.ImmediateDeliveryRetry(), admission, "dispatch", c.handleUserMessage)`; one
      `reactDeliveryFatal` for all three lanes; `delivery_owner.go` deleted with `runDispatchDeliveryWork`,
      `streamConsumerBinding` (`component.go`) deleted, `consumers []*deliverylane.Binding`, Stop uses
      `Drain`/`Closed`/`Done` and drops the `done != nil` guard. Net: −178 lines in `component.go` +
      `delivery_owner.go`, +11 for `reactDeliveryFatal` and the two-line `Settle` call.
      **One thing the design did not say** (recorded, and the check every other consumer needs): today's
      settlement-lane body returns early when admission refuses, so its "did not settle cleanly" log is implicitly
      guarded. `Settle` returns the ZERO `DeliveryResult` on refusal, and `DeliveryResult.Err()` is NON-NIL for a
      zero value ("delivery result is incomplete for decision 0"), so the same branch must now read
      `if admitted && result.Err() != nil && !result.OwnerStopRequired()`. Without the `admitted &&` a refused
      delivery would be logged as a settlement failure. It is a call-site guard, not a package change — but loop
      and governance have the same inline shape and must be checked for it (2.3, 2.5). Corrected on internal
      review (MEDIUM-1): this line previously cited the package doc as already documenting the bool contract; that
      sentence was in `Consume`'s doc only, and is now in `Settle`'s with the consequence named and gated by
      `TestSettleRefusalReturnsAZeroResultWhoseErrIsNonNil`.
- [ ] 2.5 agentic-governance: `Settle(..., ImmediateDeliveryRetry(), admission, "governance", handler)`; delete
      `delivery_owner.go`, `runGovernanceDeliveryWork` (`component.go:349-361`), the out-of-file `drain` (`:728-733`).
      **Settlement guard form: EARLY RETURN**, as 2.3 (MEDIUM-2).
- [ ] 2.6 Tests — the **45 functions in 23 files** pinned in `inventory.md` § 4, regenerated at `3faca84f` (36/16 at
      the design base; L2/L3 added seven files, six of them under `agentic-loop`): `admission.fatal` length checks
      → `require.False(admission.Admit())` (buffering moves to 1.2);
      `binding.observerDone` → `binding.Done()`; `c.consumers[i].handle.Closed()` → `.Closed()`; the five
      `lifecycle_causal_test.go` literals `[]streamConsumerBinding{{handle: h}}` →
      `[]*deliverylane.Binding{deliverylane.NewBinding(h)}`; every string assertion in `design.md` § 8 stays
      byte-identical.
- [ ] 2.7 `git grep -n 'deliveryLaneAdmission\|newStreamConsumerBinding\|observeDeliveryLane\|consumeAdmittedDelivery\|run[A-Za-z]*DeliveryWork\|streamConsumerBinding\|observerDone' -- processor/agentic-*` returns nothing.
      PARTIAL: already returns nothing for `processor/agentic-dispatch/` (exit 1); the other four are 2.1-2.3, 2.5.
- [x] 2.8 M8's production-seam detector, on dispatch:
      `TestLatchedSettlementLaneRefusesTheNextDeliveryWithoutWorkOrSettlement` latches the `user.message` lane
      through the production quarantine path, replays a second delivery into the **same** lane, and asserts
      `acks+naks+terms == 0`, the work counter unchanged, and that the refusal is not reported as a settlement
      failure. Kills M8 (`m8d`) and the `admitted` guard mutant (`md6b`).

## 3. Guard (severable; drop on reviewer or owner call)

- [ ] 3.1 `test/contract/delivery_lane_one_home_contract_test.go`: fails if any production file outside
      `internal/deliverylane` declares a field of type `chan natsclient.DeliveryResult` — the latch's one
      unmistakable fingerprint (five hits today, all in the files this change deletes). Scoped to the latch only:
      the eleven legitimate `drainIssued` drain-once bindings (`inventory.md` § 2) are not in its reach, and the
      spec SHALL it enforces is narrowed to the latch to match.

## 4. Spec, docs, evidence

- [ ] 4.1 Spec delta `specs/jetstream-consumer-policy/spec.md` (three MODIFIED requirements, every scenario
      restated, no import path in a SHALL); `openspec validate delivery-lane-admission-package --strict` green AFTER
      L1's change archives (two of the three blocks restate L1's text and cannot validate before it).
- [ ] 4.2 `docs/concepts/33-semantic-settlement.md:89` step 4 names the in-tree home of "close admission and stop
      that exact handle"; `docs/operations/migration-restart-safe-nats-client.md:87-88` gains one sentence saying the
      reaction is internal for now and what an adopter builds meanwhile.
- [ ] 4.3 Mutation evidence per `design.md` § 8 (M1–M8), each by `cp` backup + checksum, `[applied]` printed
      between mutating and testing, recorded in the PR body with commands and output. PARTIAL — package and
      dispatch done; every restore verified by md5 equal to the pre-mutation sum, and `git diff` empty after each.
      Package (`go test -race -count=1 ./internal/deliverylane/...`): M1 delete `admission.Latch(result)` in
      `Consume` → KILLED; M2 delete `a.onFatal(result)` in `Latch` → KILLED; M3 delete `binding.Drain()` in
      `Observe` → KILLED; M4 delete `admission.refuse(msg)` in `Consume` → KILLED; M5 `drainOnce.Do` → bare
      `handle.Drain()` → KILLED; M7 delete the `recover` block → INVALID MUTANT (build failure: `fmt` becomes
      unused), re-run as `m7b`, a compiling mutant that also drops the import → KILLED; M8 delete the
      `admission.Admit()` guard in `Settle` → KILLED.
      Dispatch, CALL-level (`go test -race -count=1 ./processor/agentic-dispatch/...` unless noted): M6 delete the
      `deliverylane.Observe(...)` call on the `agent.complete` lane → SURVIVES the untagged suite, KILLED under
      `-tags=integration` by `TestIntegrationProductionCallbackUnknownPublishQuarantinesExactLane`; delete the
      `admitted` guard at the `user.message` lane → KILLED by 2.8's test; M8 and M3 re-run against the dispatch
      suite → both KILLED by dispatch tests.
      Dispatch, WIRING-level: passing `nil` as the `agent.complete` lane's `onFatal` SURVIVED both suites until
      this change added three assertions to the existing integration test (health is degraded, status is
      `terminal delivery ownership lost`, `LastError` names `agent.complete`) — then KILLED. Passing `nil` as that
      lane's `onRefused` SURVIVES both suites and is NOT fixed here: see the residual in `design.md` § 9c.
- [ ] 4.4 `task check:push` green; PR body carries `implemented-by: <persona>`; `Closes #1341`.
- [ ] 4.5 Archive/spec sync is the last content commit.

## Out of scope (recorded, not tasks)

- The five silent-refusal lanes at `L3:` — #1342 (blocked by #1341).
- Exporting the package — future gate, `design.md` § 3.
- Anything in #1249's or L4's own scope beyond the call they make (`design.md` § 6); L4's one-line Survives edit.
- The stale lane comment at `L3:processor/agentic-dispatch/component.go:716-718` — L3's reviewer.
