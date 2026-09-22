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

- [x] 1.0 New `processor/agentic-loop/internal/looprequest`: `Parse(id) (ID, error)` over `<loopID>:req:<i>:<r>` (loopID may itself contain colons; parse from the right, require all four parts, reject a missing retry part), `Next(prev, retry bool) string` (retry → same iteration, retry+1; else iteration+1, retry 0), `Compare(a, b)` on `(iteration, retry)`. `Next` must reproduce `ST:1345-1347` (`iteration = entity.Iterations + 1`; `%s:req:%d:%d`), so birth mints `…:req:1:0` from `Iterations = 0`. `Parse` becomes the one reader of the grammar, replacing the two prefix-only readers on `main`: `ST:1360-1361` (`ExtractLoopIDFromRequest`) and `LP:50` (`loopIDFromStructuredID`). L2 (PR #1335) shipped no parser because no reader existed yet; this change is the reader. Rapid property: `Parse(format(x)) == x`; `Compare` is a total order consistent with `Next`.
      **Landed.** `looprequest.go` — `ID{LoopID,Iteration,Retry}`, `Parse` (right-hand read; canonical ordinals
      only — `007`, `+1` and `iteration 0` are refused), `ID.String`, `Next(prev, retry)`, `Compare(a, b)`. The task
      line above says `Parse(id) (ID, error)` because that is what shipped and what the rest of the change calls;
      the design wrote it as a three-value tuple, and the three parts travel together at every call site, so they
      get a named value rather than three positional returns (developer contract, exported-surface rule). Corrected
      here 2026-09-22 so the task text and the code agree. `Parse` did NOT replace the
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
- [x] 1.2 `processor/agentic-loop/state.go`: `SetPublishedRequest(loopID, requestID)`; new `restoreLoopFromRequest`
      beside `attachContinuation` (`ST:298`) — no rebuild path exists on `main` — fed the record step 0 (2.5) already
      adopted plus the newest retained request; it rebuilds the ContextManager, the routing maps (`ST:79`
      `outstandingRequests`, `ST:887-888`) and the tool batch (`restoreToolBatch`, membership against the retained
      response only), so it has no `PublishedRequestID` mismatch to refuse. Test: `state_test.go` — set /
      restore-after-adoption / restore-equal.
      **Landed.** `SetPublishedRequest` is at `state.go:914-924`, refusing a loop the manager does not hold rather
      than ignoring it, and has three production callers (the mint sites of 2.4). The rebuild landed in two
      commits: `6c91a1cc` for the two manager primitives, `c09cc282` for the reader, the component seam and the
      call sites — sequenced that way because the shape of the ContextManager rebuild is decided by its call sites,
      never ahead of them.

      Five parts, as built:

      1. `LoopManager.restoreLoopFromRequest(record, request)` (`state.go:359`) — seats the loop, its
         ContextManager and its routing maps from the record plus the retained request. The conversation replays
         into ONE region: `system` → `RegionSystemPrompt`, everything else → `RegionRecentHistory` in the retained
         order, then `RepairToolPairs()`. It also restores `cachedTools` / `cachedToolChoice` /
         `cachedResponseFormat` / `cachedRequestTimeout` off the request — an addition to the task text, because
         without them a rebuilt loop's NEXT request advertises no tools at all. The loop is marked outstanding on
         its request (TrackRequest's shape), which `restoreToolBatch` settles when a response for it is in hand.
      2. `LoopManager.restoreToolBatch(loopID, response, applied, inFlight)` (`state.go:462`) — re-derives every
         execution identity from the retained response with `stampToolExecutionCorrelation`, adds the assistant
         turn the batch belongs to, seats names/arguments/ordinals for all of them, seats routes for the unapplied
         ones only, and queues the unapplied minus `inFlight`. **Divergence from the task text:** a fourth
         parameter, `inFlight`. Dispatch is serial, so the call whose result is arriving must not go back on the
         queue; folding it into `applied` would have meant inventing a fake ToolResult value for it.
         `pendingTools` is deliberately left empty — the queue is what says how much of the batch is left, and
         `HandleToolResult` dispatches from it before it ever asks `AllToolsComplete`.
      3. `loopEvidenceReader.ReadRetainedResponse` (`loop_evidence.go:56`) with `responseAddress`
         (`loop_evidence.go:124`) and `readRetainedAgentResponse` (`loop_evidence.go:216`). The address resolves
         from the agent.response **INPUT** port — `requestAddress`'s mirror — so the recovery read and the live
         subscription resolve the same subject after a config change. Both reads share one `newestOn`.
      4. `Component.restoreLoopFromEvidence(ctx, loopID, record, inFlightExecutionID)` (`loop_evidence.go:577`),
         called from both cold arms on `requestOrderCurrent` only (`component.go:1905`, `component.go:2653`), which
         then fall through to the ordinary warm apply (`component.go:1749`, `component.go:2451`) — no second apply
         path for a recovered loop. It ends with `rememberLoopRevision`. **Divergence from the task text:** the
         retained-RESPONSE read fires on the tool lane only. On the response lane, applying the response is what
         creates the batch; pre-seating one would add the assistant turn to the conversation twice.
         `requestOrderUnnamed` and `requestOrderAhead` still retry: neither names a request to rebuild from.
      5. Tests. Unit: `loop_rebuild_test.go` drives the two manager primitives
         (`TestARebuiltLoopIsTheRecordPlusItsRetainedRequest`, four arms — full rebuild, orphan assistant repaired
         away, mismatched request refused, held loop not rebuilt over — and
         `TestARestoredToolBatchKnowsWhatIsLeftToRun`) and both component seams
         (`TestAColdResponseRebuildsTheLoopItAnswers`, `TestAColdToolResultRebuildsTheBatchItBelongsTo`).

      **The W2 halves of 4.2** (moved here 2026-09-22, checkpoint-2 review) are REWRITTEN, not deleted, from
      "retries, writes nothing" to "rebuilds and applies":
      `tool_result_redelivery_integration_test.go` § "W2: the result was applied in memory and the record never
      learned it" and `identity_adoption_integration_test.go` § "W2: the dispatch landed and the record never
      learned it". Both now assert Ack, the replacement HOLDING the loop, and the record moving past the revision
      the crash left — the invariant that survives either way is that the input is never acknowledged away and the
      record never moves on a delivery nobody applied. The tool-lane case gained `retainModelResponse`, which
      publishes the model response to `agent.response.<requestID>` exactly as agentic-model does: a test that calls
      `HandleModelResponse` directly skips the delivery that would have put the durable fact there, and the rebuild
      needs it.
      Recorded residual, not a defect: the record says which executions are APPLIED, never which were DISPATCHED,
      so the rebuild re-dispatches a sibling the predecessor had already sent. Both cases assert the re-dispatch and
      name why it is a replay rather than a duplicate execution — the execution identity is derived, not minted
      (L2), and agentic-tools keys `TOOL_CALL_OUTCOMES` by it.

      **Mutation evidence** (`cp` backup + `md5 -q`, `[applied]` printed between mutating and testing, restore
      verified by checksum, `git status --porcelain` clean after each):
      - (i) `restoreLoopFromEvidence` stops remembering the record's revision. `loop_evidence.go`
        `ec65ad839dff6d229aa583cdbf818ad8` → `b3024025358db0a5c4363037e9b32c3f` → restored
        `ec65ad839dff6d229aa583cdbf818ad8`. RED: `TestAColdResponseRebuildsTheLoopItAnswers` ("the rebuilt holder
        could not write the record it had just read"), and both real-NATS W2 cases ("a response/result the rebuilt
        loop applied is settled, not owed to a process that will never exist").
      - (ii) `restoreLoopFromRequest` stops calling `RepairToolPairs()`. `state.go`
        `ceac885addaa74f5d8dcd8fbfc74b2cc` → `d3474b75b5ef734e1875020d92b8f501` → restored
        `ceac885addaa74f5d8dcd8fbfc74b2cc`. RED:
        `TestARebuiltLoopIsTheRecordPlusItsRetainedRequest/an assistant turn whose results never arrived is
        repaired away`.
      - (iii) `restoreToolBatch` restores the batch without skipping the applied executions. `state.go`
        `ceac885addaa74f5d8dcd8fbfc74b2cc` → `e5cc0d199aa81fde8f60d803c686006a` → restored
        `ceac885addaa74f5d8dcd8fbfc74b2cc`. RED: `TestARestoredToolBatchKnowsWhatIsLeftToRun` and
        `TestAColdToolResultRebuildsTheBatchItBelongsTo` ("the sibling that never ran must be dispatched next; an
        applied one must not be re-run").

      `go test -race -count=3 -tags=integration -p 2` over the two rewritten W2 files plus
      `task_redelivery_integration_test.go`: `ok … 5.242s`.

      **Amended 2026-09-22 (checkpoint-3 review, BLOCKING-2): a retained request is not the conversation.**
      All three mint sites wrap the conversation in `prependIterationContext` (`handlers.go:316`), so the retained
      `AgentRequest` opens with an `[Iteration Budget]` line and, when the loop has a working list, a
      `[Working list …]` block — both Role `system`. The rebuild seated them into `RegionSystemPrompt`, pinning ONE
      iteration's framing at the top of the loop's system prompt for the rest of its life while every later request
      prepended a fresh one; the reviewer read `[Iteration Budget] Iteration 1 of 20 (5% used).` back off the
      rebuilt context on the real-broker W2 path. `isIterationPrefixMessage` (`handlers.go`, beside the function it
      inverts) is the filter, and `restoreLoopFromRequest` drops the LEADING run only — a user may type either
      string, and a message further in is the conversation. The two opening literals are now constants both builders
      and the predicate read, so the recogniser cannot drift from the writer.
      Two claims corrected with it: the doc comment no longer asserts the retained body IS `GetContext()`'s order
      without qualification, and it names the second residual this exposed — system messages are re-seated together
      at the front, so a request that interleaved one does not get that interleaving back. The migration note says
      both in adopter terms.
      Tests, fixtures built by the PRODUCTION prefixer rather than by hand: `loop_rebuild_test.go` "the per-iteration
      prefix belongs to the request, not to the conversation" (drives `handler.prependIterationContext` with a real
      todo list, asserts the rebuilt `GetContext()` equals the un-prefixed conversation) and "a message that only
      looks like the prefix is still the conversation" (the leading-only rule). On the real broker the W2 tool-lane
      case asserts the rebuilt conversation opens on the loop's own task and that no message carries either prefix.
      Mutant (iv): delete the leading-prefix skip in `restoreLoopFromRequest`. `state.go`
      `eaba19f1844225c316baeed4e7dff350` → `c00431a5f7ccb0e9ed6d5570104a24b1` → restored
      `eaba19f1844225c316baeed4e7dff350`; RED on the unit arm (exit 1) AND on the real-NATS W2 arm (exit 1), green
      on restore, `git status --porcelain` empty after.

      **Declared degrade, metered (checkpoint-3 NIT-3).** The rebuild's `startTrajectory` failure continues with a
      warn; it now also counts `recovery_degradations_total{site="rebuilt_trajectory_aggregate"}`. No test asserts
      that increment, deliberately: `trajectoryManager.startTrajectory` (`trajectory.go:24-32`) returns `nil`
      unconditionally, so the branch is unreachable today. The instrumentation is there for the day it is not;
      inventing a fake failure to assert it would test the fake.

      **Migration note.** `docs/operations/migration-beta162-to-beta163.md` § "A rebuilt loop's conversation is one
      region" records the one visible consequence of the one-region replay: compaction attribution does not survive
      a process replacement. No message is lost and none moves, but per-region sizes reset at the replacement. The
      owner ruled the one-region shape stands with no objection
      ([issuecomment-5776942078](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078)).

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
      **Defect found by CI on the pushed head `cdd2c18b` and fixed inside this change** (run 35719155514,
      `TestIntegration_CancelMidExecution_NoOrphanToolCalls`: the loop stayed `exploring` where the test required
      `cancelled`). The new CAS writer read the revision it compares against as a step SEPARATE from the write, so
      two lanes of ONE process — the cancel signal and the tool lane's advance — interleaved between the read and the
      `Update`, and the loser's compare-and-swap was refused by its own process. A refusal is indistinguishable from
      another process owning the loop, so the cancel path released the loop and reported unknown durability. Root
      cause, not a flake: render → observe revision → `Update` is one critical section and had no lock. Fixed by
      serializing that whole sequence per component (`loopRecordMu`, `C:99`; taken at `C:2811` in `createLoopState`
      and `C:2857` in `persistLoopState`) rather than by retrying a lost CAS — a blind retry would rewrite the record
      from an entity read before the write that beat it, which is the lost-update the CAS exists to refuse.
      Reproduction with
      `go test -race -count=100 -cpu 2,4 -tags=integration -run TestIntegration_CancelMidExecution_NoOrphanToolCalls ./processor/agentic-loop/`:
      **74 of 200 runs red at `cdd2c18b`; 0 of 200 after the fix** (`ok … 298.063s`, exit 0). The same command at the
      default GOMAXPROCS was 0 of 25 — the low CPU counts are what expose the interleaving.
      Test: `loop_record_writer_test.go` `TestTwoLanesWritingOneLoopDoNotRefuseEachOther` — a barrier bucket holds the
      first writer inside its `Update` and announces every entry, so the interleaving is forced by explicit
      synchronization; a second writer reaching the bucket at all fails the test.
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
      Two corrections from the internal review (2026-09-22). `readRetainedAgentRequest` with neither a NATS client
      nor an injected reader now returns a transient error rather than `found = false`: both callers read a false
      `found` as "this request has not gone out, publish it", so absence was standing in for unknown. And
      `readLoopRecord` no longer retains the revision it read on the component — every caller of it is asking about
      a loop this process does not hold, the per-loop entry is released only with a loop's own transient state, and
      so one entry accumulated per loop this process was ever asked about; the revision travels in the returned
      `loopRecord` to the one caller that writes with it (`adoptNewerRetainedRequest`), which does not retain its
      committed revision either. Test: `TestAColdReadRetainsNoRevisionForALoopItDoesNotHold`, three arms (a loop
      another process holds, a settled loop, and the adoption that follows a cold read).
- [x] 2.4 `handlers.go`: set the field at the three minting sites (`H:1122` in `buildTaskRequest`, `H:2168` in
      `emitRetryRequest`, `H:2927` in `publishIterationRequest`); mint via `looprequest.Next(PublishedRequestID)`
      (task 1.0); retry ordinal from the parsed field; delete `IncrementTruncationRetry` (`ST:466`) and
      `ResetTruncationRetry` (`ST:477`) with their callers `H:2037`, `H:1389`, `H:1409` and the map cleared at
      `ST:588`. Test: `handlers_test.go` — grammar holds; retry ordinal survives a rebuilt LoopManager.
      **Landed.** The field is set at `H:1138` (`buildTaskRequest`), `H:2190`
      (`emitRetryRequest`) and `H:2956` (`publishIterationRequest`), each beside the `TrackRequest` the site already
      made. `GenerateRequestID` (`ST:1375`) now mints through `looprequest`: iteration stays `Iterations + 1`, and
      the retry ordinal is read back out of `PublishedRequestID` — a mint at the iteration the field already names
      is the retry and takes `Next(published, true)`. The truncation budget moved with it:
      `handleLengthTruncation`'s gate is `publishedRetryOrdinal(loopID) + 1` (`ST:1403`), one spelling of one fact.
      Five caller sites removed, not the three the design enumerated — `H:2037` (`Increment`), `H:1389`/`H:1409`
      (forward-progress resets) AND `H:2054`/`H:2072`, two further `ResetTruncationRetry` calls inside
      `handleLengthTruncation`'s own failure arms that the design's enumeration missed.
      `IncrementTruncationRetry` and `ResetTruncationRetry` shipped one commit marked `Deprecated:` with no
      production caller, on the reading that removing an exported method from a **Tier 1 package** (ADR-106,
      `release/tier1-packages.txt:75`) was the owner's call. **Both are DELETED as of 2026-09-22**, under the
      coordinator's ruling on the checkpoint-1 review applying the standing no-deprecation rule: a helper whose last
      caller is gone goes with it. `truncationRetryAttempts` (the map, its two constructor inits and its clear in
      `DeleteLoop`) went with them — with both methods gone nothing read or wrote it. `task api:compat:report` adds
      exactly `(*LoopManager).IncrementTruncationRetry: removed` and `(*LoopManager).ResetTruncationRetry: removed`
      to the three incompatible entries `processor/agentic-loop` already carried, and the package total is
      unchanged at 15. Migration note: `docs/operations/migration-beta162-to-beta163.md` § "The loop record names its
      outstanding request, and the truncation-retry helpers are removed (#1330, restart safety L4a)", which records
      the measured sister result — a `grep -rn` over every `sem*` repository finds the two names only in frozen
      `.txt` copies of this repository's own source under `semdev/openspec/changes/.../evidence/`, and a control
      grep over the same tree set reaches sister Go sources.
      The one test caller, `populatedLoop`'s `IncrementTruncationRetry` line in `terminal_release_test.go`, is
      dropped with the map it populated: the durable ordinal is not a per-loop manager map, so that assertion has no
      subject to move to. `perLoopMapCount` loses the same entry and the fixture floor drops 13 → 12, keeping the
      slack that refuses a fixture which skips a map.
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
      **Cost, corrected 2026-09-22** (the checkpoint's first reading said a cold redelivery pays a KV write per
      redelivery; it does not): the FIRST cold read that finds a newer retained request pays one `Update`. Every
      redelivery after it re-reads the record it just wrote, `looprequest.Compare` returns 0 and
      `adoptNewerRetainedRequest` returns at `loop_evidence.go:312` before building anything — one KV write in
      total, then one record read plus one `GetLastMsgForSubject` per redelivery. That read pair is the standing
      cost of a lane whose classification is still tasks 3.1/3.2's, and it ends when they land.
      Test: `loop_carrier_test.go` `TestColdReadAdoptsTheNewestRetainedRequestFirst`, seven arms — newer-adopt
      (`published_request_id`, `iterations = parsed − 1`, applied set emptied, `Validate` green), newer-adopt over an
      `awaiting_approval` record (gate nil, state restored), current (nothing written), nothing retained (nothing
      written), record naming a request the stream never retained (Quarantine), unparseable retained (Quarantine),
      unparseable record field (Quarantine).
      **Same blind spot, same remedy:** deleting the `adoptNewerRetainedRequest` call from BOTH cold arms
      (`C:1775`, `C:2446`) left all seven arms green. `TestColdSettlementArmsAdoptBeforeTheyRefuseTheDelivery`
      drives `handleResponseMessage` and `handleToolResultMessage` through `deliverylane.Consume` and reads the
      record afterwards; both its arms go red on that mutant.
      **Amended 2026-09-22 (checkpoint-3 review, HIGH-1): step 0 adopts only into a record that NAMES a request.**
      `orderAgainstPublished` gives an empty `PublishedRequestID` the pre-#1330 answer on purpose —
      `requestOrderUnnamed`, retry to the loop's holder, settle nothing — and step 0 contradicted it: with the field
      empty the ordering guard was skipped entirely and the newest retained request was written onto the record
      together with `PendingToolResults = nil`, so the evidence the holder was about to apply was drained by the one
      process that could not apply it, on a delivery it then retried anyway. `adoptNewerRetainedRequest` now returns
      as soon as it reads an unnamed record: no stream read, no write. The seven arms above are unaffected — each
      seeds a record that names a request.
      Test: `loop_carrier_test.go` `TestStep0LeavesAnUnnamedRecordExactlyAsItFoundIt` — an unnamed record with a
      non-empty applied set and a newer request retained, driven through the real cold tool lane, asserting Retry
      with revision, applied set, request name and iteration count all unchanged.
      Mutant (v): make the early-return condition constant false in `adoptNewerRetainedRequest`. `loop_evidence.go`
      `9ac5734981c08360d67bb522e9a6f554` → `08695b3a2e35e77f08e61b859d278294` → restored
      `9ac5734981c08360d67bb522e9a6f554`; RED at "step 0 adopted into a record the classification refuses to order
      against" (exit 1), green on restore (exit 0), `git status --porcelain` empty after.
      One fixture moved with the rule: `TestAColdAdoptAndAWarmWriteOfOneLoopDoNotRefuseEachOther` seeded an UNNAMED
      record, so under the new arm the adopt it is about never reaches the bucket. Its loop now names a request,
      which is the state every loop past its birth is in.
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
      Birth's publish error is returned too, not discarded (internal review, 2026-09-22): the record written a line
      earlier names R1, and I1 says that while the record exists the stream retains that request, so acknowledging a
      birth whose publish did not land leaves the record naming a request nothing retains — the state § 3.6's I1 arm
      answers with Quarantine on every later cold read. It returns instead, and the task lane classifies it (an
      ordinary publish error is Retry).
      **In-scope gap, recorded:** "takes the cold fork" is task 3.4's; until it lands, a refused birth AND a birth
      whose publish returned both Retry instead of forking, bounded by the lane's MaxDeliver — a redelivery meets
      birth's own `Create`, is refused with `ErrKVKeyExists` and Retries again. Task 3.4 is what republishes R1 from
      the record, so it is a merge precondition for this change, not a later slice.
      Tests: `loop_carrier_test.go` `TestBirthRefusesASecondCreateForTheSameLoop` and
      `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` (the loop is gone from memory and its revision with
      it); the I1 pair at birth — either the stream retains the request the record names, or the delivery was not
      ACKed — is `TestBirthWhosePublishFailsIsNotAcknowledged` (unit: nothing retained, unconnected client, the
      lane answers Retry and the message is never ACKed) plus
      `TestIntegrationBirthAcknowledgesOnlyWhatTheStreamRetains` (real broker: the request is retained under the
      name the record carries and the delivery ACKs). Neither arm alone separates "acknowledges what was published"
      from "acknowledges regardless".
      Mutants: `Create` → `Put` in `createLoopState` (`C:2674`) → `TestBirthRefusesASecondCreateForTheSameLoop` red.
      Deleting the `createLoopState` CALL at birth (`C:1535-1547`) left the package green — the third pin the ritual
      bought: the birth test drives `handleTaskMessage` through the real task lane with an unpublishable client, so
      a record in the bucket can only have been written before the publish that did not land, and it goes red there.
      Re-discarding the birth publish error (`_ = c.publishResults(...)`) → `TestBirthWhosePublishFailsIsNotAcknowledged` red.
## 3. Lane classification (3.9 is L4b's — see "Moved to L4b")

- [x] 3.1 Tool-result classification: nothing to delete on `main`; build it at component entry `C:2195` (ahead of
      `H:2546` and `H:2580`) and in `C:2292`'s live arm — cold → 2.5 first; classify `result.RequestID` against
      `PublishedRequestID` (older → ACK; newer → Retry; unknown → Quarantine). Test: write
      `tool_result_recovery_test.go` (absent on `main`).
      **Landed.** One reading of the grammar for all four sites that ask: `orderAgainstPublished`
      (`processor/agentic-loop/loop_classification.go:57`) orders an incoming request name against the one the record
      names — applied / current / ahead / foreign / unnamed — so warm and cold, tool lane and response lane, cannot
      drift into four readings of one grammar. The tool lane's warm arm is `classifyRedeliveredToolResult` (`:174`),
      called at component entry (`C:2440`) BEFORE `HandleToolResult`, which stores the result and acts on `StopLoop`
      ahead of its own terminal guard: a result the loop has moved past has to be settled there or not at all. The cold
      arm classifies after step 0 (`C:2573`). Older → Ack with a counted drop; ahead of the record → Retry, not yet
      observable; not this loop's grammar → Quarantine.
      **Deviation, recorded:** the warm arm classifies against the loop's IN-MEMORY entity rather than re-reading the
      record. That entity is exactly what the carrier marshals into `AGENT_LOOPS`, and this process holds the loop, so
      a read would return what the process already has at the price of a round trip on every delivery. The cold arms
      read the record, because there the entity is what is missing.
      Tests: `tool_result_recovery_test.go` — `TestRedeliveredToolResultIsClassifiedBeforeTheHandlerTouchesIt` (older,
      not-yet-named, foreign, current) and `TestColdToolResultIsClassifiedAgainstTheAdoptedRecord` (superseded by the
      adoption, foreign). The real broker is 4.2.
      Mutant (`cp` backup + md5 + printed `[applied]`, restored and checksum-verified): delete the whole classification
      call at `C:2439-2447` → `TestRedeliveredToolResultIsClassifiedBeforeTheHandlerTouchesIt` (older, ahead) and
      `TestTerminalLoopAcknowledgesAToolResultWithoutEffect` (warm) red.
      **Declared degrade, metered (checkpoint-3 NIT-2).** When `GetLoop` fails between the routing lookup and this
      check — the loop was released mid-delivery — the classification is SKIPPED and the result goes to the handler
      anyway. That is safe (the handler answers the race as it did before the check existed) but it is a degrade,
      and a degrade is a declared event: the Warn line is now joined by
      `recovery_degradations_total{site="tool_result_classification"}`, because a log line is not something an
      operator can alert on. The counter drops no work and changes no delivery decision, and its Help says so.
      Test: `TestASkippedClassificationSaysSoInTheLog` asserts the line AND a delta of exactly one on that site.
      Mutant (vi): delete the counter call, keep the log. `component.go`
      `77064b5e84396c23a6fdf20a389a712e` → `ffb00040435af2668767b11d3bbfb627` → restored
      `77064b5e84396c23a6fdf20a389a712e`; RED at "a log line is not something an operator can alert on; a declared
      degrade carries both" (exit 1), green on restore (exit 0), `git status --porcelain` empty after.
- [x] 3.2 Response lane: cold → 2.5 first at `C:1698-1700`'s live arm; the warm superseded-response guard at `H:1253`
      (`CurrentRequest`, process-local and empty after replacement — L2's residual `ST:955-961`) compares against the
      record's `PublishedRequestID`; newer → Retry. Test: write `settlement_recovery_test.go` (response-lane cases).
      **Landed.** The warm superseded-response guard (`H:1274`) compares against the record's `PublishedRequestID`
      through the same `orderAgainstPublished`, and the process-local map it used to read is DELETED, not bypassed:
      `LoopManager.CurrentRequest` and the `currentRequests` map are gone from `state.go`, so the two spellings of "the
      loop's current request" cannot drift apart again. A response the record does not yet name is no longer a drop —
      the guard returns `errRequestNotYetObservable` (`H:1227`), which `handleResponseMessage` turns into a Retry
      instead of a loop failure (`C:1756`). The cold arm is step 0 then classify (`C:1855`, `C:1863`).
      **No deviation.** All four sites now give a FOREIGN request name the same disposition — Quarantine — as the
      delta requires: the warm guard returns `errResponseForeign` (`handlers.go`) and `handleResponseMessage` wraps it
      fatal, matching `classifyRedeliveredToolResult` and both cold arms. The warm APPLIED arm returns
      `errResponseSuperseded` and the component acknowledges it in place: an empty `HandlerResult` was still a result,
      and it flowed into `persistHandlerResult` → `persistLoopState`, whose compare-and-swap moved the record's
      revision for a delivery that changed nothing. The early return matches the tool lane's `if !apply { return nil }`.
      Tests: `settlement_recovery_test.go` — `TestAReplacementClassifiesAResponseAgainstTheRecordNotItsOwnMints` (four
      warm arms: superseded, not-yet-named, current, and the foreign name that is refused rather than dropped — the
      superseded arm also asserts the record's revision does not move) and `TestColdResponseIsClassifiedAgainstTheAdoptedRecord`;
      `superseded_response_test.go` still pins the metric and now pins the empty write list; `export_test.go`'s `CurrentRequestForTest` now reads the record's field, so no fixture can pass
      against a source production no longer has. The real broker is 4.2.
      Mutant: replace the guard's ordering with a constant `requestOrderCurrent` (`H:1274`) →
      `TestAReplacementClassifiesAResponseAgainstTheRecordNotItsOwnMints` (superseded, not-yet-named) and
      `TestSupersededResponseDoesNotSettleATimedOutLoop` red.
      **The quarantine found a reconstruction one package over** (CI run
      [35733719359](https://github.com/C360Studio/semstreams/actions/runs/35733719359), Test job).
      `processor/agentic-dispatch/terminal_loop_seam_test.go` births a REAL loop through `HandleTask` and then fed
      `HandleModelResponse` a response named `"seam-req"` — a value production never mints. It passed only because the
      old collapsed arm dropped-and-acked a foreign name; with the disposition corrected it quarantines, exactly as
      ruled. Fixed in the TEST: `publishedRequestID` reads the `Nats-Msg-Id` off the `agent.request.<loopID>` message
      the loop's own birth published, so the answer names the question the loop actually asked. Class swept before the
      fix: `git grep -nE 'RequestID:[[:space:]]*"[a-z-]+"' -- 'processor/**/*_test.go' 'agentic/**/*_test.go' 'test/**/*_test.go'`
      returns **55** hits, and `HandleModelResponse` has exactly ONE call site outside `processor/agentic-loop` — this
      one. (`8dc81425`'s commit body records this sweep with `\s` and a count of 56; `git grep -E` is POSIX ERE, where
      `\s` matches nothing and the command returns 0 — the POSIX class above is the one that was actually run, and 55
      is its count. The squash body must carry the corrected form, since a commit body is what reaches `main`.) The in-package literals (`delivery_owner_test.go:285`, `tool_result_handler_failure_test.go:193`) sit on
      loops built with `CreateLoop`, whose record names no request, so they meet `requestOrderUnnamed` and are fixtures
      rather than reconstructions. The 14 in-package files that both birth a loop and answer it are green under
      `-race`.
- [x] 3.4 Task lane: a cold fork before `C:1396` (`HandleTask`) — read the record by `task.LoopID`; absent → birth;
      present at iteration 0 → rebuild R1 through `buildTaskRequest` (`H:1120`) and republish it unconditionally with
      the MsgId, no retained read (Q1); present and advanced → ACK. Test: `recovery_test.go` (exists, extend) — cold
      redelivery at iteration 0 and after advance.
      **Landed.** `classifyRedeliveredTask` (`loop_classification.go:117`) is the cold fork, called before `HandleTask`
      (`C:1436`): no record → birth; a record at iteration 0 → `taskRepublishFirstRequest`; advanced or terminal →
      `taskApplied`, an Ack with an audit line naming the iteration and state; an unreadable record → a transient
      error, never a birth, because birthing on a failed read is a second loop under a name that may already have one.
      The republish arm rebuilds R1 through the ordinary birth path and takes the record's own revision as its own
      (`rememberLoopRevision`, `C:1573`), so the replacement becomes the holder without writing the record again.
      **Deviation from the task line, RATIFIED by the owner
      ([issuecomment-5776942078](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078),
      2026-09-22, verbatim "1330 agree with recommendation"):** the rebuilt R1 goes out through `publishResults`,
      which consults `adoptRetainedRequest` first, so an R1 the stream ALREADY retains is adopted rather than
      published a second time. The task line says "publish it unconditionally with the MsgId, no retained read (Q1)".
      Q1's point is that a birth must not be blocked behind a retained read, and it is not — adoption is a no-op when
      nothing is retained, which is the state the ruling describes. Where the two rules meet, publishing a second copy
      of a request the stream holds under the same name is the thing 2.3 exists to prevent. Q1 is amended to the
      adopt-not-republish reading; `design.md` § Conformance carries the row.
      Test: `recovery_test.go` `TestTaskRedeliveredToAProcessWithNoMemoryOfItsLoop` — six arms: iteration zero
      republishes R1; the replacement becomes the holder at the record's revision; an advanced loop Acks without
      effect; a settled loop Acks without re-birthing; no record at all is an ordinary birth; an unreadable record
      never births a second loop.
      Mutant: replace the classify call at `C:1436` with a constant `taskBirth` → four of the six arms red.
- [x] 3.5 Terminal + unproven result (Q7): effect-free ACK with a `WarnContext` audit line; two new reason values on
      the existing `tool_results_dropped_total` (`older_request`, `terminal_unproven`; metric name `M:169`, recorder
      `M:527`, the reason values enumerated in its doc comment `M:514-526`, existing use `C:2300`) rather than a new
      counter; the warm check is inserted before `HandleToolResult` at
      `C:2195`, because `H:2546` and `H:2580` precede the lane's only terminal guard at `H:2652`. Test: write
      `terminal_tool_recovery_test.go`; both reason values asserted.
      **Landed.** Two reason values on the existing counter and no new metric: `older_request` and `terminal_unproven`
      join `stale_execution` on `tool_results_dropped_total` (Help at `M:170`, the recorder's doc comment enumerating
      all three at `M:514-527`). Each drop carries a `WarnContext` audit line naming the loop, the execution and both
      request names (`loop_classification.go:181-204`, `C:2574-2580`). Q7 is unconditional: a terminal loop can apply
      nothing.
      **Deviation, recorded:** the terminal arm does not check `PendingToolResults` membership first. Membership would
      only distinguish "already applied" from "never applied" for a loop that can no longer apply either, and Q7 rules
      both to the same effect-free Ack — the branch's two sides would do the same thing.
      Test: `terminal_tool_recovery_test.go` `TestTerminalLoopAcknowledgesAToolResultWithoutEffect`, warm
      (`terminal_unproven`) and cold (`stale_execution`), each asserting the Ack, the counted reason and an unwritten
      record; `older_request` is asserted warm and cold in `tool_result_recovery_test.go` and on a real broker in both
      W4 cases of 4.2.
- [x] 3.8 In-flight answer (the MODIFIED requirement's new SHALL NOT): a test citing that requirement (`// spec:` line;
      `git grep 'acknowledgement floor' -- '*_test.go'` is empty — add it beside the in-flight query) with a case where
      the record names `published_request_id` and a non-empty `pending_tool_results` while the process is gone and the
      answer is still consumer bookkeeping; `git grep -n 'PublishedRequestID\|published_request_id' -- ':!processor/agentic-loop' ':!agentic'`
      returns only docs/spec.
      **Landed.** `inflight_test.go` `TestInFlight_ALoudLoopRecordIsNotAnInFlightAnswer` carries
      `// spec: agentic-loop / In-flight state MUST NOT be derived from the acknowledgement floor` and puts the record
      in the loudest state it can reach — `published_request_id` set, `pending_tool_results` non-empty, no process
      holding the loop — and asserts the in-flight answer is still consumer bookkeeping. Its bucket fails the test if
      the in-flight path reads the record AT ALL (`readCountingLoopBucket`), which is the assertion that survives a
      refactor of what the record happens to contain.
      `git grep -n 'PublishedRequestID\|published_request_id' -- ':!processor/agentic-loop' ':!agentic'` returns only
      `docs/operations/migration-beta162-to-beta163.md` and this change's own `openspec/` files: the field has no
      reader outside the two packages that own it. `task spec:properties` → `spec-properties: 312/312 citations
      resolve.`
- [x] 3.10 Component-entry classification for a duplicate terminal tool result (docket OQ5, owner ruling 2026-09-22):
      the check sits at `C:2195`, and `TransitionTo`'s same-state `nil` (`AG:181`) stays untouched — it is a legitimate
      no-op for other callers. Test: a duplicate `StopLoop` result delivered to a loop whose record is `complete`
      publishes nothing and moves no record timestamp (the record is not written at all), in
      `terminal_tool_recovery_test.go`.
      **Landed** inside 3.1's component-entry check: a duplicate `StopLoop` result meets the terminal arm at `C:2440`
      before `HandleToolResult` can act on it, and `LoopEntity.TransitionTo`'s same-state `nil` (`AG:181`) is left
      alone — it is a legitimate no-op for the other callers of a state machine this lane does not own.
      Test: `terminal_tool_recovery_test.go`, warm arm — a duplicate `StopLoop` result for a loop whose record is
      `complete` Acks, counts `terminal_unproven`, writes NO record (`bucket.written()` is empty after a reset, so not
      even a timestamp moves) and publishes nothing (the fixture's client cannot publish, so any publication would have
      failed the delivery). The cold arm proves the same for a loop no process holds.

## 4. Property and window evidence

- [x] 4.1 New `processor/agentic-loop/applied_facts_property_test.go`: Rapid state machine over an in-memory KV and a
      fake evidence reader; actions deliver / crash-at-{W1,W2,W3,W4} / redeliver / replace-process on the tool and
      response lanes; checks I1–I4 after every step (I2 as membership, never rendering) and "no duplicate request
      published unless absent from the fake stream". The approval-response lane's three shapes use named examples and
      land with L4b (#1362).
      **Landed.** `applied_facts_property_test.go` `TestPropAppliedFactsHoldAcrossEveryCrashWindow`: a Rapid state
      machine over the recording bucket and the evidence-reader seam with five actions — advance the loop (its crash
      point is a drawn bool, so "the record write never landed" is an ordinary draw, with or without a tool batch),
      replay the mint of the current request, replace the process, redeliver a tool result to a cold process, redeliver
      a model response to a cold process — plus an unnamed invariant action that checks I1–I4 and "no request is
      published twice while the stream holds it" after EVERY step. The decisions under test are production's:
      `adoptRetainedRequest`, `adoptNewerRetainedRequest`, `persistLoopState` and both cold lane arms run unchanged,
      and the model supplies only retention and crash points. I2 is checked as membership of what the request
      dispatched, never by rendering. 300 checks under `-race` in 11.09s.
      The approval lane's three shapes stay with L4b (#1362).
      Mutant (c) in 4.3 is what proves the property is not self-satisfying.
- [x] 4.2 Real-NATS W4 on the tool lane — the W4 case RESTARTS the process via `test/e2e/harness/processbarrier`
      (replacement, not a fresh handler) and asserts the adoption-first write — plus W2 and W4 (truncation retry) on the
      response lane; assertions read `AGENT_LOOPS` (`published_request_id`, `iterations`, `state`) and count messages
      per subject, never bodies. Files: new `tool_result_redelivery_integration_test.go`, new
      `identity_adoption_integration_test.go`. The approval-lane reject W4 and the terminal lane's
      crash-after-publish-before-`Update` are L4b's.
      **Landed** as two in-package files under the `integration` tag: `tool_result_redelivery_integration_test.go`
      (`TestToolResultRedeliveredToAReplacementProcess`) and `identity_adoption_integration_test.go`
      (`TestModelResponseRedeliveredToAReplacementProcess`), W2 and W4 on each lane.
      Every residue is built by dying where a process really dies. A real loop is born (`createLoopState` →
      `publishResults`), a real turn is taken through the handler and the real carrier, and only then does the
      component's loops bucket refuse its `Update` (`crashedBeforeRecordUpdate` — `Update` only; every read stays the
      real bucket). The publish that runs first has genuinely PubAck'd and the record write genuinely never landed, so
      no part of the durable state is written by hand. Assertions read `AGENT_LOOPS` fields (`published_request_id`,
      `iterations`, `state`, `pending_tool_results`, and the record's revision) and per-subject message counts from
      `stream.Info(WithSubjectFilter)`; no message body is compared.
      W4 on the tool lane: the record ends at `<loop>:req:2:0` with `iterations = 1` and an empty applied set, the
      delivery Acks as `older_request`, and `agent.request.<loopID>` still holds exactly two messages — the adopting
      process publishes nothing. W4 on the response lane: the truncation retry `<loop>:req:1:1` is adopted with
      `iterations` still 0 (a within-iteration retry advances nothing) and the response for `:req:1:0` Acks as
      `superseded_request`. **The W2 halves of both files are NOT this task's** — they pin a provisional settlement
      that the cold rebuild changes, so they moved under task 1.2 (split out 2026-09-22 at the checkpoint-2 review:
      a ticked task must not carry a provisional assertion).
      The task lane's own real-broker case lands here too: `task_redelivery_integration_test.go`
      (`TestTaskRedeliveredToAReplacementLeavesOneFirstRequest`) births a loop through the real task lane, hands the
      same bytes to a replacement, and asserts the delivery Acks while `agent.request.<loopID>` still holds exactly
      one message under `Nats-Msg-Id = R1`, with the record's revision unmoved. It exists because the unit arm of 3.4
      asserts only what the delivery does not do, and the `taskBirth` mutant satisfies that too; on a real stream it
      does not (re-run recorded on 3.4).
      **Deviation, recorded:** the process replacement is a second `Component` with its own `MessageHandler` over the
      same bucket and stream, not an OS process restarted through `test/e2e/harness/processbarrier`. That barrier is
      the agentic E2E tier's tool executor for holding a real app across a docker restart; inside a Go integration test
      there is no second OS process to restart, and everything the cold arms recover from — the routing maps, the
      context managers, the minted-request map — is process-local state a new `Component` genuinely does not have. The
      OS-process replacement is task 6.2's `task e2e:agentic` stage.
      `go test -race -tags=integration -count=3` over both tests: `ok … 4.198s`.
- [x] 4.3 Mutation evidence (`cp` backup + checksum, never stash): (a) restore Put-before-publish in 2.1 → W4 in 4.2
      fails; (b) restore plain `Put` → the CAS case in 2.1 fails; (c) skip adoption in 2.3 → the 4.1 property fails;
      (e) skip step 0 in 2.5 → the restarted W4 case in 4.2 retries to `MaxDeliver`. (d) and (f) are L4b's.
      **Landed.** Every mutant was taken on a committed tree: `cp` backup into the scratch directory, `md5 -q` before,
      a printed `[applied]` line between the edit and the test run, then restore, checksum match and an empty
      `git status --porcelain`. Baselines: `component.go` `d018dd58870c81f8a06e3f922505bc83`, `loop_evidence.go`
      `a34674f97e5206c77a8ebb4bafbd650a`, `handlers.go` `5a6990797e92deb80dad88fbd3aee75d`.
      (a) Delete the `publishThenWrite` dispatch in `persistHandlerResult` (`C:2122-2124`), restoring write-then-publish
      on every lane → all four cases of 4.2 red, the W4 arms at "the self-heal re-asks the same iteration under the
      next retry ordinal" and its tool-lane twin: a record write that fails FIRST means nothing is ever published, so
      the crash window cannot even form.
      (b) Replace the CAS `Update` in `persistLoopState` (`C:2881`) with a blind `Put` →
      `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` red ("Expected error with `kv: revision mismatch
      (concurrent update)` in chain but got nil"), and both 4.2 tests red as well — the crash double overrides `Update`
      only, so a writer that stops calling it can no longer be interrupted where a process really dies.
      (c) Neuter the adoption decision: `adoptRetainedRequest` (`loop_evidence.go:230`) returns `false, nil` →
      `TestPropAppliedFactsHoldAcrossEveryCrashWindow` red after 0 tests, `request "…:req:1:0" was published 2 times
      while the stream held it`. Its WIRING is pinned separately, because the property drives the decision directly:
      deleting the identity check from `publishResults` (`C:2609-2617`) leaves the property GREEN and turns
      `TestPublishingAMintedRequestConsultsTheRetainedIdentity` red — the second pin task 2.3's own ritual added for
      exactly this blindness, re-verified here.
      (e) Delete step 0 from both cold arms (`C:1855` and `C:2565`, each replaced by `adopted := record`) → both W4
      cases of 4.2 red with Ack (1) expected and Retry (2) observed, while the W2 cases stay green, which is the right
      asymmetry: they have nothing to adopt. Without step 0 the restarted process retries a delivery nobody will ever
      apply, to `MaxDeliver`.
      (d) and (f) are L4b's.
      Three further wiring mutants, outside 4.3's list, pin the lane classification itself and are recorded on
      tasks 3.1, 3.2 and 3.4.

## 5. Docs and spec

- [x] 5.1 Correct `docs/concepts/17-approval-flow.md:65` and `processor/agentic-loop/doc.go` restart claims to state the
      I1–I4 contract and its prerequisites (#1327–#1329).
      **Landed.** Four sites, three of them claims that were FALSE rather than merely incomplete:
      - `doc.go:224` said "replacement preserves retained deadlines". Replaced by a `# Recovery across a process
        replacement` section (`processor/agentic-loop/doc.go:226-253`) stating I1–I4, the ordering rule
        (older → effect-free ACK, newer → retry, foreign → quarantine, current → rebuild from the record plus the
        retained request), the one-region replay, and what a replacement does NOT recover.
      - `docs/concepts/17-approval-flow.md:65-69` claimed "the new process picks up the same `awaiting_approval` loop
        and waits on the same response subject" — that is #1362's cold approval branch, not L4a's. The bullet now
        separates the durable pending state from the process-local timer and names #1362.
      - `approval_sweeper.go:40-46` carried the same class one path over from the task's pins: "Restart-safe:
        … a restored loop's deadline is computed correctly on the first sweep after process restart" reads as a
        guarantee the sweeper cannot give, because it snapshots `m.loops` and nothing reads the bucket at startup.
        Rewritten to say memory-only, and to say what a rebuilt loop does get.
      - `docs/concepts/13-agentic-systems.md:216-223` gained the durable one-region fact beside the compaction
        list, agreeing with the operator version in `docs/operations/migration-beta162-to-beta163.md`
        § "A rebuilt loop's conversation is one region" (no "nothing moves" claim on either side).
      Class sweep for the same defect elsewhere: `git grep -n -i 'restart|replacement|survives|persist'` over every
      doc naming `PendingApproval`/`awaiting_approval`/`approval_timeout` found one further hit,
      `docs/operations/migration-beta24-to-beta25.md:155-162` ("An expired loop in KV at restart will auto-reject
      within `approvalSweepInterval` of the new process booting"). It is a SHIPPED release note for a past release
      and is left as written; the beta162→beta163 note's #1330 section names and supersedes it (task 5.3).
- [x] 5.2 Apply the `specs/agentic-loop/spec.md` and `specs/agentic-dispatch/spec.md` deltas;
      `openspec validate agentic-loop-durable-applied-facts --strict` green; `task spec:properties` resolves the `// spec:` citation from 4.1 against the ADDED requirement.
      **Landed.** Both deltas are in the tree: `specs/agentic-loop/spec.md` (one ADDED requirement with seven
      scenarios, one MODIFIED requirement restated in full) and `specs/agentic-dispatch/spec.md` (the OQ4
      requirement, landed `0e327a51`).
      `openspec validate agentic-loop-durable-applied-facts --strict` → `Change 'agentic-loop-durable-applied-facts'
      is valid`, exit 0.
      `task spec:properties` → `323/323 citations resolve.`, exit 0, and `324/324` once task 5.3's new test file is
      tracked — the denominator moves by exactly the one citation that test adds, because the script counts TRACKED
      test files only. Fifteen files carry `// spec: agentic-loop / The loop record names its outstanding request`,
      including 4.1's `applied_facts_property_test.go:102`, and every one resolves against the ADDED requirement.
- [x] 5.3 No approval-deadline hydration (docket OQ2, owner ruling 2026-09-22): the delta scenario "a replaced process
      re-arms no approval deadline; the loop stays `awaiting_approval` until answered or cancelled" plus the same
      sentence as a line in `docs/operations/migration-beta162-to-beta163.md` under a `#1330` section. Test: the zero
      is a measured delta, not a bare absence. The same test first arms a deadline in-process — a real
      `awaiting_approval` record whose deadline the live component's snapshot (`AS:69`
      `SnapshotExpiredApprovals`) reports — then starts a REPLACEMENT component over the same KV and asserts the
      replacement's snapshot is empty, the record is still `awaiting_approval`, and its revision is unchanged.
      **Landed.** Test `TestAReplacementReArmsNoApprovalDeadline`
      (`processor/agentic-loop/approval_deadline_hydration_integration_test.go:34`, `//go:build integration`, real
      NATS). It drives the REAL gate — born loop, model response dispatching one tool, a tool result whose error
      carries `agentic.ApprovalRequiredPrefix` — so `RequestedAt` and `Timeout` are a real pending approval's and
      the record is written by the real carrier. The zero is a measured delta: one instrument
      (`SnapshotExpiredApprovals`, `state.go:630`), one instant, two processes over the same bucket — 1 candidate on
      the process that gated the loop, 0 on the replacement. The instant is passed in rather than waited for, so
      there is no sleep and no backdated fixture and the configured 12h wait stays production's.
      Migration section: `docs/operations/migration-beta162-to-beta163.md:1758` § "A replaced process re-arms no
      approval deadline", which also supersedes the beta.25 note's false "Restart safety" paragraph by name.
      Delta scenario: `specs/agentic-loop/spec.md:58-63`. Conformance row OQ2 updated (`design.md` § 9).
      Mutant (`cp` backup + `md5 -q`, `[applied]` printed between mutating and testing, restore verified by
      checksum): hydrate the replacement — `initializeKVBuckets` lists `AGENT_LOOPS` after acquiring it and seats
      every non-terminal record into the LoopManager, which is exactly the startup pass OQ2 refused. `component.go`
      `77064b5e84396c23a6fdf20a389a712e` → `2333f89a51f9782b80eb857035968c8b` → restored
      `77064b5e84396c23a6fdf20a389a712e`, `git status --porcelain` empty after. RED at
      `approval_deadline_hydration_integration_test.go:59` — "a replacement re-armed a deadline its predecessor
      held", the snapshot returning the gated candidate `{… call-gated … 12h0m0s}`.
      **Finding, escalated not applied (contract § 0.2).** The requirement's free-text sentence
      (`specs/agentic-loop/spec.md:36-38`, verbatim OQ2 ruling text) says a replaced process SHALL re-arm no
      approval deadline, without qualification. Measured false in this tree: task 1.2's rebuild seats the record
      wholesale (`state.go:383-384`), including `State = awaiting_approval` and `PendingApproval`, so a replacement
      that takes a redelivered tool result or model response for a gated loop DOES hold that deadline again — at
      the record's own `RequestedAt + Timeout`. Probe, run and discarded: predecessor snapshot 1, replacement at
      start 0, replacement after redelivering the gated result 1, record still `awaiting_approval` at revision 4.
      The delta SCENARIO is scoped to startup and is exactly true; only the free-text sentence over-reaches. It is
      ruling text, so it is not edited here — the migration note and the doc comments state the narrow truth, and
      the narrowing is owed to the owner.
- [x] 5.4 `tasks_submitted_total` is at-least-once under redelivery (docket OQ4, owner ruling 2026-09-22): the new
      `specs/agentic-dispatch/spec.md` delta states it, plus one line in
      `docs/operations/migration-beta162-to-beta163.md` under the same `#1330` section; no arm change. Test: a
      dispatch-side unit test carrying `// spec: agentic-dispatch / The task submission counter is at-least-once
      under redelivery` and asserting the delta's scenario — a replayed task submission increments the counter again
      while reusing the retained LoopID and publishing no second task
      (`processor/agentic-dispatch/metrics.go:112`, `recordTaskSubmitted` `:321`, increment `:322`).
      **Landed.** Test `TestTaskSubmissionCounterIsAtLeastOnceUnderRedelivery`
      (`processor/agentic-dispatch/task_submission_counter_integration_test.go:42`). Migration section:
      `docs/operations/migration-beta162-to-beta163.md:1782` § "`tasks_submitted_total` is at-least-once, and stays
      that way", with the two reading rules an operator needs. No arm changed: `recordTaskSubmitted` is still called
      unconditionally after the publication on both lanes (`component.go:1168`, `http.go:428`). Conformance row OQ4
      updated (`design.md` § 9).
      **Overlap measured, not assumed.** The second increment is ALREADY observed on this head, inside
      `TestIntegrationPublishedTaskWithFailedResponseQuarantines`
      (`task_submission_settlement_integration_test.go:134`, "the redelivery counted one submission twice"). That
      test's GIVEN is a quarantine with no USER stream, and it cites a different requirement — the counter's
      semantics must not be contingent on that failure being present. The new test's GIVEN is the ordinary path:
      both streams exist, both deliveries acknowledge, nothing fails. It adds what the existing one does not carry —
      the OQ4 citation, the clean-ack precondition, and the derived-identity assertion against
      `stableDispatchTaskID`.
      **Integration, not unit, and why.** Both publications on this path go through the NATS client, so a component
      built without a live one fails the task publication and never reaches the counter. The only way to make it a
      unit test was a production publish seam whose sole consumer is a test, which the "before adding anything new"
      check refuses.
      Mutant (`cp` backup + `md5 -q`, `[applied]` printed between mutating and testing, restore verified by
      checksum): arm the counter to exactly-once — gate `c.metrics.recordTaskSubmitted()` at `component.go:1168`
      behind `if !found`, so a recovered submission does not count. `component.go`
      `bf2dc93c82632dc9e9ba8ded5f56dfe3` → `0949490c12ce7182b610091b55f91de4` → restored
      `bf2dc93c82632dc9e9ba8ded5f56dfe3`, `git status --porcelain` empty after.
      RED at `task_submission_counter_integration_test.go:78` — expected 2, actual 1.

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
