# Tasks — agentic-loop-durable-applied-facts (#1330)

> **Re-pin and reconciliation done (2026-09-22).** Base `b7ce8727`; `scripts/inventory-verify.sh inventory.md` →
> `pins=293 ok=293 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`. The reconciliation is SEMANTIC, not mechanical:
> the Codex recovery layer this design was accepted as a pruning of never landed on `main`, so § 6 inverts (nine
> "survives" items are builds; one real deletion target) and three ruling premises read differently here (Q1 birth
> order, Q2 revision availability, § 3.6's applied-set rationale). Rows, replacement sentences, the L4a/L4b split and
> the no-home Codex facts are in `reconciliation.md`; pin substitutions for this file are in `tasks-pins.md`. Neither
> the design text nor these tasks are amended until the owner rules on the 2026-09-22 docket on #1330; every
> `68c14c8e` pin below maps through `reconciliation.md` § A. From here `scripts/inventory-verify.sh` is expected to go
> RED as the implementation lands (pins are pre-change evidence; never re-pin a landed change). The seed-time edit to
> task 1.0's wording stands.

> File:line pins are at `68c14c8e`. Dependency order; each task names its file and the proving test. No landing tasks (claim PR, CI,
> merge, archive) — PR checklist, owner ruling #1230 Option 1. Prerequisites #1327–#1329 merged; #1328 supplies the RequestID grammar and its helpers, named `looprequest.Next/Parse`.

## 1. Field and invariant surface

- [ ] 1.0 New `processor/agentic-loop/internal/looprequest`: `Parse(id) (loopID string, iteration, retry int, err)` over `<loopID>:req:<i>:<r>` (loopID may itself contain colons; parse from the right, require all four parts, reject a missing retry part), `Next(prev, retry bool) string` (retry → same iteration, retry+1; else iteration+1, retry 0), `Compare(a, b)` on `(iteration, retry)`. L2 (PR #1335) shipped no parser because no reader existed yet; this change is the reader. Rapid property: `Parse(format(x)) == x`; `Compare` is a total order consistent with `Next`.
- [ ] 1.1 `agentic/state.go`: add `PublishedRequestID string \`json:"published_request_id,omitempty"\`` beside `:54` with
      the I1 doc comment; `Validate` (`:96-108`) unchanged. Test: `agentic/state_test.go` JSON round-trip; `TransitionTo` (`:132-150`) keeps it.
- [ ] 1.2 `processor/agentic-loop/state.go`: `SetPublishedRequest(loopID, requestID)`; `restoreLoopFromRequest` (`:349-430`)
      is fed the record step 0 (2.5) already adopted plus the newest retained request, so it has no `PublishedRequestID`
      mismatch to refuse (identity checks at `:356` unchanged). Test: `state_test.go` — set / restore-after-adoption / restore-equal.

## 2. Carrier: order, CAS, identity adoption

- [ ] 2.1 `processor/agentic-loop/component.go`: `persistLoopState` (`:2396-2415`) takes `revision` and uses `loopsBucket.Update`;
      `persistHandlerResult` (`:1782-1799`) publishes before writing for non-terminal results; birth (`:1405` → `:1410`) keeps
      Put → publish. Test: `persist_handler_result_test.go` — publish observed before Update; CAS conflict → `DeliveryDecisionRetry`; birth order unchanged.
- [ ] 2.2 `component.go` `publishResults` (`:2331-2354`): messages on the `agent.request` subject publish via `PublishToStreamWithMsgID`
      with `Nats-Msg-Id = RequestID`. Test: `publication_semantics_integration_test.go` — a duplicate publish collapses within the window.
- [ ] 2.3 `component.go`: before publishing a minted next request, call `readRetainedAgentRequest` (`SR:273-323`); adopt
      on exact `RequestID` match, publish on absent/current, quarantine anything else (design § 3.3). Test: unit through the
      `loopSettlementEvidenceReader` seam (`SR:28-32`) — retained == next → no publish; == current → publish; absent → publish; other → Quarantine.
- [ ] 2.4 `handlers.go`: set the field at the three minting sites (`:1083` via `buildTaskResultFromRequest`, `:1981`, `:2654`);
      mint via `looprequest.Next(PublishedRequestID)` (task 1.0); retry ordinal from the parsed field; delete `IncrementTruncationRetry`/
      `ResetTruncationRetry` (`state.go:601-616`). Test: `handlers_test.go` — grammar holds; retry ordinal survives a rebuilt LoopManager.
- [ ] 2.5 `settlement_recovery.go`: step 0 for every cold read but the task lane (design § 3.6): read the newest retained request (`:63`),
      order it against `PublishedRequestID` with `looprequest.Parse`/`Compare` (task 1.0); newer → `Update(revision)` the record (field, `Iterations`, `PendingToolResults = nil`,
      and `PendingApproval = nil` + `State = running` when a gate was pending — no entry is synthesized) before classifying; older/unparseable → Fatal → Quarantine. Wire at
      `:517`, `:622`, `:867`. Test: unit via the evidence-reader seam — equal / newer-adopt (record passes `Validate`, `agentic/state.go:96-108`; applied set empty; I4 holds) /
      newer-adopt over an `awaiting_approval` record (gate cleared, `State = running`) / older / unparseable.

## 3. Lane classification (delete the layout proofs)

- [ ] 3.1 `settlement_recovery.go` `recoverToolResult` (`:553-682`): cold → 2.5 first; classify `result.RequestID` vs `PublishedRequestID`
      (older → ACK; newer → Retry; unknown → Quarantine); delete `toolResultProvenInLaterRequest` (`:686-740`), `approvalRequiredResultSuperseded`
      (`:763-817`), `proveTerminalToolResultApplied` (`:821-852`), truncation (`:634-636`); `state.go`: delete `Iterations--` (`:486-488`),
      `requirePreceding` (`:435-444`). Test: rewrite `tool_result_recovery_test.go`.
- [ ] 3.2 `settlement_recovery.go` `ensureResponseLoop` (`:454-548`): cold → 2.5 first; replace the retained-request compare (`:517-535`)
      with the field compare; newer → Retry. Test: `settlement_recovery_test.go` rewrite (response lane cases).
- [ ] 3.3 Approval lane: I4 replaces `validatePendingApprovalRequest` (`:1034-1040`); `validatePendingApprovalEvidence` (`:984-1013`) reduces
      to I4 + `validatePendingApprovalResult`; `republishPendingApproval` (`:743-758`) validates by identity only; `recoverApprovalResponse` (`:857-917`)
      otherwise unchanged; `settleAbsentApprovalEvidence` (`:921-981`) untouched (Q8). Test: `approval_timeout_recovery_test.go`, `approval_restore_order_test.go` + the I4 mismatch case.
- [ ] 3.4 Task lane: `recoverTaskDelivery` (`:377-452`) republishes R1 unconditionally at iteration 0 with the
      MsgId, no retained read (Q1). Test: `recovery_test.go` cold redelivery at iteration 0 and after advance.
- [ ] 3.5 Terminal + unproven result (Q7): effect-free ACK; new `toolResultsInapplicable` counter at the four `approvalDecisionsInapplicable`
      sites in `metrics.go` (`:20` field, `:113` constructor, `:304` `RegisterCounter`, `:336` `DefaultRegisterer`; use-site pattern `approval_response_handler.go:204`);
      `WarnContext` audit line; warm-lane `component.go:2178-2180` takes the same ACK when terminal. Test: rewrite `terminal_tool_recovery_test.go`; metric asserted.
- [ ] 3.6 Verdict ACK path (Q6): `handleToolCallVerdictMessage` (`component.go:2585-2596`) on no-waiter (`governance_dispatcher.go:557-560`) reads
      the entity by `verdict.LoopID` and applies the § 5.6 classification. Test: `verdict_wire_test.go` (no waiter + consumed → Ack; current unseen → Retry; terminal → Ack).
- [ ] 3.7 Terminal owner (design § 5.7): `persistTerminalOutcome` (`component.go:1833-1870`) — terminal at the observed revision
      (`:1838-1839`) → Q7 ACK; saved `COMPLETE_` payload from `selectTerminalOutcome` (`:1846`, `:1959-1960`) → adopt by loop ID +
      kind under `Update(revision)`, log differences; delete the compares (`:1852-1856`, `:1861-1866`). Test: `terminal_owner_test.go` (a)/(b)/(c).
- [ ] 3.8 In-flight answer (the MODIFIED requirement's new SHALL NOT): a test citing that requirement (`// spec:` line; none exists at `68c14c8e`,
      `git grep 'acknowledgement floor' -- '*_test.go'` is empty — add it beside the in-flight query) with a case where the record names `published_request_id`
      and a non-empty `pending_tool_results` while the process is gone and the answer is still consumer bookkeeping; `git grep -n 'PublishedRequestID\|published_request_id' -- ':!processor/agentic-loop' ':!agentic'` returns only docs/spec.

## 4. Property and window evidence

- [ ] 4.1 New `processor/agentic-loop/applied_facts_property_test.go`: Rapid state machine over an in-memory KV and a
      fake evidence reader; actions deliver / crash-at-{W1,W2,W3,W4} / redeliver / replace-process on the tool, response, and approval-response
      lanes (the generator must reach the reject-minted W4 of § 5.5);
      checks I1–I4 after every step (I2 as membership, never rendering) and "no duplicate request published unless absent from the fake stream".
- [ ] 4.2 Real-NATS W2 and W4 on the tool lane — the W4 case RESTARTS the process via `test/e2e/harness/processbarrier`
      (replacement, not a fresh handler) and asserts the adoption-first write — W2 and W4 (truncation retry) on the response lane,
      the approval-response lane's reject W4 (crash between `ARH:269` and `:275`, restart, redeliver the approval
      response → ACK inapplicable, R(N+1) counted once on `agent.request.<loopID>`, record `running` at R(N+1) with no gate),
      and the terminal lane's crash-after-publish-before-Update (§ 5.7); assertions read `AGENT_LOOPS` (`published_request_id`, `iterations`,
      `state`) and count messages per subject, never bodies. Files: `tool_result_redelivery_integration_test.go`, `terminal_tool_redelivery_integration_test.go`, new `identity_adoption_integration_test.go`.
