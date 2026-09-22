# Tasks — agentic-loop-durable-applied-facts (#1330, L4a)

> **Re-pinned at `b7ce8727`; amended 2026-09-22 to the owner's rulings.** `scripts/inventory-verify.sh inventory.md`
> at the re-pin → `pins=293 ok=293 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`. Rulings applied: #1330
> [issuecomment-5773604066](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5773604066) over the
> reconciliation docket and its simplicity re-read
> ([issuecomment-5773199445](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5773199445)), the
> re-read governing where the two differ; `design.md` § 1 carries the bullets. **Scope is L4a.** The approval lane,
> the verdict after waiter loss, the single terminal owner and route-ambiguity metering are **L4b = #1362** and are
> listed under "Moved to L4b (#1362)" at the end of this file — moved, not dropped. The approval lane's and
> sweeper's carrier reorder and OQ8's gate-order decision moved there too (coordinator scoping under OQ7/OQ8,
> `design.md` § 1): L4a's writer becomes CAS `Update` for every caller, but the ORDER flip reaches only the task,
> model-response and tool-result lanes. Delta text those tasks take over is held verbatim at the end of this file. Standing simplicity rule (owner,
> 2026-09-22): keep complexity as low as possible; an edge case that a doc sentence or a plain "not supported" can
> carry does not earn a code branch. From here `scripts/inventory-verify.sh` is expected to go RED as the
> implementation lands — pins are pre-change evidence and a landed change is never re-pinned.

> File:line pins are at `b7ce8727` through the substitutions in `tasks-pins.md`, using the `inventory.md`
> abbreviations (ST = `processor/agentic-loop/state.go`, C = `…/component.go`, H = `…/handlers.go`, and so on); a
> `68c14c8e` pin appears only where the text names the predecessor site it replaces. Dependency order; each task names
> its file and the proving test. No landing tasks (claim PR, CI, merge, archive) — PR checklist, owner ruling #1230
> Option 1. Prerequisites #1327–#1329 merged; #1328 shipped the RequestID grammar (`ST:1339-1347`).

## 1. Field and invariant surface

