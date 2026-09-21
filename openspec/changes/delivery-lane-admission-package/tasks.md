# Tasks — delivery-lane-admission-package (#1341)

Preconditions (not tasks): PR #1338 (L3) squash-merged to `main` — it is `3faca84f`; every pin in `inventory.md`
re-derived at that commit and dispatch's lane count re-measured (`design.md` § 9) — DONE, `inventory.md` re-pin note,
`scripts/inventory-verify.sh` `pins=272 ok=272 … EXIT=0`; pre-owner design review and owner acceptance recorded on
#1341; Q2 ruling on #1341 — RULED "amend" 2026-09-19 (`design-docket.md`), so #1249 consumes the package.

**`scripts/inventory-verify.sh` is RED at the implemented head and that is correct — do not re-pin it.** The pins are
pre-change evidence taken at `3faca84f`, so the change landing is exactly what makes them move: at `13a76b63` the
verifier reports `pins=272 ok=98 moved=16 ambiguous=2 drift=156`, and the drifted paths are the five
`delivery_owner.go` files this change deletes plus the five `component.go` it rewrites. Re-pinning would destroy the
evidence the design was built on. It is not a CI gate; `task check:push` does not run it.

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

- [x] 2.1 agentic-tools: replace the four calls; `recordDeliveryOwnerFatal` stays; reaction closure captures the
      observer `ctx` for `recordHandlerError`; delete `delivery_owner.go`; `consumers []*deliverylane.Binding`; Stop
      uses `Drain`/`Closed`/`Done` and drops the `done != nil` guard. DONE, 1:1 with the design; nothing the design
      did not say. Wiring mutants on the one lane (`tool.execute`): the `Observe` CALL and `nil onFatal` both
      SURVIVED the untagged AND `-tags=integration` suites, because every test in `delivery_owner_test.go` builds
      its own admission and observer. Closed the way dispatch's was: the test that already drives production
      `setupConsumer`, `TestToolsDeliveryPolicyUsesExactTargetConfigurationBeforeAcquisition`, now captures the
      callback it bound and drives ONE unprovable delivery through it, asserting health degraded, status
      `delivery ownership lost`, `LastError`, and `drains == 1`. Both mutants then KILLED. `nil onRefused` still
      SURVIVES — recorded in 4.3, not fixed (coordinator ruling).