- [ ] 4.3 Mutation evidence (`cp` backup + checksum, never stash): (a) restore Put-before-publish in 2.1 → W4 in 4.2 fails;
      (b) restore plain `Put` → the CAS case in 2.1 fails; (c) skip adoption in 2.3 → the 4.1 property fails; (d) restore the
      terminal compare in 3.7 → the terminal case in 4.2 fails; (e) skip step 0 in 2.5 → the restarted W4 case in 4.2 retries to `MaxDeliver`; (f) drop the gate clear from 2.5 → the
      approval-lane W4 case in 4.2 fails on I4 and re-runs the rejection.

## 5. Docs and spec

- [ ] 5.1 Correct `docs/concepts/17-approval-flow.md:65-68` and `processor/agentic-loop/doc.go` restart claims to state the I1–I4 contract and its prerequisites (#1327–#1329).
- [ ] 5.2 Apply `specs/agentic-loop/spec.md` delta; `openspec validate agentic-loop-durable-applied-facts --strict` green; `task spec:properties` cites the ADDED requirement from 4.1 (`// spec:` line).

## 6. Verification (before the push, every time)

- [ ] 6.1 `task check:push` (schema drift expected empty — `LoopEntity` is in no schema); `go run ./cmd/entity-id-audit .` green.
- [ ] 6.2 `task e2e:agentic` with the process-replacement stage (`stage_a_process_replacement.go`, `process_replacement_test.go`)
      and the approval-after-restart stage (`approval_restart.go`, `approval_restart_test.go`) named in the PR body with exit
      codes; BREAKING for the recovery contract, so this tier is the gate (`docs/contributing/02-e2e-tests.md` § Breaking Changes).