- [x] 1.0 New `processor/agentic-loop/internal/looprequest`: `Parse(id) (loopID string, iteration, retry int, err)` over `<loopID>:req:<i>:<r>` (loopID may itself contain colons; parse from the right, require all four parts, reject a missing retry part), `Next(prev, retry bool) string` (retry → same iteration, retry+1; else iteration+1, retry 0), `Compare(a, b)` on `(iteration, retry)`. `Next` must reproduce `ST:1345-1347` (`iteration = entity.Iterations + 1`; `%s:req:%d:%d`), so birth mints `…:req:1:0` from `Iterations = 0`. `Parse` becomes the one reader of the grammar, replacing the two prefix-only readers on `main`: `ST:1360-1361` (`ExtractLoopIDFromRequest`) and `LP:50` (`loopIDFromStructuredID`). L2 (PR #1335) shipped no parser because no reader existed yet; this change is the reader. Rapid property: `Parse(format(x)) == x`; `Compare` is a total order consistent with `Next`.
      **Landed.** `looprequest.go` — `ID{LoopID,Iteration,Retry}` (the three parts travel together, so one named
      value rather than three positional returns), `Parse` (right-hand read; canonical ordinals only — `007`, `+1`
      and `iteration 0` are refused), `ID.String`, `Next(prev, retry)`, `Compare(a, b)`. `Parse` did NOT replace the
      two prefix-only readers (`ST:1360`, `LP:50`): both are on the Tier 1 surface or its call graph and replacing
      them is section 3's lane work, not a grammar change — recorded so the substitution is not lost.
      Tests: `looprequest_test.go` (13 refusal cases + 6 accept cases — the half a round-trip property cannot see),
      `looprequest_prop_test.go` `TestPropParseRoundTripsEveryMintedName` and
      `TestPropCompareIsATotalOrderConsistentWithNext` (boundary-hugging generators: iteration at 1, retry at 0,
      loop IDs that contain colons and that END in `:req`), `FuzzParseNeverPanicsAndRoundTrips` (15 seeds across
      both grammar classes; 2,316,130 execs / 21s clean, `new interesting: 134`).
      Mutant (`cp` backup + md5 + printed `[applied]`, restored and checksum-verified): delete the
      `looprequest.Parse`/`Next` read from `GenerateRequestID` (`ST:1385-1390`) → seven tests red, among them
      `TestRetryOrdinalSurvivesARebuiltLoopManager` and `TestRequestIDCarriesTheTruncationRetryOrdinal`.
- [x] 1.1 `agentic/state.go`: add `PublishedRequestID string \`json:"published_request_id,omitempty"\`` beside `AG:57`
      with the I1 doc comment; `Validate` (`AG:136`) unchanged. Test: `agentic/state_test.go` JSON round-trip;
      `TransitionTo` (`AG:171`) keeps it.
      **Landed** at `agentic/state.go:58-71`; `Validate` untouched. Tests are in a NEW file,
      `agentic/published_request_id_test.go`, not `state_test.go`: that file carries a "Builder must make these
      tests pass without modification" banner. `TestPublishedRequestIDSurvivesTheDurableRoundTrip`,
      `TestPublishedRequestIDIsOmittedWhenUnset` (both directions of additivity — no key when unset, empty when a
      pre-field record decodes), `TestTransitionToKeepsTheOutstandingRequest`,
      `TestValidateIgnoresTheOutstandingRequest`.
- [ ] 1.2 `processor/agentic-loop/state.go`: `SetPublishedRequest(loopID, requestID)`; new `restoreLoopFromRequest`
      beside `attachContinuation` (`ST:298`) — no rebuild path exists on `main` — fed the record step 0 (2.5) already
      adopted plus the newest retained request; it rebuilds the ContextManager, the routing maps (`ST:79`
      `outstandingRequests`, `ST:887-888`) and the tool batch (`restoreToolBatch`, membership against the retained
      response only), so it has no `PublishedRequestID` mismatch to refuse. Test: `state_test.go` — set /
      restore-after-adoption / restore-equal.
      **Half landed.** `SetPublishedRequest` is at `ST:914-924`, refusing a loop the manager does not hold rather
      than ignoring it, and has three production callers (the mint sites of 2.4). `restoreLoopFromRequest` /
      `restoreToolBatch` are sequenced to the checkpoint that wires their call sites: their only consumers are the
      cold rebuilds of tasks 3.1 and 3.2 (design § 5.2 step 2, § 5.3 step 3), and the shape of the ContextManager
      rebuild — which `RegionType` each retained `ChatMessage` returns to — is decided by those call sites. Building
      it ahead of them is the "zero present consumers" shape the developer contract refuses. Recorded as a
      sequencing choice inside this PR, not a scope change.

## 2. Carrier: order, CAS, identity adoption

- [x] 2.1 `processor/agentic-loop/component.go`: `persistLoopState` (`C:2468`, write `C:2483`) takes `revision` and uses
      `loopsBucket.Update` (`KV:231`) — the writer changes for EVERY caller. The ORDER flip is lane-scoped
      (coordinator scoping under OQ7/OQ8, `design.md` § 1): `persistHandlerResult` (`C:1923`) publishes (`C:1959`)
      before it writes (`C:1947`) for a non-terminal result on the model-response and tool-result lanes only; the
      approval lane's call site (`ARH:205`) keeps today's write → publish, and the sweeper keeps its own
      publish-then-`Put` pair (`AS:100-101`) — both move in #1362. Birth (`C:1496` publish → `C:1499` `Put` with the
      error ignored today) becomes `Put` → publish, and a birth write that errors returns Retry — the first of
      #1345's five task-intake branches, converted by necessity; the other four stay #1345's. On
      `ErrKVRevisionMismatch` (`KV:238`) return Retry and release the loop's process state (`ST:574`, `C:1964`).
      `persistLoopState`'s other callers ride the same `Update`: `C:1425` (deferred continuation marker) and `AS:101`
      (sweeper). Test: `persist_handler_result_test.go` — publish observed before `Update` on the tool-result lane;
      write observed before publish on the approval lane's call site; CAS conflict → `DeliveryDecisionRetry`; birth
      order write-then-publish.
      **Landed.** The writer is `persistLoopState` (`C:2698`): CAS `Update` against a per-loop revision the process
      retains (`loopRevisions`, `C:73`; `rememberLoopRevision` `C:2624`, `observedLoopRevision` `C:2636`,
      `forgetLoopRevision` `C:2647`, released with the loop at `trajectory_handler_wiring.go:69`). **Deviation from
      the task's wording, recorded:** `persistLoopState` keeps its `(ctx, loopID)` signature and reads the retained
      revision itself instead of taking one. Four callers would otherwise have to carry a fact the component already
      holds, which the developer contract's "am I asking a caller to predict something the framework could observe"
      refuses; the invariant is unchanged, because the retained revision IS the revision this process observed. A
      caller with no observation fails CLOSED (`C:2712`, Fatal) rather than writing blind or updating against
      revision 0, which NATS reads as "must not exist".
      The ORDER is a parameter, `carrierOrder` (`C:1980`), passed at every call site:
      `publishThenWrite` on the model-response (`C:1650`) and tool-result (`C:2244`) lanes for a NON-terminal result
      (`publishThenPersistResultState`, `C:2085`); `writeThenPublish` on the approval lane (`ARH:208`), on the
      failed-terminal tool result (`C:2295`), and for every terminal result on every lane. The sweeper's own
      publish-then-`Put` pair is untouched in order and only rides the new writer (`AS:103`, which now names its
      write failure instead of discarding it — a timer has no delivery to retry).
      Birth is `Put` → publish at `C:1535`, its error returns Retry; the birth publish error stays discarded, which
      is #1345's remaining branch.
      **A second recorded deviation:** the transient test in the carrier is `errors.Is(err,
      natsclient.ErrKVRevisionMismatch)`, NOT `errs.IsTransient`. The latter substring-matches error TEXT for
      "unavailable"/"timeout" (`pkg/errs/errs.go:177-187`), which handed a commit-unknown KV failure a Retry it had
      not earned — caught by `TestResponseAndToolResultPersistenceFailureCannotAck` going from Quarantine to Retry.
      Tests: `loop_carrier_test.go` — `TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind` (both orders, through
      a real but unconnected client so the publish genuinely fails: publish-first leaves NO record, write-first
      commits it), `TestApprovalLaneKeepsWriteThenPublish` (the CALL SITE, through
      `handleApprovalResponseMessage`), `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` (a foreign write
      moves the record → `ErrKVRevisionMismatch`, the loop is gone from memory AND its revision is forgotten).
      Birth order: `carried_continuation_durability_test.go`
      `TestQuarantinedCarryWritesNoRecordAndLeavesTheTurnUncarried` asserts `bucket.written()` is EMPTY after a
      failed publish, which is the order assertion on the response lane's real path.
      Mutants: delete the `publishThenWrite` dispatch (`C:2021-2023`) →
      `TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind` +
      `TestQuarantinedCarryWritesNoRecordAndLeavesTheTurnUncarried` red; replace the CAS `Update` (`C:2724`) with a
      blind `Put` → `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` red.
