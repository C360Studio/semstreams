# Tasks

Ordering matters: the gate lands before the seams adopt it, and the terminal release lands after the fence so
the two `state.go` edits are reviewed together rather than re-derived.

## 1. Evidence (complete)

- [x] 1.1 Inventory every seam accepting a request that names an existing loop, line-pinned, per-seam check
      matrix — `inventory-attach.md`, `inventory-carriers.md`
- [x] 1.2 Answer: does the dispatch `LoopTracker` rehydrate from durable state on restart? — NO,
      `findings-decisive-questions.md`
- [x] 1.3 Answer: does legitimate continuation preserve conversation context? — NO, same file
- [x] 1.4 Enumerate the framework's existing admission/refusal precedent — `inventory-precedent.md`
- [x] 1.5 Architect verification pass over the explorer files: spot-checks, additions, strikes —
      `inventory-verification.md`
- [x] 1.6 Name the problem shapes this design adopts and their closest existing instance (#1232 pilot) —
      `inventory-problem-shape.md`

## 2. Target state (complete — under review)

- [x] 2.1 `proposal.md`, including the adopter seam inventory and the measured premises
- [x] 2.2 `design.md` — seam census, per-piece shape, invariants, decision-skill outcomes
- [x] 2.3 Spec deltas: `agentic-dispatch` (new capability), `agentic-loop`, `entity-id-contract`
- [ ] 2.4 HALT — owner word on `design.md` §Open questions 1, 2, 3 before implementation starts

## 3. The admission gate

- [ ] 3.1 Add `processor/agentic-dispatch/loop_admission.go`: one entry point, the refusal codes, the single
      metric-reason mapper, and the single named log constant — mirroring
      `processor/graph-ingest/authority_gate.go` element for element
- [ ] 3.2 Add the refusal `CounterVec` to `routerMetrics` (seam, reason labels) and register it in the `router`
      subsystem alongside the existing series
- [ ] 3.3 Adopt `errs.ClassifiedCodeDetail` for every refusal the gate returns; `processor/agentic-dispatch`
      currently has zero classified errors
- [ ] 3.4 Implement the merged lookup: `getSnapshot` ∪ `loadPersistedLoop`, reconciled by `mergeRouteField`,
      absence distinguished by `isLoopRecordAbsent`, bucket observed via the declared KV read port
- [ ] 3.5 Unit-test the ordering invariants I1–I3 directly: malformed-and-absent refuses as malformed;
      absent-and-unowned refuses as absent; one counter increment per refusal

## 4. Adopt the gate at every seam

- [ ] 4.1 Channel submission (`component.go:920-931`) and HTTP submission (`http.go:296-311`): replace
      `refuseNonCanonicalLoopTokens` with the gate on the resolved continuation token; keep the typed
      field-naming response on both lanes
- [ ] 4.2 `/cancel` (`commands.go:53-96`) and `/status` (`commands.go:148-172`): route through the gate; delete
      `canUserControlLoop` (`component.go:1204-1216`), whose only caller is `commands.go:73`
- [ ] 4.3 `GET /loops/{id}` (`http.go:602-621`): form + existence only, no ownership — record it in the
      carve-out list, not as an omission
- [ ] 4.4 `POST /loops/{id}/signal` (`http.go:642-658`) and `POST /loops/{id}/approval` (`http.go:739-757`):
      gate the URL-path token before the existence check; enforce `Permissions.Approve` on the approval
      endpoint with ownership NOT consulted
- [ ] 4.5 Map refusals to HTTP status: 400 malformed, 404 absent, 403 not permitted / not owned, 409 terminal
- [ ] 4.6 Test each seam refuses through the gate and emits exactly one counted refusal (I3)

## 5. Form enforcement at the remaining carriers (#1228)

- [ ] 5.1 `agentic.UserSignal.Validate` (`agentic/user_types.go:124-141`)
- [ ] 5.2 `agentic.ApprovalResponse.Validate` (`agentic/approval.go:122`)
- [ ] 5.3 `agentic.ApprovalPendingEvent.Validate` (`agentic/approval.go:66`)
- [ ] 5.4 `agenticdispatch.SignalMessage.Validate` (`processor/agentic-dispatch/loop_tracker.go:594-596`) —
      today an unconditional `return nil`; add non-emptiness and form
- [ ] 5.5 Retire the fixtures that encode a retired token shape as VALID, starting with
      `agentic/user_types_test.go:143-154`, and sweep for others

## 6. The create-vs-exists fence (#1227)

- [ ] 6.1 `LoopManager.CreateLoopWithID` (`state.go:151-183`): already-exists refusal before the three map
      writes, form check still first; sentinel shaped on `pkg/lifecycle/errors.go:44-50`
- [ ] 6.2 `HandleTask` (`handlers.go:800-841`): branch on already-exists and ATTACH — reuse the existing
      context manager, append the new turn, do not re-seed the system prompt, do not clear pending tools
- [ ] 6.3 Refuse a continuation whose existing loop is terminal; do not mint a replacement under the token
- [ ] 6.4 Preserve redelivery dedup across an attach (I6 does not cover this; it needs its own test)
- [ ] 6.5 Test I6 and I7: context-manager identity unchanged across a continuation; a refused create mutates
      none of the three maps

## 7. Terminal release of per-loop state (#1233)

- [ ] 7.1 Extend `releaseLoopTransientState` (`trajectory_handler_wiring.go:52-55`) to release every per-loop
      map `DeleteLoop` clears (`state.go:344-354`); keep it idempotent and keep it the ONLY release site
- [ ] 7.2 Make the three late-arrival readers treat an absent loop as an expected settled-drop, not a failure:
      `approval_response_handler.go:57-75`, and the late tool-result / late model-response paths
      (`component.go:1806-1817`)
- [ ] 7.3 Decide `DeleteLoop`'s fate — it becomes either the release's implementation or dead code; do not
      leave a second, unreferenced release path
- [ ] 7.4 Test I8 and I9: absence and terminal presence are indistinguishable to every reader; release is
      idempotent; a settled loop's result is still readable through `read_loop_result`
- [ ] 7.5 Test that release happens after the terminal observation and the terminal graph write

## 8. #1225's ordering and answer

- [ ] 8.1 Move `Track` and `recordLoopStarted` after the task marshal on both submission paths
      (`component.go:957,971,975`; `http.go:336,349,353`)
- [ ] 8.2 Answer the submitter with a typed error naming the offending field on both lanes; count the refusal
- [ ] 8.3 Test I5: a submission that publishes no task leaves the tracker and the gauge unchanged

## 9. Gates

- [ ] 9.1 `task lint`, `go test -race ./...`, `go test -tags=integration -race -p 2 ./...`
- [ ] 9.2 `task schema:generate` and commit any schema/spec drift with the code
- [ ] 9.3 `task e2e:agentic` green BEFORE the breaking commit lands (the `reply_to`-must-exist change)
- [ ] 9.4 Migration note in `docs/operations/` covering the `reply_to` behaviour change and the newly enforced
      `approve` permission, including the statement that the default admits everyone
- [ ] 9.5 Archive/spec sync as the last content commit, reviewed with the code
