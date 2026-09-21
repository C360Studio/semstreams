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

- [ ] 1.1 Add `internal/deliverylane/deliverylane.go` from `design.md` § 5 verbatim (gofmt-clean at draft; revive
      `exported` and `package-comments` satisfied by the doc comments).
- [ ] 1.2 Add `internal/deliverylane/deliverylane_test.go` proving I1–I9 (`design.md` § 7) with a local fake
      `jetstream.ConsumeContext` and a local fake `jetstream.Msg`, including `TestSettleRefusesWithoutInvokingWork`
      (M8's primitive detector), `TestDoneIsClosedWhenNoObserverRan` (I6), and `TestObserveRefusesNilReact`;
      `-race`; `// spec:` citations name the `jetstream-consumer-policy` requirements this change modifies.

## 2. Call sites (one commit per component; each green under the per-component gate above)

- [ ] 2.1 agentic-tools: replace the four calls; `recordDeliveryOwnerFatal` stays; reaction closure captures the
      observer `ctx` for `recordHandlerError`; delete `delivery_owner.go`; `consumers []*deliverylane.Binding`; Stop
      uses `Drain`/`Closed`/`Done` and drops the `done != nil` guard.
- [ ] 2.2 agentic-model: as 2.1; move `recordDeliveryOwnerFatal` (`delivery_owner.go:50-58`) into `component.go`;
      add `reactDeliveryFatal` (the existing "Model delivery ownership lost" log line).
- [ ] 2.3 agentic-loop: heartbeat lanes → `Consume`; the settlement shape → `Settle(..., settleRetry, admission,
      "loop", settleHandlerFn)`; move `recordDeliveryOwnerFatal` (`:65-72`) into `component.go`; delete
      `runLoopDeliveryWork`.
- [ ] 2.4 agentic-dispatch (three lanes at `L3:`): two terminal lanes → `Consume` with the refuse arm; the one
      settlement lane (`user.message`) → `Settle(..., natsclient.ImmediateDeliveryRetry(), admission, "dispatch",
      c.handleUserMessage)`; one `reactDeliveryFatal` for all three lanes; delete `runDispatchDeliveryWork`.
- [ ] 2.5 agentic-governance: `Settle(..., ImmediateDeliveryRetry(), admission, "governance", handler)`; delete
      `delivery_owner.go`, `runGovernanceDeliveryWork` (`component.go:349-361`), the out-of-file `drain` (`:728-733`).
- [ ] 2.6 Tests — the **45 functions in 23 files** pinned in `inventory.md` § 4, regenerated at `3faca84f` (36/16 at
      the design base; L2/L3 added seven files, six of them under `agentic-loop`): `admission.fatal` length checks
      → `require.False(admission.Admit())` (buffering moves to 1.2);
      `binding.observerDone` → `binding.Done()`; `c.consumers[i].handle.Closed()` → `.Closed()`; the five
      `lifecycle_causal_test.go` literals `[]streamConsumerBinding{{handle: h}}` →
      `[]*deliverylane.Binding{deliverylane.NewBinding(h)}`; every string assertion in `design.md` § 8 stays
      byte-identical.
- [ ] 2.7 `git grep -n 'deliveryLaneAdmission\|newStreamConsumerBinding\|observeDeliveryLane\|consumeAdmittedDelivery\|run[A-Za-z]*DeliveryWork\|streamConsumerBinding\|observerDone' -- processor/agentic-*` returns nothing.
- [ ] 2.8 M8's production-seam detector (~10 lines, governance or dispatch): after a fatal latches a settlement
      lane, replay a second delivery into the **same** lane and assert `acks+naks+terms == 0` and the work counter
      unchanged.

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
      between mutating and testing, recorded in the PR body with commands and output.
- [ ] 4.4 `task check:push` green; PR body carries `implemented-by: <persona>`; `Closes #1341`.
- [ ] 4.5 Archive/spec sync is the last content commit.

## Out of scope (recorded, not tasks)

- The five silent-refusal lanes at `L3:` — #1342 (blocked by #1341).
- Exporting the package — future gate, `design.md` § 3.
- Anything in #1249's or L4's own scope beyond the call they make (`design.md` § 6); L4's one-line Survives edit.
- The stale lane comment at `L3:processor/agentic-dispatch/component.go:716-718` — L3's reviewer.