- [x] 2.2 `C:2318-2333` `publishResults`: messages on the `agent.request` subject publish via `PublishToStreamWithMsgID`
      with `Nats-Msg-Id = RequestID`. **Shipped by L2 (#1328, PR #1335)** — recorded here because tasks record work when
      it happens. Verified at `b7ce8727`:
      `git grep -n PublishToStreamWithMsgID -- 'processor/agentic-loop/*.go'` →
      `processor/agentic-loop/component.go:2328:		if err := c.natsClient.PublishToStreamWithMsgID(ctx, msg.Subject, msg.Data, msg.MsgID); err != nil {`;
      the three mint sites it carries, `git grep -n 'MsgID:' -- 'processor/agentic-loop/*.go'` →
      `processor/agentic-loop/handlers.go:1174:				MsgID:   request.RequestID,`,
      `processor/agentic-loop/handlers.go:2194:		MsgID:   request.RequestID,`,
      `processor/agentic-loop/handlers.go:2958:		MsgID:   request.RequestID,`.
      Test: `publication_semantics_integration_test.go` (exists). L4a verifies the window collapse only.
- [x] 2.3 `component.go`: new `readRetainedAgentRequest` (identity only; the newest message on `agent.request.<loopID>`,
      pattern `PS:21`/`PS:28`/`PS:37` `GetLastMsgForSubject`, `TR:51`) behind a new evidence-reader interface with two
      reads (request, response), called before `C:2318` for the messages minted at `H:1122`, `H:2168`, `H:2927`; plus a
      revision-returning entity read (`LP:75` discards `entry.Revision()` today). Adopt on exact `RequestID` match,
      publish on absent/current, quarantine anything else (design § 3.3). Test: unit through the evidence-reader seam —
      retained == next → no publish; == current → publish; absent → publish; other → Quarantine.
      **Landed** in a new file, `processor/agentic-loop/loop_evidence.go`: `loopEvidenceReader` (`:32`),
      `natsLoopEvidenceReader` over `GetLastMsgForSubject`, `requestAddress` (`:65`, resolved from the same output
      port the publish side uses), `readRetainedAgentRequest` (`:97`, identity only — the body is decoded for its
      RequestID and nothing else), and the decision `adoptRetainedRequest` (`:218`) called from `publishResults`
      before every minted request (`C:2470`; `msg.MsgID != ""` is exactly the three mint sites, measured).
      The revision-returning entity read is `readLoopRecord` (`:159`) returning `loopRecord{entity, revision,
      presence}` (`:147`); `classifyMissingLoop` keeps its signature and delegates (`loop_presence.go:69`).
      **Recorded simplification (standing simplicity rule, owner 2026-09-22):** the interface carries ONE read, not
      two. The retained-RESPONSE read's only consumer is `restoreToolBatch`, which lands with the cold rebuild; an
      interface method nothing calls is the surface the contract refuses to add. **Recorded simplification #2:** the
      design separates "retained == the record's current request" (publish) from "retained older than it"
      (quarantine); both are "older than the request being minted" here and both publish. Reaching the second
      requires I1 to be broken already — the current request is retained by definition while the record exists — and
      publishing the request the loop needs beats refusing the loop.
      Test: `loop_carrier_test.go` `TestMintedRequestAdoptsAnAlreadyRetainedIdentity`, six arms through the
      evidence-reader seam — already retained (adopt, publish nothing), previous request, retry of the previous,
      nothing retained (publish), a LATER request (Quarantine), another loop's request (Quarantine).
      **The ritual found that test blind to its own wiring**, and a second pin exists because of it: deleting the
      whole identity check from `publishResults` (`C:2469-2477`) left it green, because it drives
      `adoptRetainedRequest` directly. `TestPublishingAMintedRequestConsultsTheRetainedIdentity` drives
      `publishResults` with a client that cannot publish — adoption is then the only thing that can make it return
      nil — and goes red on that mutant; its never-retained arm shows the nil is adoption's, not the path's.
- [x] 2.4 `handlers.go`: set the field at the three minting sites (`H:1122` in `buildTaskRequest`, `H:2168` in
      `emitRetryRequest`, `H:2927` in `publishIterationRequest`); mint via `looprequest.Next(PublishedRequestID)`
      (task 1.0); retry ordinal from the parsed field; delete `IncrementTruncationRetry` (`ST:466`) and
      `ResetTruncationRetry` (`ST:477`) with their callers `H:2037`, `H:1389`, `H:1409` and the map cleared at
      `ST:588`. Test: `handlers_test.go` — grammar holds; retry ordinal survives a rebuilt LoopManager.
      **Landed, except the two deletions.** The field is set at `H:1138` (`buildTaskRequest`), `H:2190`
      (`emitRetryRequest`) and `H:2956` (`publishIterationRequest`), each beside the `TrackRequest` the site already
      made. `GenerateRequestID` (`ST:1375`) now mints through `looprequest`: iteration stays `Iterations + 1`, and
      the retry ordinal is read back out of `PublishedRequestID` — a mint at the iteration the field already names
      is the retry and takes `Next(published, true)`. The truncation budget moved with it:
      `handleLengthTruncation`'s gate is `publishedRetryOrdinal(loopID) + 1` (`ST:1403`), one spelling of one fact.
      Five caller sites removed, not the three the design enumerated — `H:2037` (`Increment`), `H:1389`/`H:1409`
      (forward-progress resets) AND `H:2054`/`H:2072`, two further `ResetTruncationRetry` calls inside
      `handleLengthTruncation`'s own failure arms that the design's enumeration missed.
      `IncrementTruncationRetry` (`ST:473`) and `ResetTruncationRetry` (`ST:485`) are NOT deleted: both are
      **exported methods on a Tier 1 package** (ADR-106, `release/tier1-packages.txt:75`), which the checkpoint brief
      reserves for the owner. They and `truncationRetryAttempts` (`ST:112`, cleared at `ST:596`) are marked
      `Deprecated:` naming the durable replacement, and have no production caller.
      Tests: `execution_identity_test.go` `TestRequestIDCarriesTheTruncationRetryOrdinal` (rewritten onto
      `SetPublishedRequest`, the production seam) and the new `TestRetryOrdinalSurvivesARebuiltLoopManager` (a
      SECOND manager holding only the durable record mints `:3:2`, the name a process-local counter could not);
      `published_request_identity_test.go` `TestEveryMintedRequestIsNamedOnTheLoopRecord` (all three mint sites, in
      one run, through `HandleTask` → truncation retry → tool batch); `truncation_branch_test.go`
      `TestHandleLengthTruncation_BudgetRenewsOnTheNextIteration` replaces
      `TestHandleLengthTruncation_ResetAfterForwardProgress`, which encoded the counter's semantics (a tool-call
      response mid-iteration renewed the budget); the durable rule is one self-heal per ITERATION, and the new test
      drives a real tool batch to the advance.
      Mutant: delete the `SetPublishedRequest` call from `publishIterationRequest` (`H:2956-2958`) →
      `TestEveryMintedRequestIsNamedOnTheLoopRecord`, `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` and
      `TestHandleLengthTruncation_BudgetRenewsOnTheNextIteration` red.