- [x] 2.2 agentic-model: as 2.1; move `recordDeliveryOwnerFatal` (`delivery_owner.go:50-58`) into `component.go`;
      add `reactDeliveryFatal` (the existing "Model delivery ownership lost" log line). DONE, 1:1 with the design;
      nothing the design did not say. Wiring mutants on the one lane: the `Observe` CALL and `nil onFatal` are BOTH
      KILLED by `TestModelSetupWiresMetadataFailureToAcquiredOwner` — the detector `design.md` § 8 M6 named, and the
      only one of the five components where the design's prediction held without new assertions. The lane declares
      no refusals (nil `onRefused`), so there is nothing to mutate there (#1342).
- [x] 2.3 agentic-loop: heartbeat lanes → `Consume`; the settlement shape → `Settle(..., settleRetry, admission,
      "loop", settleHandlerFn)`; move `recordDeliveryOwnerFatal` (`:65-72`) into `component.go`; delete
      `runLoopDeliveryWork`. **Settlement guard form (MEDIUM-2, ruled): EARLY RETURN** —
      `result, admitted := deliverylane.Settle(...)` then `if !admitted { return }`, leaving the existing branches
      byte-identical. Not the conjunct dispatch used: the early return is today's `if !admission.admit() { return }`
      shape and stays correct when L4 adds a branch below it.
      DONE. Also relocated `recordDeliveryOwnerFatal` into `component.go` with a doc comment naming why it runs
      synchronously; kept the `if admission != nil` guard around `Observe` byte-for-byte (both branches assign it,
      so the guard is dead today — removing it would be a behaviour change this change does not owe). Wiring
      mutants, both lanes: the `Observe` CALL, `nil onFatal` on the heartbeat lane and `nil onFatal` on the
      settlement lane are ALL KILLED by existing tests (`TestLoopSetupWiresMetadataFailureToAcquiredOwner`,
      `TestLoopApprovalPanicProductionCallbackQuarantinesExactOwner`,
      `TestLoopCancellationUnknownPublicationQuarantinesWithoutReleasingTransientState`) — loop is the
      best-covered of the five. The NEW early-return guard SURVIVED at first, so
      `TestLoopApprovalPanicProductionCallbackQuarantinesExactOwner` now replays a second delivery into the same
      latched lane and asserts no terminal method and no "did not settle cleanly" report; then KILLED. Neither
      lane declares refusals, so there is no `onRefused` to mutate (#1342).
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
- [x] 2.5 agentic-governance: `Settle(..., ImmediateDeliveryRetry(), admission, "governance", handler)`; delete
      `delivery_owner.go`, `runGovernanceDeliveryWork` (`component.go:349-361`), the out-of-file `drain` (`:728-733`).
      **Settlement guard form: EARLY RETURN**, as 2.3 (MEDIUM-2).
      DONE, 1:1 with the design; nothing the design did not say beyond the `admitted` guard 2.4 recorded, which the
      ruled early-return form supplies. Governance is the only consumer with no `run*DeliveryWork` wrapper to
      delete — its panic recovery was inline at `component.go:349-361` and is now the package's — and the only one
      whose observer needed a closure to carry `port.Name` into the reaction. Wiring mutants on the three lanes
      (one construction serves all three): the `Observe` CALL and `nil onFatal` are BOTH KILLED by
      `TestGovernanceAllowedPublicationFailureQuarantinesExactOwner` and
      `TestGovernanceProductionCallbackPanicLatchesFirstFatalAndDrainsExactOwner`. The NEW early-return guard
      SURVIVED at first, closed the same way loop's was: that panic test now replays a second delivery into the
      same latched lane and asserts no terminal method and no "Governance delivery did not settle cleanly"; then
      KILLED. The lanes declare no refusals (nil `onRefused`), so there is nothing to mutate there (#1342).
      Package-level re-runs against this suite: `m7g` (drop `runWork`'s recover) KILLED; `m8g` (drop `Settle`'s
      `Admit()` guard) SURVIVED — the same reason it survives loop, recorded in 4.3.
- [x] 2.6 Tests — the **45 functions in 23 files** pinned in `inventory.md` § 4, regenerated at `3faca84f` (36/16 at
      the design base; L2/L3 added seven files, six of them under `agentic-loop`): `admission.fatal` length checks
      -> `require.False(admission.Admit())` (buffering moves to 1.2);
      `binding.observerDone` -> `binding.Done()`; `c.consumers[i].handle.Closed()` -> `.Closed()`; the five
      `lifecycle_causal_test.go` literals `[]streamConsumerBinding{{handle: h}}` ->
      `[]*deliverylane.Binding{deliverylane.NewBinding(h)}`; every string assertion in `design.md` § 8 stays
      byte-identical.
      DONE, and checked two ways rather than by eye. (a) Exactly **23 of 23** pinned files are touched on this
      branch: `git diff --name-only 3faca84f..HEAD -- 'processor/agentic-*/*_test.go' | wc -l` = 23, and the list
      equals the census file set. (b) Every spelling the census pinned is gone, with stderr visible:
      `git grep -nE 'admission\.(fatal|admit|latch|refuse)|\.handle\.Closed' -- 'processor/agentic-*'` exits 1,
      **with no `\b`** — this line first recorded the same expression with a trailing `\b`, which `git grep -E`
      silently matches nothing for, so the `admission.` half of that sweep was vacuous AS RECORDED. Re-run
      without it on the same tree: still exit 1, and the identical expression on `main` returns 36 hits, so the
      sweep is demonstrably non-vacuous. The seven-symbol sweep in 2.7 carries no `\b` and was always sound; it
      also exits 1 — so all 45 pinned references were converted by construction, not by a count that could
      miscount. Four tests gained assertions beyond the mechanical conversion; each is
      recorded against the mutant that demanded it in 2.1, 2.3, 2.4/2.8 and 2.5, never as a silent addition.
- [x] 2.7 `git grep -n 'deliveryLaneAdmission\|newStreamConsumerBinding\|observeDeliveryLane\|consumeAdmittedDelivery\|run[A-Za-z]*DeliveryWork\|streamConsumerBinding\|observerDone' -- processor/agentic-*` returns nothing.
      DONE: exit 1 (no match) across all five packages with stderr visible.
- [x] 2.8 M8's production-seam detector, on dispatch:
      `TestLatchedSettlementLaneRefusesTheNextDeliveryWithoutWorkOrSettlement` latches the `user.message` lane
      through the production quarantine path, replays a second delivery into the **same** lane, and asserts
      `acks+naks+terms == 0`, the work counter unchanged, and that the refusal is not reported as a settlement
      failure. Kills M8 (`m8d`) and the `admitted` guard mutant (`md6b`).

## 3. Guard (severable; drop on reviewer or owner call)

- [x] 3.1 `test/contract/delivery_lane_one_home_contract_test.go`: fails if any production file outside
      `internal/deliverylane` declares a field of type `chan natsclient.DeliveryResult` — the latch's one
      unmistakable fingerprint (five hits today, all in the files this change deletes). Scoped to the latch only:
      the eleven legitimate `drainIssued` drain-once bindings (`inventory.md` § 2) are not in its reach, and the
      spec SHALL it enforces is narrowed to the latch to match.
      DONE, kept (it did not fight). Implemented as designed: a `go/parser` AST scan of non-test `.go` files,
      matching a STRUCT FIELD whose type is a channel of `natsclient.DeliveryResult` in any direction. Final
      fingerprint count tree-wide: **one production declaration**, `internal/deliverylane/deliverylane.go:30`
      (five before this change). The three remaining hits are all `make(chan ...)` in tests, which the scan
      exempts by design.
      Two things the task line did not say, both found by mutating the guard itself:
      (a) the first draft read only the FIRST natsclient import spec, so a second, ALIASED import of the same
      path slipped past (`gd2` SURVIVED). The scan now collects every local name for the path; `gd2b` KILLED.
      (b) a detector that stops matching would pass over an empty tree forever, so the test asserts non-vacuity:
      it must find the home's own latch, and `t.Fatal`s naming the detector — not the tree — when it does not.
      Guard mutation evidence (`go test -count=1 ./test/contract/ -run TestDeliveryLaneLatchHasOneHome`; each
      file restored from a `cp` backup with md5 verified equal and `git diff` empty):
      `gd1` add `fatal chan natsclient.DeliveryResult` to `agentic-dispatch`'s `Component` → KILLED;
      `gd2` the same field through a second, aliased import → SURVIVED → detector widened → `gd2b` KILLED;
      `gd3` retype the home's own field to `chan *natsclient.DeliveryResult` → INVALID MUTANT (build failure, not
      test sensitivity), re-run as `gd3b`, a compiling variant that spells it through `type latched =
      natsclient.DeliveryResult` → KILLED by the non-vacuity assertion.
      Recorded bound: the scan matches the fingerprint AS WRITTEN (AST, not types), so a local type alias outside
      the home would evade it. Deliberate — this catches drift, not an adversary, and a whole-tree type-check
      (the `packages.Load` shape `context_ownership_contract_test.go` uses) is more machinery than the design
      asked for. The non-vacuity assertion is what keeps the bound honest.

## 4. Spec, docs, evidence

- [x] 4.1 Spec delta `specs/jetstream-consumer-policy/spec.md` (three MODIFIED requirements, every scenario
      restated, no import path in a SHALL); `openspec validate delivery-lane-admission-package --strict` green AFTER
      L1's change archives (two of the three blocks restate L1's text and cannot validate before it).
      DONE; the delta needed no content change — it was written for exactly this landing. L1 archived before the
      implementation base, so the design-phase caveat is discharged; the delta's own header note now records that
      `--strict` is green at `3faca84f` rather than predicting a failure that no longer happens. `grep` for
      `deliverylane` in the delta returns nothing: no import path in any SHALL, as designed.
- [x] 4.2 `docs/concepts/33-semantic-settlement.md:89` step 4 names the in-tree home of "close admission and stop
      that exact handle"; `docs/operations/migration-restart-safe-nats-client.md:87-88` gains one sentence saying the
      reaction is internal for now and what an adopter builds meanwhile.
      DONE. The concepts doc gains a paragraph under step 4 naming `internal/deliverylane`, the three things it
      provides, the lifecycle authority it does NOT hold, and the contract test that keeps it the only home. The
      migration doc gains a paragraph that says plainly it is **not exported**, promises no export, and — the part
      an adopter actually needs — names the four properties to hold while building the reaction themselves: the
      latch closes once and keeps the FIRST owner-stop result; the health/log write completes before that result
      is buffered for the observer, so it is complete before the FATAL observer drains the handle — and it orders
      nothing against the owner's own Stop; the drain targets the exact acquired handle and no sibling; the handle drains once however many
      results demand it. Plus the refusal behaviour a closed lane owes. Both kept under the 120-column convention.
- [x] 4.3 Mutation evidence per `design.md` § 8 (M1-M8), each by `cp` backup + checksum, `[applied]` printed
      between mutating and testing, recorded in the PR body with commands and output. COMPLETE. Every restore was
      verified by md5 equal to the pre-mutation sum, with `git diff` empty after each; `git stash`, `git checkout`
      and `git restore` were used at no point.
      **Package** (`go test -race -count=1 ./internal/deliverylane/...`): M1 delete `admission.Latch(result)` in
      `Consume` -> KILLED; M2 delete `a.onFatal(result)` in `Latch` -> KILLED; M3 delete `binding.Drain()` in
      `Observe` -> KILLED; M4 delete `admission.refuse(msg)` in `Consume` -> KILLED; M5 `drainOnce.Do` -> bare
      `handle.Drain()` -> KILLED; M7 delete the `recover` block -> INVALID MUTANT (build failure: `fmt` becomes
      unused), re-run as `m7b`, a compiling mutant that also drops the import -> KILLED; M8 delete the
      `admission.Admit()` guard in `Settle` -> KILLED.
      **Dispatch, CALL level** (`./processor/agentic-dispatch/...` unless noted): M6 delete the
      `deliverylane.Observe(...)` call on the `agent.complete` lane -> SURVIVES the untagged suite, KILLED under
      `-tags=integration` by `TestIntegrationProductionCallbackUnknownPublishQuarantinesExactLane`; delete the
      `admitted` guard at the `user.message` lane -> KILLED by 2.8's test; M8 and M3 re-run against the dispatch
      suite -> both KILLED by dispatch tests.
      **Wiring level, every lane of every component** — the mutant the design did not call for, and the one that
      found real gaps. `nil onFatal` per lane, and deleting the `Observe` CALL per component:

      | component | lane | `Observe` call | `nil onFatal` | settlement guard |
      |---|---|---|---|---|
      | dispatch | `agent.complete` | KILLED (integration only) | SURVIVED -> KILLED (3 new assertions) | n/a |
      | dispatch | `agent.failed` | SURVIVED -> KILLED (new detector) | SURVIVED -> KILLED (new detector) | n/a |
      | dispatch | `user.message` | KILLED | KILLED | KILLED by 2.8 |
      | tools | `tool.execute` | SURVIVED -> KILLED | SURVIVED -> KILLED | n/a |
      | loop | heartbeat | KILLED | KILLED | n/a |
      | loop | settlement | KILLED | KILLED | SURVIVED -> KILLED (same-lane replay) |
      | model | metadata | KILLED | KILLED | n/a |
      | governance | 3 ports | KILLED | KILLED | SURVIVED -> KILLED (same-lane replay) |

      Dispatch has three distinct `Observe` call sites and three distinct `onFatal` writers
      (`recordDeliveryOwnerFatal`, `recordAgentCompleteFatal`, `recordAgentFailedFatal`), so all six were mutated
      separately rather than assumed to share a fate — and they did not.
      **`agent.failed` had no wiring coverage at all, and now does.** Deleting its `Observe` call (`dm3`) and
      nulling its `onFatal` (`dm4`) both SURVIVED the untagged suite AND `-tags=integration -p 2` (81.1s / 79.4s):
      no test in the package walked that lane to a fatal through production `setupSubscriptions`.
      `TestTerminalLaneFatalHealthFailsClosedIndependently` covers `recordAgentFailedFatal` by calling it directly,
      and `port_overrides_test.go` covers the lane's configuration; neither reaches the wiring. The remedy the
      coordinator prescribed had no target, because `agent.complete`'s detector
      (`TestIntegrationProductionCallbackUnknownPublishQuarantinesExactLane`) had no `agent.failed` analogue —
      **so the analogue was written** (coordinator ruling 2026-09-21: where no existing test reaches a lane this
      change rewires, the proof must be supplied, or a rewired lane lands with zero sensitivity).
      `TestIntegrationProductionCallbackUnknownPublishQuarantinesExactFailedLane` drives the real `agent.failed`
      lane on the production seam to an unknown terminal publication and asserts what only THAT lane's wiring can
      produce: arrival on `terminalDeliveryDoneFn` (which the callback does not call once the result requires owner
      stop — only `reactDeliveryFatal` does, from the observer), the drain of `c.consumers[2]` with both siblings
      still holding their handles, and `ErrorCount == 1` with `LastError` naming `agent.failed delivery ownership
      lost` and NOT `agent.complete` — reachable only through `recordAgentFailedFatal`, which writes a field of its
      own. Re-run: `dm3` KILLED (82.9s), `dm4` KILLED (78.5s), each failing ONLY the new test. The gap was
      pre-existing, not introduced: `inventory.md` § 4 pins the same two wiring calls at `3faca84f` with the same
      arguments (`component.go:659`, `:675`) and the 45-function census lists nothing reaching them.
      `dr2` (`nil onRefused` on this lane) was re-run against the new detector and still SURVIVES (79.3s) — the
      detector drives a fatal, not a refusal. Recorded, not fixed (#1342).
      The two SURVIVED -> KILLED wiring pairs (dispatch `agent.complete`, tools) were closed inside the existing
      test that already walks that lane to a fatal, by asserting health degraded, status, and `LastError`; the two SURVIVED -> KILLED
      settlement guards (loop, governance) were closed by replaying a second delivery into the SAME latched lane
      and asserting no terminal method and no "did not settle cleanly" report. Loop and model needed no new
      assertions at the wiring level: loop is the best-covered of the five, and model's detector is the one
      `design.md` § 8 M6 named by hand.
      **`nil onRefused`, per lane — SURVIVORS, recorded not fixed** (coordinator ruling; obligation posted on
      #1342). Only three lanes pass a non-nil `onRefused` today, so only three are mutable:
      `dispatch/agent.complete` -> SURVIVED both suites; `dispatch/agent.failed` -> SURVIVED both suites, and
      re-confirmed SURVIVING (79.3s) after the new fatal detector landed, because a detector that drives a fatal
      does not drive a refusal;
      (`-race -count=1`, then `-tags=integration -p 2`, 80.4s); `tools/tool.execute` -> SURVIVED both suites.
      The other five constructions (`dispatch/user.message`, both loop lanes, model, governance) pass nil already,
      so there is nothing to mutate: the refusal is undeclared in production, which IS #1342.
      **Re-runs of the package mutants against a consumer suite**: `m7g` (drop `runWork`'s recover) -> KILLED by
      governance; `m8g` (drop `Settle`'s `Admit()` guard) -> SURVIVED governance, and survives loop for the same
      reason: their settlement work panics, so a re-run quarantines and settles nothing, which is
      indistinguishable from a refusal at the assertions those suites make. Dispatch's `/help` replay (task 2.8)
      is M8's production-seam detector and kills it; the package test kills it directly. Not a governance or loop
      gap.
      **The guard's own mutants** are in 3.1, including one more INVALID MUTANT and the alias hole they found.
- [x] 4.4a `task check:push` green — the branch-checkable half. EXIT=0 at `40cb5a9b`, run to completion (the first
      attempt was killed by a 10-minute harness budget mid-`test:integration`, which is a harness timeout and not a
      result; re-run unbounded). Denominator, because an exit code alone does not prove the suites ran: 0 `FAIL`
      lines; race unit 156 `ok` + 20 no-test-files; integration 156 `ok`; `internal/deliverylane` and all five
      agentic processors appear by name in BOTH.
- [x] 4.4b PR body carries `implemented-by: opus` and `Closes #1341` — the coordinator's half, published by the
      coordinator; this branch is not pushed by the implementer. Split from 4.4a because one checkbox over two
      owners cannot be read: a tick would have claimed something this branch cannot show.
- [ ] 4.5 Archive/spec sync is the last content commit. NOT DONE HERE: it is a merge-time commit and this branch
      is unpushed.
- [x] 4.6 **E2E: none run, and that is a checked fact, not an assumption.** The rule is that a tier runs when a
      consumer's admission behaviour changed. Four mechanical checks say none did.
      (a) The production diff touches exactly eleven Go files: one added package, five `component.go`, five
      deleted `delivery_owner.go`. No other production file changed.
      (b0) **Four of the five components are themselves Tier 1**: `release/tier1-packages.txt:74-78` lists
      `processor/agentic-dispatch`, `-loop`, `-model` and `-tools` (plus `-loop/lessonmatch`, `-tools/executors`,
      `-tools/runner`); only `agentic-governance` is Tier 2. So the exported surface of these packages is
      semver-binding at 1.0 and "no exported change" is the load-bearing claim, not a courtesy. Re-derived here
      with an AST extractor over the only production files this change touches in those four (`component.go`
      modified, `delivery_owner.go` deleted), at `3faca84f` and at head: **57 exported declarations on each side,
      diff empty** (exported funcs, methods, types, exported struct fields, vars and consts). `agentic-governance`
      is outside the list, and `internal/deliverylane` is outside both tiers by ADR-106's `internal/` rule.
      (b) No exported declaration was added or removed in any of the five components:
      `git diff 3faca84f..HEAD -- 'processor/agentic-*/component.go' 'processor/agentic-*/delivery_owner.go' |
      grep -E '^[+-](func|type|var|const) '` filtered to exported names exits 1. `natsclient/` is untouched
      (`git diff --name-only 3faca84f..HEAD -- natsclient/` is empty), so the Tier 1 frozen surface is unchanged
      and `internal/` is outside both tiers (ADR-106).
      (c) The STRING-LITERAL multiset over the five components' production files is unchanged except for imports,
      comment text, and three panic-cause format strings that moved into the package. `"dispatch delivery work
      panicked: %v"`, `"governance ..."` and `"loop ..."` are now composed by the package's one
      `fmt.Errorf("%s delivery work panicked: %v", owner, recovered)` with `owner` = `"dispatch"` /
      `"governance"` / `"loop"` — byte-identical output, and governance's existing test asserts the literal
      string (`delivery_settlement_test.go:140`, green). Every port name, subject, health status, log message
      and metric label is therefore identical. Nothing an E2E tier observes changed.
      (d) `task schema:generate && git diff --stat schemas/ specs/` is empty: no config or wire surface moved.
      This is a `refactor`, not a BREAKING change, so the `docs/contributing/02-e2e-tests.md` § Breaking Changes
      rule is not triggered either. Integration tiers DID run in full, twice (once directly, once inside
      `check:push`), and they are the layer that exercises the real NATS delivery paths this change touches.

- [x] 4.7 **Ordering-claim sweep (owner's Codex round on `dd36b199`, MEDIUM, nonblocking).** The package orders
      exactly one sequence — close admission -> run `onFatal` -> buffer the result -> observer reaction ->
      FATAL-OBSERVER drain. It does NOT order health against the owner's own Stop: `Binding.Drain` is called by
      component cleanup independently of the admission and of `onFatal`, so a Stop concurrent with a latching lane
      can drain while the health writer is still running, and an ordinary Stop drains a lane that never latched.
      Five sites claimed the stronger thing. All were written by this change; the runtime is unchanged.
      Sweep commands (run in the worktree, stderr visible) — the reviewer's, plus a second pass for the class the
      first could miss:
      `git grep -n -iE 'before (the|its|that) (exact )?handle|before .*drain|before the latch|latch is observable|health (is|gets) written before|written before' -- internal/deliverylane docs/operations openspec/changes/delivery-lane-admission-package 'processor/agentic-*/component.go' 'processor/agentic-*/*_test.go'`
      and
      `git grep -n -iE 'health.{0,90}(drain|observable)|(drain|observable).{0,90}health' -- internal/deliverylane docs openspec/changes/delivery-lane-admission-package 'processor/agentic-*'`.
      Every hit, with its disposition:

      | Hit | Disposition |
      |---|---|
      | `docs/operations/migration-restart-safe-nats-client.md:95` "health is written before the exact handle can drain" | **FIXED** — the write completes before the result is buffered, so before the OBSERVER drains; and the LIMIT is now explicit: it says nothing about the adopter's own Stop |
      | `internal/deliverylane/deliverylane.go:37` `NewAdmission` "so health latches before the exact handle drains" | **FIXED** — names the fatal observer as the only drain it orders, and names both Stop cases |
      | `internal/deliverylane/deliverylane.go:195` `Binding.Drain` "Admission latches BEFORE the handle is drained" | **FIXED** — false for an ordinary Stop, where the lane never latched; now says Drain is deliberately unsynchronized with Admission and the ordering holds on the fatal path only |
      | `design.md:123`, `:271` — verbatim copies of those two comments in § 5 | **FIXED** — kept byte-identical to the package |
      | `design.md:383` invariant I3 | **FIXED** — same qualification |
      | `deliverylane_test.go:178-180` the I3 comment | **FIXED** — says why its assertions hold (no Stop competes in this test) instead of implying a general contract |
      | `deliverylane_test.go:204` assertion message | **FIXED** — names the OBSERVER as what must not have drained |
      | `tasks.md:198` (4.2) "the health write completes before the latch is observable" | **FIXED** — doubly wrong: `Admit()` is already false while `onFatal` runs, which the test at `:203` asserts |
      | `processor/agentic-loop/component.go:1021` and `agentic-model/component.go:534` "health can never read healthy after the exact handle has drained" | **FIXED** — qualified to the fatal observer, and says it is not ordered against cleanup's own Drain |
      | `deliverylane.go:180`, `design.md:256` "a fatal reported before the handle returned stays buffered" | KEPT — true, and about acquisition ordering, not drain ordering |
      | `design.md:13` "record the fatal into health synchronously, buffer the result, and let an observer drain" | KEPT — the correct sequence, stated correctly |
      | `design.md:324-325` "must run in the callback before the result is buffered" | KEPT — correct wording |
      | spec delta `:28` "SHALL run synchronously inside the latch before the result is buffered" | KEPT — the reviewer named this as the correct wording; every other site was made to match it |
      | `agentic-dispatch/component.go:675`, `agentic-model/component.go:548` `reactDeliveryFatal` "run by the observer before it drains that lane's exact handle" | KEPT — true: this IS the fatal observer |
      | `agentic-dispatch/terminal_settlement_integration_test.go:506` "by the time the exact handle has drained, health already names THIS lane" | KEPT — true of that test, which observes the fatal path with no competing Stop |
      | `agentic-tools/delivery_owner_test.go:93`, `:148` "the lane closes before the handle exists" | KEPT — about latching before acquisition |
      | `inventory.md:475`, `:488`, `:495`; `proposal.md:36`; `tasks.md:41`; `design.md:408`; `agentic-loop/partial_publish_settlement_integration_test.go:59` | KEPT — lists of outcomes or test names, no ordering guarantee |
      | `docs/operations/migration-beta162-to-beta163.md:769`, `:1283`; `docs/adr/055`, `084`, `094`; two `docs/proposals/*`; `agentic-dispatch/component.go:849`; `agentic-dispatch/terminal_settlement_integration_test.go:154`, `:585`; `agentic-loop/response_handler_failure_test.go:59`, `terminal_failure_record_integration_test.go:17`, `tool_result_handler_failure_test.go:67`, `:227` | KEPT — unrelated subject matter (KV records "written before acknowledged", forwarder drain, handler ordering) |

      No runtime change: `internal/deliverylane/deliverylane.go` differs only in comment lines.

## Out of scope (recorded, not tasks)

- The five silent-refusal lanes at `L3:` — #1342 (blocked by #1341).
- Exporting the package — future gate, `design.md` § 3.
- Anything in #1249's or L4's own scope beyond the call they make (`design.md` § 6); L4's one-line Survives edit.
- The stale lane comment at `L3:processor/agentic-dispatch/component.go:716-718` — L3's reviewer.