- [x] 2.5 Step 0 for every cold read but the task lane (design § 3.6): read the newest retained request (task 2.3's
      reader), order it against `PublishedRequestID` with `looprequest.Parse`/`Compare` (task 1.0); newer →
      `Update(revision)` the record before classifying — field, `Iterations = parsed iteration − 1` (`ST:1345`: a
      record's request carries `Iterations + 1`), `PendingToolResults = nil` (the shape the advance itself leaves on
      `main`, `H:2870` → `ST:1121`), and `PendingApproval = nil` + `State = running` when a gate was pending; no entry
      is synthesized. Older/unparseable → Fatal → Quarantine. Wire at the cold arms `C:1700`
      (`settleResponseWithoutLoop`) and `C:2292` (`settleToolResultWithoutLoop`); NOT the task lane (Q1) and NOT cancel
      (`C:2593-2612` classifies by `State` only). Test: unit via the evidence-reader seam — equal / newer-adopt (record
      passes `Validate`, `AG:136`; applied set empty; I4 holds) / newer-adopt over an `awaiting_approval` record (gate
      cleared, `State = running`) / older / unparseable.
      **Landed** as `adoptNewerRetainedRequest` (`loop_evidence.go:271`), wired at both cold arms:
      `settleResponseWithoutLoop` (`C:1775`) and `settleToolResultWithoutLoop` (`C:2446`), each BEFORE the arm
      returns. The task lane and cancel are untouched. The gate clear is `LoopEntity.ResolveApproval` (`AG:246`) —
      the existing owner of that transition — which restores `StateBeforeApproval`; there is no `running` state in
      the vocabulary, and `StateBeforeApproval` is what "running" names. The adopted record is `Validate`d before it
      is written, and the write is `Update(record.revision)`, so a record that moved under the read is a Retry, not
      a silent overwrite.
      L4a note: the classification that follows step 0 is tasks 3.1/3.2, so today the arm still returns its existing
      not-held error after adopting. The adopt is idempotent — the redelivery reads the record it just wrote and
      compares equal.
      Test: `loop_carrier_test.go` `TestColdReadAdoptsTheNewestRetainedRequestFirst`, seven arms — newer-adopt
      (`published_request_id`, `iterations = parsed − 1`, applied set emptied, `Validate` green), newer-adopt over an
      `awaiting_approval` record (gate nil, state restored), current (nothing written), nothing retained (nothing
      written), record naming a request the stream never retained (Quarantine), unparseable retained (Quarantine),
      unparseable record field (Quarantine).
      **Same blind spot, same remedy:** deleting the `adoptNewerRetainedRequest` call from BOTH cold arms
      (`C:1775`, `C:2446`) left all seven arms green. `TestColdSettlementArmsAdoptBeforeTheyRefuseTheDelivery`
      drives `handleResponseMessage` and `handleToolResultMessage` through `deliverylane.Consume` and reads the
      record afterwards; both its arms go red on that mutant.
- [x] 2.6 Birth by `Create` and CAS-loss release (docket OQ3, owner ruling 2026-09-22): birth writes the record with
      `loopsBucket.Create` (`KV:211`) so a second consumer's birth is refused with `ErrKVKeyExists` (`KV:218`) and
      takes the cold fork; a CAS loss anywhere on the carrier releases the loop's process state (`ST:574` `DeleteLoop`,
      `C:1964` `releaseLoopTransientState`) before it returns Retry. Test: `persist_handler_result_test.go` — a second
      birth for the same loop ID is refused and forks cold; after a CAS loss the loop holds no in-memory state and the
      redelivery reads the winning record.
      **Landed.** `createLoopState` (`C:2663`) writes birth with `Create` and seeds the loop's revision; the refusal
      is `natsclient.ErrKVKeyExists` and birth answers it by releasing the loop it just built in memory and
      returning Retry (`C:1535-1547`). `natsclient.IsKVConflictError` is the shared classifier at both sites — the
      component holds a raw `jetstream.KeyValue`, not a `natsclient.KVStore`, so `KV:211`/`KV:231` are the pattern
      and not the call; the conflict class is unambiguous by call site (Create ⇒ key exists, Update ⇒ revision
      moved). Recorded, because it is the one place the design's pins do not resolve to the call this makes.
      CAS-loss release: `persistLoopState` (`C:2721-2727`) calls `releaseLoopTransientState` before returning, which
      takes `DeleteLoop` (`ST:581`) and `forgetLoopRevision` with it.
      **In-scope gap, recorded:** "takes the cold fork" is task 3.4's; until it lands a refused birth Retries
      instead of forking, bounded by the lane's MaxDeliver.
      Tests: `loop_carrier_test.go` `TestBirthRefusesASecondCreateForTheSameLoop` and
      `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` (the loop is gone from memory and its revision with
      it).
      Mutants: `Create` → `Put` in `createLoopState` (`C:2674`) → `TestBirthRefusesASecondCreateForTheSameLoop` red.
      Deleting the `createLoopState` CALL at birth (`C:1535-1547`) left the package green — the third pin the ritual
      bought: `TestBirthRecordsTheLoopBeforeItPublishes` drives `handleTaskMessage` with an unpublishable client, so
      a record in the bucket can only have been written before the publish that failed, and it goes red there.
## 3. Lane classification (3.9 is L4b's — see "Moved to L4b")

- [ ] 3.1 Tool-result classification: nothing to delete on `main`; build it at component entry `C:2195` (ahead of
      `H:2546` and `H:2580`) and in `C:2292`'s live arm — cold → 2.5 first; classify `result.RequestID` against
      `PublishedRequestID` (older → ACK; newer → Retry; unknown → Quarantine). Test: write
      `tool_result_recovery_test.go` (absent on `main`).
- [ ] 3.2 Response lane: cold → 2.5 first at `C:1698-1700`'s live arm; the warm superseded-response guard at `H:1253`
      (`CurrentRequest`, process-local and empty after replacement — L2's residual `ST:955-961`) compares against the
      record's `PublishedRequestID`; newer → Retry. Test: write `settlement_recovery_test.go` (response-lane cases).
- [ ] 3.4 Task lane: a cold fork before `C:1396` (`HandleTask`) — read the record by `task.LoopID`; absent → birth;
      present at iteration 0 → rebuild R1 through `buildTaskRequest` (`H:1120`) and republish it unconditionally with
      the MsgId, no retained read (Q1); present and advanced → ACK. Test: `recovery_test.go` (exists, extend) — cold
      redelivery at iteration 0 and after advance.
- [ ] 3.5 Terminal + unproven result (Q7): effect-free ACK with a `WarnContext` audit line; two new reason values on
      the existing `tool_results_dropped_total` (`older_request`, `terminal_unproven`; metric name `M:169`, recorder
      `M:527`, the reason values enumerated in its doc comment `M:514-526`, existing use `C:2300`) rather than a new
      counter; the warm check is inserted before `HandleToolResult` at
      `C:2195`, because `H:2546` and `H:2580` precede the lane's only terminal guard at `H:2652`. Test: write
      `terminal_tool_recovery_test.go`; both reason values asserted.
- [ ] 3.8 In-flight answer (the MODIFIED requirement's new SHALL NOT): a test citing that requirement (`// spec:` line;
      `git grep 'acknowledgement floor' -- '*_test.go'` is empty — add it beside the in-flight query) with a case where
      the record names `published_request_id` and a non-empty `pending_tool_results` while the process is gone and the
      answer is still consumer bookkeeping; `git grep -n 'PublishedRequestID\|published_request_id' -- ':!processor/agentic-loop' ':!agentic'`
      returns only docs/spec.
- [ ] 3.10 Component-entry classification for a duplicate terminal tool result (docket OQ5, owner ruling 2026-09-22):
      the check sits at `C:2195`, and `TransitionTo`'s same-state `nil` (`AG:181`) stays untouched — it is a legitimate
      no-op for other callers. Test: a duplicate `StopLoop` result delivered to a loop whose record is `complete`
      publishes nothing and moves no record timestamp (the record is not written at all), in
      `terminal_tool_recovery_test.go`.

## 4. Property and window evidence

- [ ] 4.1 New `processor/agentic-loop/applied_facts_property_test.go`: Rapid state machine over an in-memory KV and a
      fake evidence reader; actions deliver / crash-at-{W1,W2,W3,W4} / redeliver / replace-process on the tool and
      response lanes; checks I1–I4 after every step (I2 as membership, never rendering) and "no duplicate request
      published unless absent from the fake stream". The approval-response lane's three shapes use named examples and
      land with L4b (#1362).
- [ ] 4.2 Real-NATS W2 and W4 on the tool lane — the W4 case RESTARTS the process via `test/e2e/harness/processbarrier`
      (replacement, not a fresh handler) and asserts the adoption-first write — plus W2 and W4 (truncation retry) on the
      response lane; assertions read `AGENT_LOOPS` (`published_request_id`, `iterations`, `state`) and count messages
      per subject, never bodies. Files: new `tool_result_redelivery_integration_test.go`, new
      `identity_adoption_integration_test.go`. The approval-lane reject W4 and the terminal lane's
      crash-after-publish-before-`Update` are L4b's.
- [ ] 4.3 Mutation evidence (`cp` backup + checksum, never stash): (a) restore Put-before-publish in 2.1 → W4 in 4.2
      fails; (b) restore plain `Put` → the CAS case in 2.1 fails; (c) skip adoption in 2.3 → the 4.1 property fails;
      (e) skip step 0 in 2.5 → the restarted W4 case in 4.2 retries to `MaxDeliver`. (d) and (f) are L4b's.

## 5. Docs and spec

- [ ] 5.1 Correct `docs/concepts/17-approval-flow.md:65` and `processor/agentic-loop/doc.go` restart claims to state the
      I1–I4 contract and its prerequisites (#1327–#1329).
- [ ] 5.2 Apply the `specs/agentic-loop/spec.md` and `specs/agentic-dispatch/spec.md` deltas;
      `openspec validate agentic-loop-durable-applied-facts --strict` green; `task spec:properties` resolves the `// spec:` citation from 4.1 against the ADDED requirement.
- [ ] 5.3 No approval-deadline hydration (docket OQ2, owner ruling 2026-09-22): the delta scenario "a replaced process
      re-arms no approval deadline; the loop stays `awaiting_approval` until answered or cancelled" plus the same
      sentence as a line in `docs/operations/migration-beta162-to-beta163.md` under a `#1330` section. Test: the zero
      is a measured delta, not a bare absence. The same test first arms a deadline in-process — a real
      `awaiting_approval` record whose deadline the live component's snapshot (`AS:69`
      `SnapshotExpiredApprovals`) reports — then starts a REPLACEMENT component over the same KV and asserts the
      replacement's snapshot is empty, the record is still `awaiting_approval`, and its revision is unchanged.
- [ ] 5.4 `tasks_submitted_total` is at-least-once under redelivery (docket OQ4, owner ruling 2026-09-22): the new
      `specs/agentic-dispatch/spec.md` delta states it, plus one line in
      `docs/operations/migration-beta162-to-beta163.md` under the same `#1330` section; no arm change. Test: a
      dispatch-side unit test carrying `// spec: agentic-dispatch / The task submission counter is at-least-once
      under redelivery` and asserting the delta's scenario — a replayed task submission increments the counter again
      while reusing the retained LoopID and publishing no second task
      (`processor/agentic-dispatch/metrics.go:112`, `recordTaskSubmitted` `:321`, increment `:322`).

## 6. Verification (before the push, every time)

- [ ] 6.1 `task check:push` (schema drift expected empty — `LoopEntity` is in no schema); `go run ./cmd/entity-id-audit .`
      green.
- [ ] 6.2 `task e2e:agentic` with the process-replacement stage (`stage_a_process_replacement.go`,
      `process_replacement_test.go`) named in the PR body with exit codes; BREAKING for the recovery contract, so this
      tier is the gate (`docs/contributing/02-e2e-tests.md` § Breaking Changes). The approval-after-restart stage is
      L4b's.

## Moved to L4b (#1362)

Plain bullets, deliberately not checkboxes: these are #1362's tasks, listed so the reader sees exactly what left this
change and where it went. Owner ruling 2026-09-22, OQ7, plus the coordinator's scoping of the carrier reorder under
OQ7/OQ8 (`design.md` § 1); the split line is `reconciliation.md` § G.

- 3.3 Approval lane — the cold KV branch at `ARH:58` on `ErrLoopNotFound` (today `staleDrop` `ARH:78` → Ack
  `ARH:194-199`), I4 plus key presence against the record it just adopted, `republishPendingApproval` built inside it,
  and the warm re-echo at `H:2674`. Tests: `approval_timeout_recovery_test.go`, `approval_restore_order_test.go`.
- OQ1 `continuation_unavailable` — the ruled outcome for confirmed-absent approval evidence (#1146 acceptance line;
  owner ruling 2026-09-13), one branch, built with the approval lane.
- 3.6 Governance verdict after waiter loss (Q6) — `C:2734` → `settleVerdictWithoutWaiter` (`C:2759`) reads the record
  and classifies (Ack older/consumed/terminal with a reason on `M:393`; Retry current-unseen). Test:
  `verdict_wire_test.go`.
- 3.7 One terminal owner — three `Put` paths (`C:1974`, `C:1752` → `:1809` → `:1831`, `C:2631` → `:2668` → `:2683`)
  become marker `Create` (`KV:211`/`KV:218`) → stamps → publish → entity `Update` (`KV:231`), clearing
  `PendingApproval` on the terminal transition (L3's deferred item). Test: `terminal_owner_test.go` (a)/(b)/(c).
- 3.9 Route-ambiguity metering (owner ruling 2026-09-21; docket OQ6) — meter `loop_route_ambiguous` on
  `loop_admission_refusals_total` inside `activeLoop` (`processor/agentic-dispatch/http_activity.go:334`,
  `metrics.go:180`) with one resolver-seam reason value; retire the two post-refusal absence assertions in
  `command_target_resolution_test.go` (`:335`, `:349`), keep the 409 assertion at `:346-348`. Never opened here.
- 2.1 (approval lane and sweeper order) — flip the approval lane's call site (`ARH:205`) and the sweeper's own
  publish-then-`Put` pair (`AS:100-101`, D39) to publish → CAS `Update`, so the auto-reject takes the same order and
  the same CAS as an operator rejection. It moves with the lane because the reject-minted W4 the flip opens is closed
  only by the approval lane's cold branch (3.3 above). L4a changes the writer for these callers, never the order.
- 2.7 Gate order, conditional (docket OQ8) — write the test that an approval answer arriving before its gate is
  durable is Retried by the approval-response lane's cold KV branch. If it can be written, the approval gate takes
  the uniform publish → `Update` order; if it cannot, the accepted write → publish branch for the gate stands and
  #1362's PR body records which shipped and why. Test: `persist_handler_result_test.go` (gate branch) plus the lane
  case named above.
- 4.2 (approval and terminal cases) — the approval-response lane's reject W4 (crash between the carrier's publish and
  its `Update` for the approval result, restart, redeliver → ACK inapplicable, R(N+1) counted once, record `running` at
  R(N+1) with no gate) and the terminal lane's crash-after-publish-before-`Update`, in a new
  `terminal_tool_redelivery_integration_test.go`.
- 4.3 (d) and (f) — restore `Put` for the marker in 3.7 (drop the `Create` read-back) → the terminal case in 4.2 fails;
  drop the gate clear from 2.5 → the approval-lane W4 case in 4.2 fails on I4 and re-runs the rejection.
- 6.2 (approval stage) — the `task e2e:agentic` approval-after-restart stage (`approval_restart.go`,
  `approval_restart_test.go`, both absent on `main`), which asserts the answer is applied after replacement, not that a
  deadline fires.

## Delta text carried to L4b (#1362)

Not tasks. This is the delta text that was written for L4a's `agentic-loop` delta and moved out of it because it
states #1362's behaviour (tasks 3.3, 3.6, 3.7 above). It is held here verbatim so #1362 seeds its own delta from it
and nothing is lost. The header clause it replaces — "L4b is #1362, which adds no delta of its own beyond what is
stated here" — is deliberately NOT carried: #1362 carries its own delta.

```markdown
<!-- from the ADDED requirement's opening sentence: the two lanes L4b classifies -->
... and every redelivered model response, tool result, approval response, and governance verdict SHALL be classified
against that field and against `pending_tool_results` rather than against retained conversation content.

<!-- from the same requirement, after the compare-and-swap sentence -->
A redelivered terminal input SHALL adopt the loop's durable terminal by loop ID and terminal kind.
Recovery SHALL never compare rendered messages, result content, or terminal content to decide whether an input was applied.

#### Scenario: A cold replacement adopts past a rejection-minted request and acknowledges the stale approval response

- **GIVEN** a loop `awaiting_approval` at `R` whose gate was rejected, where the rejection minted and published `R(N+1)`
  and the process crashed before the record was updated
- **WHEN** the approval response is redelivered to a replacement process with no memory of the loop
- **THEN** the replacement writes the record to `R(N+1)` with the gate cleared and `state = running` under
  compare-and-swap, classifies the approval response as inapplicable, acknowledges it, and publishes nothing

#### Scenario: A governance verdict redelivered after its waiter is gone

- **GIVEN** a loop that restarted after proposing execution `e` under request `R` and later applied `e`'s result
- **WHEN** the verdict for `e` is redelivered and no waiter exists
- **THEN** the loop reads its record, finds `e` in `pending_tool_results` or `R` older than
  `published_request_id`, and acknowledges the verdict without dispatching it

#### Scenario: A redelivered terminal adopts the published terminal by identity

- **GIVEN** a loop whose durable terminal (`COMPLETE_<loopID>`) exists and whose record is not yet terminal, because the
  process crashed after publishing the terminal event and before the record update
- **WHEN** the input that produced the terminal is redelivered and this delivery derives a terminal whose content differs
- **THEN** the loop adopts the durable terminal by loop ID and terminal kind, publishes it, writes the record terminal under
  compare-and-swap, acknowledges, and logs the content difference at the audit line without retrying or quarantining
```

L4a's own delta keeps the remainder, with the compare-and-swap sentence scoped to the model-response and tool-result
lanes and loop birth, and the approval lane and approval-timeout sweeper named as keeping their present order
until #1362.
